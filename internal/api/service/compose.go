// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// Selection is the set of UI choices that identify one combination, plus any
// Advanced-mode overrides. The service uses the first five fields to look up
// the matching pre-authored template in the manifest; ImageName never
// participates in that lookup (findTemplate ignores it) — it is applied to
// the resolved template afterward via a generated extends delta.
type Selection struct {
	Vertical  string
	SKU       string
	Platform  string
	OS        string
	ImageType string

	// ImageName overrides the matched template's image.name. Empty means
	// "not overridden".
	ImageName string

	// Packages are the Advanced tab's selected packages, emitted into the
	// generated delta's systemConfig.packages. A pinned pick carries the
	// `name_version` form the package resolvers match (debutils/resolver.go,
	// rpmutils/helper.go); a bare name floats to whatever the repository
	// publishes. Like ImageName, it takes no part in findTemplate's lookup.
	Packages []string
	// Repos are catalog repository ids the user enabled, emitted into the
	// delta's packageRepositories so a package picked from a repository the
	// matched template doesn't configure can still resolve. Also lookup-neutral.
	Repos []string
	// Disk replaces the matched template's disk block wholesale, as edited by
	// the Advanced tab's Disk step. Nil means "not overridden". Lookup-neutral
	// like the fields above.
	Disk *DiskOverride
	// Credentials supply a login for a template user the template itself leaves
	// without one, emitted into the delta's systemConfig.users. The curated
	// unattended-ISO templates deliberately ship a sudo account with a
	// placeholder key file and no password, so without this they can only be
	// built from the CLI via --ssh-authorized-key. Lookup-neutral.
	Credentials []CredentialInput
	// NewUsers are accounts the template does not declare, created by this
	// selection. Credentials deliberately cannot do this (an unknown name is
	// rejected there), so adding an account is a separate, explicit input: it
	// carries the account's privileges and shell as well as its login, and must
	// never be confused with attaching a password to a curated account.
	// Lookup-neutral like the fields above.
	NewUsers []NewUserInput
}

// CredentialInput is a login the caller supplies for one existing template
// user. Either field satisfies the account on its own, matching what
// config.ValidateUserCredentials accepts.
type CredentialInput struct {
	// User must name a user the resolved template already declares; this never
	// creates an account, matching --ssh-authorized-key's contract.
	User string
	// Password is accepted as plain text (what a browser form collects) or as a
	// complete crypt(3) hash. Either way only a hash reaches the generated
	// delta — see config.HashPasswordForHost.
	Password string
	// SSHAuthorizedKey is one authorized_keys line, validated with the same
	// parser that checks the key files a template points at.
	SSHAuthorizedKey string
}

// NewUserInput is an account to create that the template does not declare.
// Mirrors config.UserConfig minus the fields a browser cannot supply: a host
// key-file path, and a hash algorithm (Password is hashed here).
type NewUserInput struct {
	// User is the account name. It must *not* name a user the resolved template
	// already declares — that is what CredentialInput is for.
	User string
	// Password is accepted as plain text or as a complete crypt(3) hash, like
	// CredentialInput.Password, and only a hash reaches the generated delta.
	Password string
	// SSHAuthorizedKey is one authorized_keys line.
	SSHAuthorizedKey string
	// Sudo grants the new account sudo access.
	Sudo bool
	// Groups are additional groups the account joins.
	Groups []string
	// Shell, Home and StartupScript mirror the same template fields; empty means
	// "leave it to the OS default".
	Shell         string
	Home          string
	StartupScript string
	// PasswordMaxAge is the password ageing limit in days; nil inherits.
	PasswordMaxAge *int
}

// CredentialRequirement reports one template user the caller may supply a
// login for, so the UI can prompt before a build instead of letting it fail
// deep inside one.
type CredentialRequirement struct {
	User string
	Sudo bool
	// Required is true for every account the credential rule applies to
	// (config.UserConfig.CredentialRequired) — every user except one confined
	// to a startupScript, which has no interactive login to protect. A caller
	// uses this to tell "must supply a login" from "may optionally add one".
	Required bool
	// Satisfied is false when the account is Required and the template leaves it
	// with no password and no SSH key, which is what blocks a build. Always true
	// for an account the rule exempts: nothing about it ever blocks a build.
	Satisfied bool
}

// hasOverrides reports whether the selection asks for anything beyond the
// matched template, i.e. whether a delta has to be generated at all. Without
// any, the curated template is resolved directly and no file is written.
func (s Selection) hasOverrides() bool {
	return s.ImageName != "" || len(s.Packages) > 0 || len(s.Repos) > 0 ||
		s.Disk != nil || len(s.Credentials) > 0 || len(s.NewUsers) > 0
}

// credentialRequirements lists every user of a merged template, flagging which
// the credential rule applies to and, for those, whether they still need one.
//
// Every user is listed, including one the rule exempts: a credential is still
// accepted for it (validateCredentials has no such exemption), so the UI
// needs to know the account exists in order to offer an optional SSH key for
// it. Only a Required account ever blocks a build — that uses the same
// predicate NeedsCredential is built on, so what is required here and what is
// enforced at build time cannot diverge.
//
// Must run on a merged, lowered template: lowerSSHKeyFiles has by then read
// every sshAuthorizedKeysFiles entry into SSHAuthorizedKeys, so a key file
// holding only comments correctly reads as unsatisfied — which is exactly the
// case the curated EdgePack templates ship.
func credentialRequirements(merged *config.ImageTemplate) []CredentialRequirement {
	var out []CredentialRequirement
	for _, u := range merged.SystemConfig.Users {
		out = append(out, CredentialRequirement{
			User:      u.Name,
			Sudo:      u.HasSudoAccess(),
			Required:  u.CredentialRequired(),
			Satisfied: !u.NeedsCredential(),
		})
	}
	return out
}

// validateCredentials checks supplied credentials against the curated parent.
// Returns the credentials with every password replaced by a crypt hash, so a
// caller cannot accidentally emit the plain text.
//
// parent is the curated template as merged *without* these credentials, which
// is what names the users available to attach one to.
func validateCredentials(creds []CredentialInput, parent *config.ImageTemplate) ([]CredentialInput, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	known := make(map[string]bool, len(parent.SystemConfig.Users))
	for _, u := range parent.SystemConfig.Users {
		known[u.Name] = true
	}
	seen := make(map[string]bool, len(creds))
	out := make([]CredentialInput, 0, len(creds))
	for _, c := range creds {
		if c.User == "" {
			return nil, fmt.Errorf("credential is missing a user name")
		}
		// Mirrors ApplyCompositionOverrides' wording for the equivalent CLI
		// failure, so the same mistake reads the same way from either entry point.
		if !known[c.User] {
			return nil, fmt.Errorf("credential for user %q: no such user in systemConfig.users", c.User)
		}
		if seen[c.User] {
			return nil, fmt.Errorf("credential for user %q: supplied more than once", c.User)
		}
		seen[c.User] = true
		if c.Password == "" && c.SSHAuthorizedKey == "" {
			return nil, fmt.Errorf("credential for user %q: supply a password, an SSH public key, or both", c.User)
		}
		if c.SSHAuthorizedKey != "" {
			if err := config.ValidateSSHAuthorizedKey(c.SSHAuthorizedKey); err != nil {
				return nil, fmt.Errorf("SSH public key for user %q: %w", c.User, err)
			}
		}
		if c.Password != "" {
			hashed, err := config.HashPasswordForHost(c.Password)
			if err != nil {
				return nil, fmt.Errorf("password for user %q: %w", c.User, err)
			}
			c.Password = hashed
		}
		out = append(out, c)
	}
	return out, nil
}

// userNameRe mirrors the template schema's pattern for systemConfig.users
// items' `name` (and for a group name, which has the same shape). Enforced
// here as well as by the schema so a bad name is reported against the field
// the user typed it into rather than as a generated-delta validation failure.
var userNameRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,31}$`)

// startupScriptRe mirrors the schema pattern for a user's startupScript: an
// absolute path with no colon or whitespace, or empty.
var startupScriptRe = regexp.MustCompile(`^(/[^:\s]*)?$`)

// maxNewUsers bounds how many accounts one selection may add, matching the
// OpenAPI maxItems. A request-size guard, not an image limit.
const maxNewUsers = 32

// The remaining NewUserInput bounds the contract publishes. Nothing checks a
// request body against the spec at runtime — the handlers only decode the
// generated structs — so a value outside the published shape reaches here
// unexamined unless this mirrors it, exactly as ValidateDisk does for the disk
// override.
const (
	maxNewUserPasswordLen = 256
	maxNewUserKeyLen      = 4096
	maxNewUserPathLen     = 256
	maxNewUserGroups      = 64
	maxPasswordMaxAge     = 99999
)

// validateNewUsers checks accounts the caller asked to create against the
// curated parent. Returns them with every password replaced by a crypt hash,
// for the same reason validateCredentials does.
//
// parent is the curated template merged *without* this request, which is what
// says which names are already taken. A name it already declares is rejected
// rather than merged into: the extends merge would quietly rewrite that
// account's sudo grant, shell and home, so changing a curated account is not
// something this path offers by accident.
func validateNewUsers(users []NewUserInput, parent *config.ImageTemplate) ([]NewUserInput, error) {
	if len(users) == 0 {
		return nil, nil
	}
	if len(users) > maxNewUsers {
		return nil, fmt.Errorf("too many new users: %d exceeds the limit of %d", len(users), maxNewUsers)
	}
	existing := make(map[string]bool, len(parent.SystemConfig.Users))
	for _, u := range parent.SystemConfig.Users {
		existing[u.Name] = true
	}
	seen := make(map[string]bool, len(users))
	out := make([]NewUserInput, 0, len(users))
	for _, u := range users {
		switch {
		case u.User == "":
			return nil, fmt.Errorf("new user is missing a name")
		case !userNameRe.MatchString(u.User):
			return nil, fmt.Errorf("new user %q: name must match %s", u.User, userNameRe.String())
		case existing[u.User]:
			return nil, fmt.Errorf("new user %q: the template already declares this user; "+
				"supply a credential for it instead of creating it", u.User)
		case seen[u.User]:
			return nil, fmt.Errorf("new user %q: supplied more than once", u.User)
		}
		seen[u.User] = true
		if len(u.Password) > maxNewUserPasswordLen {
			return nil, fmt.Errorf("new user %q: password exceeds %d characters", u.User, maxNewUserPasswordLen)
		}
		if len(u.SSHAuthorizedKey) > maxNewUserKeyLen {
			return nil, fmt.Errorf("new user %q: SSH public key exceeds %d characters", u.User, maxNewUserKeyLen)
		}
		for field, value := range map[string]string{
			"login shell":    u.Shell,
			"home directory": u.Home,
			"startup script": u.StartupScript,
		} {
			if len(value) > maxNewUserPathLen {
				return nil, fmt.Errorf("new user %q: %s exceeds %d characters", u.User, field, maxNewUserPathLen)
			}
		}
		if !startupScriptRe.MatchString(u.StartupScript) {
			return nil, fmt.Errorf("new user %q: startup script must be an absolute path", u.User)
		}
		// Demanded for every account the credential rule applies to, which is
		// every account except one confined to a startupScript — it has no
		// interactive login to protect, and config.UserConfig.CredentialRequired
		// exempts it, so demanding one here would make an account the template
		// format allows impossible to create.
		if u.StartupScript == "" && u.Password == "" && u.SSHAuthorizedKey == "" {
			return nil, fmt.Errorf("new user %q: supply a password, an SSH public key, or both", u.User)
		}
		if len(u.Groups) > maxNewUserGroups {
			return nil, fmt.Errorf("new user %q: too many groups: %d exceeds the limit of %d",
				u.User, len(u.Groups), maxNewUserGroups)
		}
		groups := make(map[string]bool, len(u.Groups))
		for _, g := range u.Groups {
			if !userNameRe.MatchString(g) {
				return nil, fmt.Errorf("new user %q: group %q must match %s", u.User, g, userNameRe.String())
			}
			if groups[g] {
				return nil, fmt.Errorf("new user %q: group %q listed more than once", u.User, g)
			}
			groups[g] = true
		}
		if u.PasswordMaxAge != nil && (*u.PasswordMaxAge < 0 || *u.PasswordMaxAge > maxPasswordMaxAge) {
			return nil, fmt.Errorf("new user %q: password max age must be between 0 and %d days",
				u.User, maxPasswordMaxAge)
		}
		if u.SSHAuthorizedKey != "" {
			if err := config.ValidateSSHAuthorizedKey(u.SSHAuthorizedKey); err != nil {
				return nil, fmt.Errorf("SSH public key for new user %q: %w", u.User, err)
			}
		}
		if u.Password != "" {
			hashed, err := config.HashPasswordForHost(u.Password)
			if err != nil {
				return nil, fmt.Errorf("password for new user %q: %w", u.User, err)
			}
			u.Password = hashed
		}
		out = append(out, u)
	}
	return out, nil
}

// ComposeSummary is the human-readable summary shown in the Review panel and
// stored on the build record for the Build Details panel.
type ComposeSummary struct {
	// Selection echo — what the user chose
	Vertical  string
	SKU       string
	Platform  string
	OS        string
	ImageType string

	// Template-derived — info the user can't see from the dropdowns
	ImageName      string
	ImageVersion   string
	Description    string
	Architecture   string
	KernelVersion  string
	PackageCount   int
	DiskSize       string
	PartitionCount int
	PartitionTable string
	Hostname       string

	// Overlay-mode only: the baseline image the packages are layered onto.
	BaseImage string

	// Advanced-tab changes layered onto the matched template. Nil when the
	// selection overrode nothing.
	Advanced *AdvancedChanges `json:",omitempty"`
}

// AdvancedChanges records what an Advanced-tab selection changed relative to
// the matched template, so a build's details can show the delta and not just
// the merged result. Logins are reduced to account names: no password or key
// is ever kept here.
type AdvancedChanges struct {
	ImageName      string             `json:",omitempty"`
	Packages       []string           `json:",omitempty"`
	Repos          []string           `json:",omitempty"`
	Disk           *DiskChangeSummary `json:",omitempty"`
	NewUsers       []string           `json:",omitempty"`
	CredentialsFor []string           `json:",omitempty"`
}

// DiskChangeSummary is the display-relevant part of a disk override.
type DiskChangeSummary struct {
	Size           string                 `json:",omitempty"`
	PartitionTable string                 `json:",omitempty"`
	Partitions     []DiskPartitionSummary `json:",omitempty"`
}

// DiskPartitionSummary is one partition of a disk override, for display.
type DiskPartitionSummary struct {
	ID         string `json:",omitempty"`
	FsType     string `json:",omitempty"`
	MountPoint string `json:",omitempty"`
	Start      string `json:",omitempty"`
	End        string `json:",omitempty"`
}

// advancedChanges summarises a selection's overrides, or nil when it has none.
func advancedChanges(sel Selection) *AdvancedChanges {
	if !sel.hasOverrides() {
		return nil
	}
	a := &AdvancedChanges{
		ImageName: sel.ImageName,
		Packages:  sel.Packages,
		Repos:     sel.Repos,
	}
	if d := sel.Disk; d != nil {
		ds := &DiskChangeSummary{Size: d.Size, PartitionTable: d.PartitionTableType}
		for _, p := range d.Partitions {
			ds.Partitions = append(ds.Partitions, DiskPartitionSummary{
				ID: p.ID, FsType: p.FsType, MountPoint: p.MountPoint, Start: p.Start, End: p.End,
			})
		}
		a.Disk = ds
	}
	for _, u := range sel.NewUsers {
		a.NewUsers = append(a.NewUsers, u.User)
	}
	for _, c := range sel.Credentials {
		a.CredentialsFor = append(a.CredentialsFor, c.User)
	}
	return a
}

// ComposeResult is the outcome of resolving a Selection to a template.
type ComposeResult struct {
	Template string // resolved template filename (the curated parent, even when overridden)
	YAML     string // the resolved final template — what a build from this selection would run
	Summary  ComposeSummary
	// DeltaYAML is the generated extends delta, verbatim — only what this
	// selection overrode. Empty when the selection carries no overrides, since
	// no delta is generated then.
	DeltaYAML string
	// BaseYAML is the curated parent resolved *without* this selection's
	// overrides: what YAML would have been had nothing been overridden. Lets a
	// caller show the baseline beside the delta that modifies it. Empty when
	// there are no overrides, because YAML already is the base.
	BaseYAML string
	// PinConflicts names packages requested at a pinned version whose name the
	// base template already lists unpinned. The merge unions package lists and
	// cannot drop the parent's entry, so both survive into YAML. Advisory only.
	PinConflicts []string
	// BasePackages is the curated template's own systemConfig.packages list —
	// what it ships before this selection's overrides are applied. Populated
	// unconditionally (unlike BaseYAML/PinConflicts), so a caller can show
	// "already included" packages even before the user has added anything of
	// their own.
	BasePackages []string
	// Credentials lists every account of the resolved template and whether each
	// has a login yet. Accounts subject to the credential rule are marked
	// Required; those exempt (confined to a startupScript) are marked optional.
	// A caller can use this to prompt for missing credentials rather than
	// discovering the gap partway through a build.
	Credentials []CredentialRequirement
}

// Compose resolves the selections to a template, applies any Advanced-mode
// overrides, and returns the resulting resolved template plus a summary.
// Input and lookup failures return a *Error carrying the appropriate HTTP
// status/code.
//
// With no overrides, the returned YAML is exactly what
// `image-composer-tool resolve --full` prints for the matched curated
// template — the same functions are used. With an override, a generated
// extends delta is resolved instead, so the returned YAML is what building
// this exact selection would produce. Either way, this is the review path:
// it never mutates the curated template on disk.
func (s *Service) Compose(sel Selection) (*ComposeResult, error) {
	if sel.Vertical == "" || sel.Platform == "" || sel.OS == "" || sel.ImageType == "" {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST",
			"vertical, platform, os, and imageType are required")
	}
	if err := ValidateImageName(sel.ImageName); err != nil {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	if err := ValidatePackages(sel.Packages); err != nil {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	if err := ValidateDisk(sel.Disk, sel.ImageType); err != nil {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	tmpl := s.manifest.findTemplate(sel.Vertical, sel.SKU, sel.Platform, sel.OS, sel.ImageType)
	if tmpl == "" {
		return nil, newError(http.StatusBadRequest, "NO_MATCH",
			"no template maps to the selected combination")
	}

	path, err := safeTemplatePath(s.cfg.TemplatesDir, tmpl)
	if err != nil {
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_INVALID",
			"manifest template path is invalid")
	}
	// A manifest entry pointing at a file that isn't on disk is a server-side
	// configuration error, not "this template is invalid" — surface it as its
	// own code/status rather than letting it fall through to the generic
	// load/merge failure below. Distinguish "not found" from other stat
	// failures (permissions, transient IO) so the latter isn't misreported as
	// a missing file and its cause isn't lost.
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, newError(http.StatusInternalServerError, "TEMPLATE_MISSING",
				"matched template file not found on disk")
		}
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_STAT_FAILED",
			fmt.Sprintf("checking matched template file: %v", statErr))
	}

	// Credentials name existing users and new users must not collide with one,
	// so both are checked against the curated parent as merged *without* them —
	// and their passwords hashed — before the delta that carries them is
	// rendered. Only merged early when one of them is actually supplied, to keep
	// the common path at one merge.
	if len(sel.Credentials) > 0 || len(sel.NewUsers) > 0 {
		parentMerged, perr := config.LoadAndMergeTemplate(path)
		if perr != nil {
			return nil, newError(http.StatusUnprocessableEntity, "TEMPLATE_INVALID",
				"matched template failed to load/validate: "+perr.Error())
		}
		hashed, cerr := validateCredentials(sel.Credentials, parentMerged)
		if cerr != nil {
			return nil, newError(http.StatusBadRequest, "BAD_REQUEST", cerr.Error())
		}
		sel.Credentials = hashed
		created, uerr := validateNewUsers(sel.NewUsers, parentMerged)
		if uerr != nil {
			return nil, newError(http.StatusBadRequest, "BAD_REQUEST", uerr.Error())
		}
		sel.NewUsers = created
	}

	resolvePath := path
	var deltaYAML string
	if sel.hasOverrides() {
		deltaPath, data, cleanup, derr := s.deltaForOverride(tmpl, path, sel)
		if derr != nil {
			return nil, newError(http.StatusInternalServerError, "TEMPLATE_INVALID", derr.Error())
		}
		defer cleanup()
		resolvePath = deltaPath
		// Redacted, unlike the bytes written for the build: this string is
		// returned to the caller and the Advanced tab renders it with Copy and
		// Export, so a password hash in it would be on screen and in a
		// downloaded file.
		deltaYAML = redactDeltaYAML(data)
	}

	// Parse+merge (reuses ICT's own logic) for both the summary and the
	// resolved YAML. A merge failure means the matched template (or, with an
	// override, the generated delta) is invalid — surface it now rather than
	// returning a misleading success that fails at build time.
	merged, err := config.LoadAndMergeTemplate(resolvePath)
	if err != nil {
		return nil, newError(http.StatusUnprocessableEntity, "TEMPLATE_INVALID",
			"matched template failed to load/validate: "+err.Error())
	}

	yamlBytes, err := config.MarshalTemplateYAML(config.RedactSensitiveData(merged))
	if err != nil {
		return nil, newError(http.StatusInternalServerError, "TEMPLATE_INVALID",
			"resolved template failed to marshal: "+err.Error())
	}

	res := &ComposeResult{
		Template:  tmpl,
		YAML:      string(yamlBytes),
		Summary:   buildComposeSummary(sel, merged),
		DeltaYAML: deltaYAML,
		// With no overrides, merged *is* the base template — set here so it's
		// still correct if the overrides branch below skips the second resolve.
		BasePackages: merged.SystemConfig.Packages,
		// Reported from the resolved template, so a credential supplied in this
		// same request shows as satisfied and the caller stops prompting.
		Credentials: credentialRequirements(merged),
	}

	// With overrides in play, resolve the curated parent a second time so the
	// caller can show the baseline beside the delta and attribute the difference
	// to the user's own choices. Skipped when there are no overrides, because
	// YAML above already *is* the base — and a failure here is not worth failing
	// the compose over: the resolved template is the answer, the baseline is
	// context.
	if sel.hasOverrides() {
		if base, berr := config.LoadAndMergeTemplate(path); berr == nil {
			if baseBytes, merr := config.MarshalTemplateYAML(config.RedactSensitiveData(base)); merr == nil {
				res.BaseYAML = string(baseBytes)
			}
			res.PinConflicts = pinConflicts(sel.Packages, base.SystemConfig.Packages)
			res.BasePackages = base.SystemConfig.Packages
		} else {
			logger.Logger().Warnf("compose: baseline resolve of %s failed, omitting base view: %v", tmpl, berr)
		}
	}
	return res, nil
}

// pinConflicts names the requested packages that are pinned to a version while
// the base template already lists the same package unpinned. The extends merge
// unions package lists and cannot remove the parent's entry, so both survive
// into the resolved template and the resolver may install either.
//
// Reported rather than rejected or silently dropped: rejecting would fail the
// Review pane for a common pick (the curated templates list dozens of packages
// users browse straight into), and dropping would discard the version the user
// explicitly asked for.
func pinConflicts(requested, basePackages []string) []string {
	if len(requested) == 0 || len(basePackages) == 0 {
		return nil
	}
	base := make(map[string]bool, len(basePackages))
	for _, p := range basePackages {
		base[p] = true
	}
	var out []string
	for _, entry := range requested {
		if !isPinned(entry) {
			continue
		}
		// Only an *unpinned* parent entry conflicts. A parent already pinned to
		// the same version dedups on the exact string, and one pinned to a
		// different version is a collision the parent authored deliberately.
		if base[packageName(entry)] {
			out = append(out, entry)
		}
	}
	sort.Strings(out)
	return out
}

// deltaForOverride loads the curated parent's image/target info, renders a
// delta applying sel's overrides, and writes it via writeDelta.
//
// parentTmpl is the manifest-provided name (relative to TemplatesDir, as
// returned by findTemplate/safeTemplatePath) — it is used verbatim as the
// delta's `extends` value, since the delta is always written at the root of
// TemplatesDir and extends is resolved relative to the child's own directory.
// Using filepath.Base(parentPath) instead would break for a curated template
// that lives in a subdirectory of TemplatesDir.
//
// Split out of Compose so StartBuild's build-time delta generation
// (builds.go) can share the exact same rendering path.
//
// The rendered bytes are returned alongside the path so Compose can publish the
// delta for review without reading back the file it just wrote.
func (s *Service) deltaForOverride(parentTmpl, parentPath string, sel Selection) (
	path string, data []byte, cleanup func(), err error) {
	parent, err := config.LoadTemplate(parentPath, false)
	if err != nil {
		return "", nil, nil, fmt.Errorf("loading parent template: %w", err)
	}
	data, err = buildDelta(parentTmpl, parent.Image, parent.Target, sel, s.toTemplateRepos(sel.OS, sel.Repos), s.edgePackNameSet())
	if err != nil {
		return "", nil, nil, err
	}
	written, cleanup, err := s.writeDelta(data)
	if err != nil {
		return "", nil, nil, err
	}
	return written, data, cleanup, nil
}

// buildComposeSummary constructs a ComposeSummary from a selection and a merged
// template. Shared by Compose and StartBuild (the latter stores the summary on
// the build record for the Build Details panel).
func buildComposeSummary(sel Selection, merged *config.ImageTemplate) ComposeSummary {
	// For overlay-mode templates, surface the baseline image the packages are
	// layered onto (local path or URL). Empty for from-scratch builds.
	var baseImage string
	if merged.Baseline != nil && merged.Baseline.Mode == config.BaselineModeOverlay && merged.Baseline.Source != nil {
		if merged.Baseline.Source.Path != "" {
			baseImage = merged.Baseline.Source.Path
		} else {
			baseImage = merged.Baseline.Source.URL
		}
	}

	// Overlay mode doesn't populate systemConfig.kernel; when a kernel swap is
	// configured, surface its (descriptive-only) version instead.
	kernelVersion := merged.SystemConfig.Kernel.Version
	if merged.OverlayPolicy != nil && merged.OverlayPolicy.ReplaceKernel != nil && merged.OverlayPolicy.ReplaceKernel.Version != "" {
		kernelVersion = merged.OverlayPolicy.ReplaceKernel.Version
	}

	return ComposeSummary{
		Vertical:  sel.Vertical,
		SKU:       sel.SKU,
		Platform:  sel.Platform,
		OS:        sel.OS,
		ImageType: sel.ImageType,

		ImageName:      merged.Image.Name,
		ImageVersion:   merged.Image.Version,
		Description:    merged.SystemConfig.Description,
		Architecture:   merged.Target.Arch,
		KernelVersion:  kernelVersion,
		PackageCount:   len(merged.SystemConfig.Packages),
		DiskSize:       merged.Disk.Size,
		PartitionCount: len(merged.Disk.Partitions),
		PartitionTable: merged.Disk.PartitionTableType,
		Hostname:       merged.SystemConfig.HostName,
		BaseImage:      baseImage,

		Advanced: advancedChanges(sel),
	}
}
