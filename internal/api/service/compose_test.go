// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// TestBuildComposeSummary_KernelVersion confirms KernelVersion prefers
// overlayPolicy.replaceKernel.version (overlay mode doesn't populate
// systemConfig.kernel) and otherwise falls back to systemConfig.kernel.version.
func TestBuildComposeSummary_KernelVersion(t *testing.T) {
	t.Run("create mode uses systemConfig.kernel.version", func(t *testing.T) {
		merged := &config.ImageTemplate{
			SystemConfig: config.SystemConfig{Kernel: config.KernelConfig{Version: "6.8.0-40-generic"}},
		}
		got := buildComposeSummary(Selection{}, merged)
		if got.KernelVersion != "6.8.0-40-generic" {
			t.Errorf("KernelVersion = %q, want %q", got.KernelVersion, "6.8.0-40-generic")
		}
	})

	t.Run("overlay mode with replaceKernel.version set", func(t *testing.T) {
		merged := &config.ImageTemplate{
			OverlayPolicy: &config.OverlayPolicy{
				ReplaceKernel: &config.ReplaceKernel{
					Package: "linux-image-6.11.0-1004-oem",
					Version: "6.11.0-1004-oem",
				},
			},
		}
		got := buildComposeSummary(Selection{}, merged)
		if got.KernelVersion != "6.11.0-1004-oem" {
			t.Errorf("KernelVersion = %q, want %q", got.KernelVersion, "6.11.0-1004-oem")
		}
	})

	t.Run("overlay mode without replaceKernel.version falls back", func(t *testing.T) {
		merged := &config.ImageTemplate{
			SystemConfig: config.SystemConfig{Kernel: config.KernelConfig{Version: "baseline-version"}},
			OverlayPolicy: &config.OverlayPolicy{
				ReplaceKernel: &config.ReplaceKernel{Package: "linux-image-6.11.0-1004-oem"},
			},
		}
		got := buildComposeSummary(Selection{}, merged)
		if got.KernelVersion != "baseline-version" {
			t.Errorf("KernelVersion = %q, want fallback %q", got.KernelVersion, "baseline-version")
		}
	})
}

// TestAdvancedChanges confirms the build summary records each Advanced-tab
// override, and that logins are reduced to account names.
func TestAdvancedChanges(t *testing.T) {
	t.Run("nil without overrides", func(t *testing.T) {
		if got := advancedChanges(Selection{Vertical: "v"}); got != nil {
			t.Fatalf("got %+v, want nil", got)
		}
	})

	sel := Selection{
		ImageName: "custom",
		Packages:  []string{"vim", "curl_8.5.0"},
		Repos:     []string{"intel-gpu"},
		Disk: &DiskOverride{
			Size:               "32GiB",
			PartitionTableType: "gpt",
			Partitions:         []DiskPartitionOverride{{ID: "root", FsType: "ext4", MountPoint: "/"}},
		},
		NewUsers:    []NewUserInput{{User: "alice", Password: "secret"}},
		Credentials: []CredentialInput{{User: "user", SSHAuthorizedKey: "ssh-ed25519 AAAA"}},
	}
	got := advancedChanges(sel)

	t.Run("records every override", func(t *testing.T) {
		if got.ImageName != "custom" || len(got.Packages) != 2 || len(got.Repos) != 1 {
			t.Fatalf("got %+v", got)
		}
		d := got.Disk
		if d == nil || d.Size != "32GiB" || len(d.Partitions) != 1 || d.Partitions[0].MountPoint != "/" {
			t.Fatalf("disk: got %+v", d)
		}
	})

	t.Run("keeps account names only", func(t *testing.T) {
		if len(got.NewUsers) != 1 || got.NewUsers[0] != "alice" {
			t.Fatalf("new users: got %+v", got.NewUsers)
		}
		if len(got.CredentialsFor) != 1 || got.CredentialsFor[0] != "user" {
			t.Fatalf("credentials: got %+v", got.CredentialsFor)
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "secret") || strings.Contains(string(b), "AAAA") {
			t.Fatalf("summary leaks a credential: %s", b)
		}
	})
}
