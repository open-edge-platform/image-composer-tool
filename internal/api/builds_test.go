// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import "testing"

// TestParseArtifacts uses real ICT build output (bullet line with name+size,
// followed by the absolute path line) to verify artifact extraction.
func TestParseArtifacts(t *testing.T) {
	logs := []string{
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:61	  Generated Artifacts (including SBOM):`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:79	    • minimal-os-image-ubuntu-26.04.raw.gz (1.13 GB)`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:80	      /home/user/arodage/image-composer-tool/webui-workspace/builds/0de42c32/ubuntu-ubuntu26-x86_64/imagebuild/minimal/minimal-os-image-ubuntu-26.04.raw.gz`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:79	    • minimal-os-image-ubuntu-26.04.vhdx (1.84 GB)`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:80	      /home/user/arodage/image-composer-tool/webui-workspace/builds/0de42c32/ubuntu-ubuntu26-x86_64/imagebuild/minimal/minimal-os-image-ubuntu-26.04.vhdx`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:79	    • spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json (0.20 MB)`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:80	      /home/user/arodage/image-composer-tool/webui-workspace/builds/0de42c32/ubuntu-ubuntu26-x86_64/imagebuild/minimal/spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json`,
	}

	got := parseArtifacts(logs)
	if len(got) != 3 {
		t.Fatalf("expected 3 artifacts, got %d: %+v", len(got), got)
	}

	want := []artifact{
		{Name: "minimal-os-image-ubuntu-26.04.raw.gz", Type: "image"},
		{Name: "minimal-os-image-ubuntu-26.04.vhdx", Type: "image"},
		{Name: "spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json", Type: "sbom"},
	}
	for i, w := range want {
		if got[i].Name != w.Name {
			t.Errorf("artifact[%d] name = %q, want %q", i, got[i].Name, w.Name)
		}
		if got[i].Type != w.Type {
			t.Errorf("artifact[%d] type = %q, want %q", i, got[i].Type, w.Type)
		}
		if got[i].Path == "" || got[i].Path[0] != '/' {
			t.Errorf("artifact[%d] path not captured: %q", i, got[i].Path)
		}
	}
}
