// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
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
