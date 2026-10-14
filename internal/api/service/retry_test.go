// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
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
	// Free the slot: there is no real ict binary in a unit test, so the
	// original build's background process is irrelevant — only the stored
	// retry record is under test here.
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
	merged, merr := config.LoadAndMergeTemplate(b.TemplatePath)
	if merr != nil {
		t.Fatalf("merging retried build's template: %v", merr)
	}
	for _, u := range merged.SystemConfig.Users {
		if u.Name == "admin" && u.NeedsCredential() {
			t.Error("retried build's admin account still needs a credential; the retry dropped it")
		}
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
