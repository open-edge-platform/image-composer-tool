// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestRetryBuildResubmitsHashedCredentials is the regression guard for the bug
// this feature fixes: retrying a build that only failed for a reason unrelated
// to its configuration (a transient network timeout, say) must not drop the
// credential that originally satisfied a sudo account the curated template
// ships with no login — reconstructing a bare ComposeRequest from the
// summary, as the UI used to do, loses it and the retry is rejected with
// "credential required".
func TestRetryBuildResubmitsHashedCredentials(t *testing.T) {
	requireOpenssl(t)
	s := newTestService(t)

	// robotics.yml here ships a sudo account with no login — the curated
	// unattended-ISO pattern the reported bug hit.
	sudoTemplate := `image:
  name: test-image
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: iso
systemConfig:
  users:
    - name: admin
      sudo: true
`
	if err := os.WriteFile(filepath.Join(s.cfg.TemplatesDir, "robotics.yml"), []byte(sudoTemplate), 0o644); err != nil {
		t.Fatalf("writing template: %v", err)
	}

	acc, err := s.StartBuild(BuildRequest{Compose: &Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
		Credentials: []CredentialInput{{User: "admin", Password: "correct horse battery staple"}},
	}})
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	// There is no real ict binary in a unit test, so the original build's
	// background process is `/bin/true` — wait for it to finish (finish()
	// archives its template and rewrites TemplatePath under b.mu) before
	// touching anything else, then free the slot for the retry.
	origBuild, ok := s.getBuild(acc.BuildID)
	if !ok {
		t.Fatalf("original build %s not tracked", acc.BuildID)
	}
	<-origBuild.done
	s.releaseBuildSlot(acc.BuildID)

	retryAcc, err := s.RetryBuild(acc.BuildID)
	if err != nil {
		t.Fatalf("RetryBuild: %v", err)
	}
	if retryAcc.BuildID == acc.BuildID {
		t.Fatal("retry must start a new build, not reuse the original id")
	}

	b, ok := s.getBuild(retryAcc.BuildID)
	if !ok {
		t.Fatalf("retried build %s not tracked", retryAcc.BuildID)
	}
	// Wait for the retried build's own background process the same way, then
	// read its archived path through snapshot() rather than the field directly
	// — finish() rewrites TemplatePath under b.mu as it archives the delta.
	// Read the raw YAML directly rather than through the config package's
	// validating loaders: re-validating a flattened document against the full
	// image-template schema is a merge-time concern unrelated to this test, and
	// fails here only because a unit test has no config/osv tree to fill in
	// bootloader defaults from.
	<-b.done
	data, rerr := os.ReadFile(b.snapshot().TemplatePath)
	if rerr != nil {
		t.Fatalf("reading retried build's template: %v", rerr)
	}
	var doc struct {
		SystemConfig struct {
			Users []struct {
				Name     string `yaml:"name"`
				Password string `yaml:"password"`
			} `yaml:"users"`
		} `yaml:"systemConfig"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing retried build's template: %v", err)
	}
	for _, u := range doc.SystemConfig.Users {
		if u.Name == "admin" && u.Password == "" {
			t.Error("retried build's admin account still has no password; the retry dropped the credential")
		}
	}
}

// TestRetryInputSurvivesMetaRoundTrip is the regression guard for a build
// retried after a server restart: writeMeta/buildFromMeta did not carry the
// retry record, so a build created with retry support still read back as
// b.retry == nil and RetryBuild rejected it as predating that support.
func TestRetryInputSurvivesMetaRoundTrip(t *testing.T) {
	root := t.TempDir()
	b := &build{
		ID:      "rt1",
		RootDir: root,
		retry: &retryInput{
			Compose: &Selection{Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso"},
		},
		done: make(chan struct{}),
	}
	b.finish(StatusSuccess, nil, "")
	if err := b.writeMeta(); err != nil {
		t.Fatalf("writeMeta: %v", err)
	}

	s := newTestService(t)
	reloaded, ok := loadMetaInto(t, s, root, "rt1")
	if !ok {
		t.Fatal("build could not be reconstructed from meta.json")
	}
	if reloaded.retry == nil || reloaded.retry.Compose == nil {
		t.Fatal("retry record did not survive the meta.json round trip")
	}
	if reloaded.retry.Compose.Vertical != "robotics" {
		t.Errorf("reconstructed retry.Compose.Vertical = %q, want robotics", reloaded.retry.Compose.Vertical)
	}
}

// TestRetryBuildRejectsUnknownOrLegacyBuild covers the two ways a retry can't
// proceed: a build id the tracker has never heard of, and one reconstructed
// from a meta.json written before retry support existed (no retry record to
// resubmit).
func TestRetryBuildRejectsUnknownOrLegacyBuild(t *testing.T) {
	s := newTestService(t)

	if _, err := s.RetryBuild("no-such-build"); err == nil {
		t.Fatal("expected an error retrying an unknown build")
	}

	b := &build{ID: "legacy", status: StatusFailed, done: make(chan struct{})}
	close(b.done)
	s.tracker.add(b)
	if _, err := s.RetryBuild("legacy"); err == nil {
		t.Fatal("expected an error retrying a build with no retry record")
	}
}
