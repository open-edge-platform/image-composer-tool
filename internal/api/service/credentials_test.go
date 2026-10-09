// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"gopkg.in/yaml.v3"
)

// testPubKey is a throwaway ed25519 public key, generated for these tests and
// held by nobody. A public key is not a secret; it is here as a literal so the
// tests exercise the real OpenSSH parser rather than a stub.
const testPubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEFoaP9Qqm8uDNaok/P4QsOihd5kZ+FhrAdOWJesmBzR ict-test-key"

func sudoUser(name string) config.UserConfig {
	return config.UserConfig{Name: name, Sudo: true}
}

func templateWithUsers(users ...config.UserConfig) *config.ImageTemplate {
	return &config.ImageTemplate{SystemConfig: config.SystemConfig{Users: users}}
}

// TestCredentialRequirements covers which accounts are reported, which are
// Required, and whether each reads as satisfied. The unsatisfied sudo case is
// the one that matters most: it is what the curated unattended-ISO templates
// ship, and reporting it is what lets the UI prompt instead of letting a build
// fail minutes in. Every user is listed — including unprivileged ones, which
// are never Required but may still optionally receive a credential — since
// validateCredentials accepts one for any user in the template.
func TestCredentialRequirements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		users []config.UserConfig
		want  []CredentialRequirement
	}{
		{
			name:  "sudo user with no credential is required and unsatisfied",
			users: []config.UserConfig{sudoUser("admin")},
			want:  []CredentialRequirement{{User: "admin", Sudo: true, Required: true, Satisfied: false}},
		},
		{
			// A template pointing at a key file holding only comments reaches
			// here with SSHAuthorizedKeys empty, because lowerSSHKeyFiles skips
			// comment lines. That is the EdgePack admin.pub case.
			name: "sudo user whose key file yielded no keys is unsatisfied",
			users: []config.UserConfig{{
				Name: "admin", Sudo: true,
				SSHAuthorizedKeysFiles: []string{"additionalfiles/base-platform/admin.pub"},
			}},
			want: []CredentialRequirement{{User: "admin", Sudo: true, Required: true, Satisfied: false}},
		},
		{
			name: "inline SSH key satisfies",
			users: []config.UserConfig{{
				Name: "admin", Sudo: true, SSHAuthorizedKeys: []string{testPubKey},
			}},
			want: []CredentialRequirement{{User: "admin", Sudo: true, Required: true, Satisfied: true}},
		},
		{
			name:  "password satisfies",
			users: []config.UserConfig{{Name: "admin", Sudo: true, Password: "$6$salt$hash"}},
			want:  []CredentialRequirement{{User: "admin", Sudo: true, Required: true, Satisfied: true}},
		},
		{
			name:  "sudo via admin group counts as privileged",
			users: []config.UserConfig{{Name: "ops", Groups: []string{"wheel"}}},
			want:  []CredentialRequirement{{User: "ops", Sudo: true, Required: true, Satisfied: false}},
		},
		{
			// Listed so the UI can offer an optional SSH key, but never blocks a
			// build: Required and Satisfied both reflect that.
			name:  "unprivileged user is reported but not required",
			users: []config.UserConfig{{Name: "guest"}},
			want:  []CredentialRequirement{{User: "guest", Sudo: false, Required: false, Satisfied: true}},
		},
		{
			name:  "root is privileged without the sudo flag",
			users: []config.UserConfig{{Name: "root"}},
			want:  []CredentialRequirement{{User: "root", Sudo: false, Required: true, Satisfied: false}},
		},
		{
			// The installer environment's console account, confined to its script:
			// still listed, but neither required nor satisfied-by-default implied.
			name:  "root confined to a startup script is not required",
			users: []config.UserConfig{{Name: "root", StartupScript: "/usr/bin/installer"}},
			want:  []CredentialRequirement{{User: "root", Sudo: false, Required: false, Satisfied: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := credentialRequirements(templateWithUsers(tt.users...))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d requirements %+v, want %d", len(got), got, len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("requirement %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestValidateCredentialsRejects covers the inputs that must not reach a
// delta. Each is a 400 to the caller rather than a failed build.
func TestValidateCredentialsRejects(t *testing.T) {
	t.Parallel()

	parent := templateWithUsers(sudoUser("admin"), config.UserConfig{Name: "guest"})

	tests := []struct {
		name    string
		creds   []CredentialInput
		wantErr string
	}{
		{
			name:    "unknown user",
			creds:   []CredentialInput{{User: "nobody", Password: "hunter2"}},
			wantErr: "no such user in systemConfig.users",
		},
		{
			name:    "missing user name",
			creds:   []CredentialInput{{Password: "hunter2"}},
			wantErr: "missing a user name",
		},
		{
			name:    "neither password nor key",
			creds:   []CredentialInput{{User: "admin"}},
			wantErr: "supply a password, an SSH public key, or both",
		},
		{
			name: "same user twice",
			creds: []CredentialInput{
				{User: "admin", SSHAuthorizedKey: testPubKey},
				{User: "admin", Password: "hunter2"},
			},
			wantErr: "supplied more than once",
		},
		{
			name:    "malformed SSH key",
			creds:   []CredentialInput{{User: "admin", SSHAuthorizedKey: "not-a-key"}},
			wantErr: "not a valid OpenSSH public key line",
		},
		{
			name: "private key pasted instead of public",
			creds: []CredentialInput{{
				User:             "admin",
				SSHAuthorizedKey: "-----BEGIN OPENSSH PRIVATE KEY-----",
			}},
			wantErr: "looks like a private key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateCredentials(tt.creds, parent)
			if err == nil {
				t.Fatalf("validateCredentials accepted %+v, want error", tt.creds)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidateCredentialsHashesPassword is the core secrecy guarantee: a
// plain-text password supplied over the API is replaced by a crypt hash before
// it can be written anywhere.
func TestValidateCredentialsHashesPassword(t *testing.T) {
	requireOpenssl(t)

	parent := templateWithUsers(sudoUser("admin"))
	const plaintext = "correct horse battery staple"

	got, err := validateCredentials([]CredentialInput{
		{User: "admin", Password: plaintext, SSHAuthorizedKey: testPubKey},
	}, parent)
	if err != nil {
		t.Fatalf("validateCredentials: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d credentials, want 1", len(got))
	}
	if got[0].Password == plaintext {
		t.Error("password was passed through as plain text")
	}
	if !config.IsCryptHash(got[0].Password) {
		t.Errorf("password = %q, want a complete crypt(3) hash", got[0].Password)
	}
	if got[0].SSHAuthorizedKey != testPubKey {
		t.Errorf("SSH key was altered: %q", got[0].SSHAuthorizedKey)
	}
}

// TestValidateCredentialsKeepsCryptHash confirms a value that is already a
// hash is not hashed again, which would make it unusable as a password.
func TestValidateCredentialsKeepsCryptHash(t *testing.T) {
	t.Parallel()

	const hash = "$6$rounds=5000$abcdefgh$" +
		"0123456789012345678901234567890123456789012345678901234567890123456789012345678901234a"
	if !config.IsCryptHash(hash) {
		t.Fatalf("test fixture %q is not recognised as a crypt hash", hash)
	}

	got, err := validateCredentials(
		[]CredentialInput{{User: "admin", Password: hash}}, templateWithUsers(sudoUser("admin")))
	if err != nil {
		t.Fatalf("validateCredentials: %v", err)
	}
	if got[0].Password != hash {
		t.Errorf("password = %q, want it left as the supplied hash", got[0].Password)
	}
}

// TestBuildDeltaCarriesCredentials confirms the generated delta declares the
// user, and that the rendered YAML is what a build would act on.
func TestBuildDeltaCarriesCredentials(t *testing.T) {
	t.Parallel()

	sel := Selection{
		Credentials: []CredentialInput{
			{User: "admin", Password: "$6$salt$hash", SSHAuthorizedKey: testPubKey},
		},
	}
	data, err := buildDelta("parent.yml",
		config.ImageInfo{Name: "img", Version: "1.0"},
		config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "iso"},
		sel, nil, nil)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}

	var d deltaTemplate
	if err := yaml.Unmarshal(data, &d); err != nil {
		t.Fatalf("generated delta is not parseable: %v", err)
	}
	if d.SystemConfig == nil || len(d.SystemConfig.Users) != 1 {
		t.Fatalf("delta does not declare exactly one user: %s", data)
	}
	u := d.SystemConfig.Users[0]
	if u.Name != "admin" || u.Password != "$6$salt$hash" {
		t.Errorf("delta user = %+v, want admin with the supplied hash", u)
	}
	if len(u.SSHAuthorizedKeys) != 1 || u.SSHAuthorizedKeys[0] != testPubKey {
		t.Errorf("delta SSH keys = %v, want the supplied key", u.SSHAuthorizedKeys)
	}
	// hash_algo must stay absent: the value is already a hash, and declaring an
	// algorithm would make the build hash it a second time.
	if strings.Contains(string(data), "hash_algo") {
		t.Errorf("delta declares hash_algo:\n%s", data)
	}
}

// TestRedactDeltaYAML covers the copy returned to the caller. The Advanced tab
// renders it with Copy and Export, so a password hash in it would be on screen
// and in a downloaded file.
func TestRedactDeltaYAML(t *testing.T) {
	t.Parallel()

	sel := Selection{
		Credentials: []CredentialInput{
			{User: "admin", Password: "$6$salt$hash", SSHAuthorizedKey: testPubKey},
		},
	}
	data, err := buildDelta("parent.yml",
		config.ImageInfo{Name: "img", Version: "1.0"},
		config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "iso"},
		sel, nil, nil)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}

	// The bytes a build reads keep the real value.
	if !strings.Contains(string(data), "$6$salt$hash") {
		t.Fatalf("delta written for the build lost the password hash:\n%s", data)
	}

	redacted := redactDeltaYAML(data)
	if strings.Contains(redacted, "$6$salt$hash") {
		t.Errorf("redacted delta still contains the password hash:\n%s", redacted)
	}
	if !strings.Contains(redacted, config.RedactedValue) {
		t.Errorf("redacted delta does not mark the password:\n%s", redacted)
	}
	// The public key is not a secret and is what proves the key actually landed.
	if !strings.Contains(redacted, testPubKey) {
		t.Errorf("redacted delta dropped the SSH key:\n%s", redacted)
	}
}

// TestRedactDeltaYAMLPassesThroughWithoutUsers confirms the common case — a
// delta with no credentials — is returned byte-for-byte rather than re-rendered.
func TestRedactDeltaYAMLPassesThroughWithoutUsers(t *testing.T) {
	t.Parallel()

	data, err := buildDelta("parent.yml",
		config.ImageInfo{Name: "img", Version: "1.0"},
		config.TargetInfo{OS: "ubuntu", Dist: "ubuntu24", Arch: "x86_64", ImageType: "iso"},
		Selection{Packages: []string{"curl"}}, nil, nil)
	if err != nil {
		t.Fatalf("buildDelta: %v", err)
	}
	if got := redactDeltaYAML(data); got != string(data) {
		t.Errorf("delta without credentials was altered:\ngot:\n%s\nwant:\n%s", got, data)
	}
}

// TestDeltaUsersSorted confirms the same selection always renders the same
// bytes, which is what makes the Review pane's delta diffable.
func TestDeltaUsersSorted(t *testing.T) {
	t.Parallel()

	got := deltaUsers([]CredentialInput{
		{User: "zoe", Password: "$6$a$b"},
		{User: "admin", Password: "$6$c$d"},
	})
	if len(got) != 2 || got[0].Name != "admin" || got[1].Name != "zoe" {
		t.Errorf("deltaUsers = %+v, want sorted by name", got)
	}
}

func requireOpenssl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not installed; password hashing cannot be exercised")
	}
}
