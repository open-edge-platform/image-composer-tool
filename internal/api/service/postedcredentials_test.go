// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// postedTemplateWithSudoUser mirrors what an imported template looks like when
// it ships a privileged account with a placeholder login: the shape that makes
// a build fail at account creation, minutes in, unless a credential is supplied.
//
// hash_algo is deliberately present. It is the field that makes patching the
// password into the document directly the wrong move — the merge would hash an
// already-hashed value — and the extends delta is what avoids that.
const postedTemplateWithSudoUser = `image:
  name: imported-image
  version: "1.0.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
systemConfig:
  name: imported
  hostname: imported
  users:
    - name: user
      password: ""
      hash_algo: sha512
      groups: ["sudo"]
`

// TestResolveBuildTemplatePostedCredentialsRequired is the guarantee that an
// imported template missing a login is rejected up front, not after the chroot
// is populated.
func TestResolveBuildTemplatePostedCredentialsRequired(t *testing.T) {
	s := newTestService(t)

	_, _, err := s.resolveBuildTemplate(&BuildRequest{YAML: postedTemplateWithSudoUser}, t.TempDir())
	if err == nil {
		t.Fatal("expected a credential-required rejection, got none")
	}
	if !strings.Contains(err.Error(), "no password or SSH authorized key") {
		t.Errorf("error = %q, want it to name the missing credential", err)
	}
}

// TestResolveBuildTemplatePostedCredentials covers supplying the missing login:
// the build resolves to a generated delta over the posted document, and the
// account reads as satisfied once merged.
func TestResolveBuildTemplatePostedCredentials(t *testing.T) {
	requireOpenssl(t)

	s := newTestService(t)
	wd := t.TempDir()

	path, name, err := s.resolveBuildTemplate(&BuildRequest{
		YAML:        postedTemplateWithSudoUser,
		Credentials: []CredentialInput{{User: "user", Password: "correct horse battery staple"}},
	}, wd)
	if err != nil {
		t.Fatalf("resolveBuildTemplate: %v", err)
	}
	// The display name still follows the document, not the generated file.
	if name != "imported-image.yml" {
		t.Errorf("name = %q, want imported-image.yml", name)
	}
	if filepath.Dir(path) != wd {
		t.Errorf("delta %q is not a sibling of the posted document in %q", path, wd)
	}

	// The posted document is built as supplied — the credential rides in a
	// separate file rather than being patched into it.
	posted, rerr := os.ReadFile(filepath.Join(wd, "template.yml"))
	if rerr != nil {
		t.Fatalf("reading the posted template: %v", rerr)
	}
	if string(posted) != postedTemplateWithSudoUser {
		t.Error("the posted document was modified; it must be built verbatim")
	}

	delta, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("reading the generated delta: %v", rerr)
	}
	if !strings.Contains(string(delta), "extends: template.yml") {
		t.Errorf("delta does not extend the posted document:\n%s", delta)
	}
	if strings.Contains(string(delta), "correct horse battery staple") {
		t.Error("the plain-text password reached the generated delta")
	}

	merged, merr := config.LoadAndMergeTemplate(path)
	if merr != nil {
		t.Fatalf("merging the generated delta: %v", merr)
	}
	for _, u := range merged.SystemConfig.Users {
		if u.Name != "user" {
			continue
		}
		if u.NeedsCredential() {
			t.Error("the account still reads as needing a credential after the merge")
		}
		if !config.IsCryptHash(u.Password) {
			t.Errorf("merged password = %q, want a crypt hash", u.Password)
		}
		// Left set, the merge would hash the hash at build time.
		if u.HashAlgo != "" {
			t.Errorf("hash_algo = %q, want it cleared by the merge", u.HashAlgo)
		}
	}
}

// TestResolveBuildTemplateCredentialsWithoutYAML pins the one combination the
// property-count bounds in the contract cannot express.
func TestResolveBuildTemplateCredentialsWithoutYAML(t *testing.T) {
	s := newTestService(t)

	_, _, err := s.resolveBuildTemplate(&BuildRequest{
		Compose:     &Selection{Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso"},
		Credentials: []CredentialInput{{User: "admin", Password: "x"}},
	}, t.TempDir())
	if err == nil {
		t.Fatal("expected credentials alongside compose to be rejected")
	}
	if !strings.Contains(err.Error(), "compose.credentials") {
		t.Errorf("error = %q, want it to point at compose.credentials", err)
	}
}

// TestValidateTemplateReportsCredentials covers the list the import flow
// prompts from: validating a posted template reports its accounts.
func TestValidateTemplateReportsCredentials(t *testing.T) {
	s := newTestService(t)

	res, err := s.ValidateTemplate(postedTemplateWithSudoUser)
	if err != nil {
		t.Fatalf("ValidateTemplate: %v", err)
	}
	if !res.Valid {
		t.Fatalf("template reported invalid: %+v", res.Errors)
	}
	var found bool
	for _, c := range res.Credentials {
		if c.User != "user" {
			continue
		}
		found = true
		if !c.Required || c.Satisfied {
			t.Errorf("account = %+v, want required and unsatisfied", c)
		}
	}
	if !found {
		t.Errorf("accounts = %+v, want the template's sudo user listed", res.Credentials)
	}
}

// An invalid document has no accounts to report; the errors are what the caller
// acts on, and resolving it would only fail.
func TestValidateTemplateOmitsCredentialsWhenInvalid(t *testing.T) {
	s := newTestService(t)

	res, err := s.ValidateTemplate("image:\n  name: \"-bad\"\ntarget:\n  os: not-an-os\n")
	if err != nil {
		t.Fatalf("ValidateTemplate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected the template to be reported invalid")
	}
	if res.Credentials != nil {
		t.Errorf("credentials = %+v, want none for an invalid template", res.Credentials)
	}
}
