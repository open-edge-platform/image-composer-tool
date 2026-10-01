// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package isomaker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

func pkg(name string) ospackage.PackageInfo {
	return ospackage.PackageInfo{Name: name, Type: "rpm", Version: "1.0", URL: "https://example.invalid/" + name}
}

// The sidecar exists so template-dump.yaml stays a readable template. That only
// holds if the inline block is cleared as well as skipped — a template that
// arrives carrying one would otherwise have it written straight back out.
func TestTakeSBOMMetadata(t *testing.T) {
	tests := []struct {
		name    string
		bom     []ospackage.PackageInfo
		inline  []ospackage.PackageInfo
		wantOut []string
	}{
		{
			name:    "a normal build takes the payload from the live BOM",
			bom:     []ospackage.PackageInfo{pkg("curl")},
			wantOut: []string{"curl"},
		},
		{
			name:    "an inline block is used when the live BOM is empty",
			inline:  []ospackage.PackageInfo{pkg("bash")},
			wantOut: []string{"bash"},
		},
		{
			name:    "the live BOM wins over a stale inline block",
			bom:     []ospackage.PackageInfo{pkg("curl")},
			inline:  []ospackage.PackageInfo{pkg("stale")},
			wantOut: []string{"curl"},
		},
		{
			name:    "neither present yields no payload",
			wantOut: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			template := &config.ImageTemplate{FullPkgListBom: tc.bom, SBOMPackageMetadata: tc.inline}

			got := takeSBOMMetadata(template)

			if len(got) != len(tc.wantOut) {
				t.Fatalf("payload = %d packages, want %d", len(got), len(tc.wantOut))
			}
			for i, want := range tc.wantOut {
				if got[i].Name != want {
					t.Errorf("payload[%d] = %q, want %q", i, got[i].Name, want)
				}
			}
			if template.SBOMPackageMetadata != nil {
				t.Errorf("inline sbomPackageMetadata left on the template: %+v", template.SBOMPackageMetadata)
			}
		})
	}
}

// What the caller ultimately cares about: the dump the installer carries has no
// sbomPackageMetadata block in it, whatever the input template had.
func TestTakeSBOMMetadataKeepsDumpFreeOfInlineBlock(t *testing.T) {
	template := &config.ImageTemplate{SBOMPackageMetadata: []ospackage.PackageInfo{pkg("bash"), pkg("curl")}}

	pkgs := takeSBOMMetadata(template)
	if len(pkgs) != 2 {
		t.Fatalf("payload = %d packages, want 2", len(pkgs))
	}

	dumpPath := filepath.Join(t.TempDir(), "template-dump.yaml")
	if err := template.SaveUpdatedConfigFile(dumpPath); err != nil {
		t.Fatalf("SaveUpdatedConfigFile: %v", err)
	}
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("reading dump: %v", err)
	}
	if strings.Contains(string(data), "sbomPackageMetadata") {
		t.Errorf("dump still carries an inline sbomPackageMetadata block:\n%s", data)
	}
}
