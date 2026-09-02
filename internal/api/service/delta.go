// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
	"gopkg.in/yaml.v3"
)

// imageNameRe mirrors the schema pattern for image.name
// (#/$defs/Image/properties/name): starts and ends with an alphanumeric,
// dashes/underscores allowed in between. Enforced here too because the name
// is embedded in a generated delta before the schema ever sees it, and
// because it lands in build artifact filenames.
var imageNameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-_]*[a-zA-Z0-9])?$`)

// maxImageNameLen bounds the override independently of the schema's general
// string limits: the name becomes part of an artifact filename on disk.
const maxImageNameLen = 64

// packageEntryRe mirrors the schema pattern for systemConfig.packages items
// (#/$defs/SystemConfig/properties/packages/items). It admits the glob forms
// the schema documents (*, ?, bracket ranges) and the `name_version` pin form
// the resolvers match, since a Debian or rpm version uses only characters this
// set already allows. Enforced here as well as by the schema because a bad
// entry is easier to attribute at the field that produced it than inside a
// generated delta's validation failure.
var packageEntryRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+_.:~*?\[\]-]*$`)

const (
	// maxPackages bounds how many packages one selection may carry, matching the
	// OpenAPI maxItems. It is a request-size guard, not a build limit: the
	// curated templates themselves list far more.
	maxPackages = 256
	// maxPackageEntryLen bounds a single entry. A pinned name_version is the
	// long case and stays well under this.
	maxPackageEntryLen = 256
)

// ValidatePackages reports whether every entry is a legal
// systemConfig.packages value, rejecting duplicates and an over-long list. An
// empty slice is valid (nothing selected).
func ValidatePackages(packages []string) error {
	if len(packages) > maxPackages {
		return fmt.Errorf("too many packages: %d exceeds the limit of %d", len(packages), maxPackages)
	}
	seen := make(map[string]bool, len(packages))
	for _, p := range packages {
		switch {
		case p == "":
			return fmt.Errorf("package name must not be empty")
		case len(p) > maxPackageEntryLen:
			return fmt.Errorf("package name exceeds %d characters", maxPackageEntryLen)
		case !packageEntryRe.MatchString(p):
			return fmt.Errorf("package %q must match %s", p, packageEntryRe.String())
		case seen[p]:
			// The schema marks the array uniqueItems, so a duplicate would fail
			// the generated delta's validation anyway — reported here instead so
			// the message names the package.
			return fmt.Errorf("package %q listed more than once", p)
		}
		seen[p] = true
	}
	return nil
}

// packageName returns the package-name part of an entry, dropping a
// `name_version` pin's version. A version begins with a digit (an epoch is
// numeric too), so a suffix that doesn't is part of the name itself —
// createrepo_c is one package, not createrepo pinned to version "c".
//
// Used only to report pin collisions; nothing here rewrites what the user chose.
func packageName(entry string) string {
	i := strings.Index(entry, "_")
	if i <= 0 || i == len(entry)-1 {
		return entry
	}
	if c := entry[i+1]; c < '0' || c > '9' {
		return entry
	}
	return entry[:i]
}

// isPinned reports whether an entry carries an explicit version.
func isPinned(entry string) bool { return packageName(entry) != entry }

// ValidateImageName reports whether name is a legal image.name override, or ""
// (not overridden).
func ValidateImageName(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > maxImageNameLen {
		return fmt.Errorf("image name exceeds %d characters", maxImageNameLen)
	}
	if !imageNameRe.MatchString(name) {
		return fmt.Errorf("image name %q must match %s", name, imageNameRe.String())
	}
	return nil
}

// deltaTemplate is the minimal on-disk shape of a generated extends delta.
// Deliberately its own type rather than config.ImageTemplate: that struct's
// Image/Target/SystemConfig/Disk fields mostly lack `omitempty`, so marshaling
// it emits empty collections and zero-valued blocks that the schema's
// additionalProperties:false would then have to tolerate. This type only ever
// carries what Advanced mode currently allows a user to change.
type deltaTemplate struct {
	Extends string            `yaml:"extends"`
	Image   config.ImageInfo  `yaml:"image"`
	Target  config.TargetInfo `yaml:"target"`
	// SystemConfig is a local minimal type for the same reason the struct above
	// isn't config.ImageTemplate: config.SystemConfig's own Packages field lacks
	// omitempty, so embedding it would emit `packages: []` for a selection with
	// nothing in it.
	SystemConfig *deltaSystemConfig `yaml:"systemConfig,omitempty"`
	// PackageRepositories is safe to reuse from config: its fields carry
	// omitempty except Codename and PKey, both of which a delta always sets.
	PackageRepositories []config.PackageRepository `yaml:"packageRepositories,omitempty"`
	// Disk is the request's own override, emitted verbatim. Unlike the fields
	// above there is nothing to merge with the parent's: the extends merge
	// replaces disk wholesale (config/merge.go), so the override has to be the
	// complete block and the delta simply restates it. DiskOverride carries
	// omitempty throughout, so an unset field is omitted rather than emitted
	// zero-valued.
	Disk *DiskOverride `yaml:"disk,omitempty"`
}

// deltaSystemConfig is the only systemConfig a delta ever declares. Advanced
// mode changes no other field of it, and the merge unions packages across the
// chain, so listing just the additions is enough.
type deltaSystemConfig struct {
	Packages []string `yaml:"packages,omitempty"`
}

// buildDelta renders the extends delta for a selection against its curated
// parent, applying the requested overrides. Deterministic: the same parent,
// image info, and selection always produce the same bytes, so the file the
// Review step resolves and the file a build runs can be generated
// independently and still agree.
//
// parentTemplate is the manifest's template filename (a bare, already
// safeTemplatePath-validated name) — used as-is as the `extends` value so the
// delta references its parent as a sibling, satisfying the containment guard
// in resolveExtendsParentPath.
//
// repos are the already-mapped packageRepositories entries for sel.Repos; the
// mapping needs the catalog, which buildDelta deliberately does not reach for,
// so the caller supplies them (see Service.toTemplateRepos).
func buildDelta(parentTemplate string, parentImage config.ImageInfo, parentTarget config.TargetInfo,
	sel Selection, repos []config.PackageRepository) ([]byte, error) {
	name := parentImage.Name
	if sel.ImageName != "" {
		name = sel.ImageName
	}
	d := deltaTemplate{
		Extends:             parentTemplate,
		Image:               config.ImageInfo{Name: name, Version: parentImage.Version},
		Target:              parentTarget,
		PackageRepositories: repos,
	}
	if len(sel.Packages) > 0 {
		// Sorted rather than kept in click order, so the delta the Review pane
		// shows depends only on *which* packages were chosen and not on the order
		// they were clicked in — the same selection always renders the same file,
		// which is what makes the Delta view diffable against a previous compose.
		// Package order within systemConfig.packages carries no build meaning;
		// dependencies are resolved, not installed positionally.
		pkgs := make([]string, len(sel.Packages))
		copy(pkgs, sel.Packages)
		sort.Strings(pkgs)
		d.SystemConfig = &deltaSystemConfig{Packages: pkgs}
	}
	d.Disk = sel.Disk
	data, err := yaml.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("marshaling delta template: %w", err)
	}
	jsonData, convErr := yamlToJSONForValidation(string(data))
	if convErr != "" {
		return nil, fmt.Errorf("generated delta is not valid YAML: %s", convErr)
	}
	if issues := validate.ValidateUserTemplateIssues(jsonData); hasErrorIssue(issues) {
		return nil, fmt.Errorf("generated delta failed validation: %v", issues)
	}
	return data, nil
}

// hasErrorIssue reports whether any issue is error-severity (warnings are
// tolerated; the delta is machine-generated and minimal by construction, so an
// error here means a generator bug, not a user mistake).
func hasErrorIssue(issues []validate.Issue) bool {
	for _, iss := range issues {
		if iss.Severity == validate.SeverityError {
			return true
		}
	}
	return false
}

// writeDelta writes a generated delta into TemplatesDir under a server-chosen
// name, never a user-derived one. The extends containment guard
// (resolveExtendsParentPath) requires the parent to resolve at or below the
// child's directory, so the delta cannot live in the per-build work
// directory — it must be a sibling of the curated templates it extends.
//
// The returned cleanup func removes the file; callers must invoke it once the
// delta is no longer needed (compose: immediately after use; build: once the
// build finishes and the resolved template has been archived).
func (s *Service) writeDelta(data []byte) (path string, cleanup func(), err error) {
	base, absErr := filepath.Abs(s.cfg.TemplatesDir)
	if absErr != nil {
		return "", nil, fmt.Errorf("resolving templates dir: %w", absErr)
	}
	name := ".ict-adv-" + uuid.NewString() + ".yml"
	full := filepath.Join(base, name)
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return "", nil, fmt.Errorf("writing delta template: %w", err)
	}
	return full, func() { _ = os.Remove(full) }, nil
}
