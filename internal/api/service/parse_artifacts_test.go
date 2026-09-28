// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	want := []Artifact{
		{Name: "minimal-os-image-ubuntu-26.04.raw.gz", Type: "image"},
		{Name: "minimal-os-image-ubuntu-26.04.vhdx", Type: "image"},
		{Name: "spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json", Type: "sbom"},
	}
	for i, wnt := range want {
		if got[i].Name != wnt.Name {
			t.Errorf("artifact[%d] name = %q, want %q", i, got[i].Name, wnt.Name)
		}
		if got[i].Type != wnt.Type {
			t.Errorf("artifact[%d] type = %q, want %q", i, got[i].Type, wnt.Type)
		}
		// Path must be a clean absolute path — no leftover logger prefix such as
		// "/display.go:80\t..." and it must end with the artifact name.
		if !strings.HasPrefix(got[i].Path, "/home/") {
			t.Errorf("artifact[%d] path not clean: %q", i, got[i].Path)
		}
		if strings.Contains(got[i].Path, "\t") || strings.Contains(got[i].Path, "display.go") {
			t.Errorf("artifact[%d] path has logger prefix: %q", i, got[i].Path)
		}
		if !strings.HasSuffix(got[i].Path, wnt.Name) {
			t.Errorf("artifact[%d] path %q does not end with name %q", i, got[i].Path, wnt.Name)
		}
	}
}

// TestSnapshotOrdersSBOMLast pins the order the API serves: images first, SBOM
// last. It goes through snapshot() because that is where the ordering is
// applied — which is what makes it hold for a past build reconstructed from a
// meta.json recorded in some other order, as the input here is.
func TestSnapshotOrdersSBOMLast(t *testing.T) {
	b := &build{ID: "s1", done: make(chan struct{})}
	b.finish(StatusSuccess, []Artifact{
		{Name: "spdx_manifest_deb_ubuntu_20260707_165343.json", Type: "sbom", Path: "/b/sbom.json"},
		{Name: "ubuntu24-robotics-amr.raw.gz", Type: "image", Path: "/b/a.raw.gz"},
		{Name: "UPLOAD-MANIFEST.txt", Type: "unknown", Path: "/b/u.txt"},
		{Name: "ubuntu24-robotics-amr.vhdx", Type: "image", Path: "/b/a.vhdx"},
	}, "")

	got := b.snapshot().Artifacts
	wantOrder := []string{
		"ubuntu24-robotics-amr.raw.gz",
		"ubuntu24-robotics-amr.vhdx",
		"UPLOAD-MANIFEST.txt",
		"spdx_manifest_deb_ubuntu_20260707_165343.json",
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("expected %d artifacts, got %d: %+v", len(wantOrder), len(got), got)
	}
	for i, want := range wantOrder {
		if got[i].Name != want {
			t.Errorf("artifact[%d] = %q, want %q (full order: %+v)", i, got[i].Name, want, got)
		}
	}
	if last := got[len(got)-1]; last.Type != "sbom" {
		t.Errorf("last artifact type = %q, want %q", last.Type, "sbom")
	}
}

// TestSortArtifactsIsStable checks the within-group guarantee: reordering only
// moves artifacts between groups, so the source's own ordering of two images is
// not shuffled.
func TestSortArtifactsIsStable(t *testing.T) {
	arts := []Artifact{
		{Name: "z-second.iso", Type: "image"},
		{Name: "sbom.spdx.json", Type: "sbom"},
		{Name: "a-third.iso", Type: "image"},
	}
	sortArtifacts(arts)

	want := []string{"z-second.iso", "a-third.iso", "sbom.spdx.json"}
	for i, w := range want {
		if arts[i].Name != w {
			t.Errorf("artifact[%d] = %q, want %q (full: %+v)", i, arts[i].Name, w, arts)
		}
	}
}

// TestParseArtifactsUnclassifiedIsNotImage covers a bullet line this parser
// cannot place — an older ICT, or one whose summary lists working files. Such a
// line must be reported as "unknown", never guessed to be an image.
func TestParseArtifactsUnclassifiedIsNotImage(t *testing.T) {
	logs := []string{
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:79	    • debian13-x86_64-desktop-virtualization-13.0.iso (966.42 MB)`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:80	      /var/tmp/ict/builds/abc/imagebuild/debian13-x86_64-desktop-virtualization-13.0.iso`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:79	    • UPLOAD-MANIFEST.txt (3.81 kB)`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:80	      /var/tmp/ict/builds/abc/imagebuild/UPLOAD-MANIFEST.txt`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:79	    • debian_version (5 B)`,
		`2026-07-07T16:54:30.684Z	INFO	display/display.go:80	      /var/tmp/ict/builds/abc/imagebuild/debian_version`,
	}

	got := parseArtifacts(logs)
	want := map[string]string{
		"debian13-x86_64-desktop-virtualization-13.0.iso": "image",
		"UPLOAD-MANIFEST.txt":                             "unknown",
		"debian_version":                                  "unknown",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d artifacts, got %d: %+v", len(want), len(got), got)
	}
	for _, a := range got {
		if want[a.Name] != a.Type {
			t.Errorf("artifact %q type = %q, want %q", a.Name, a.Type, want[a.Name])
		}
	}
}

// TestDiscoverArtifactsRealBuildDirectory covers the directory-scan fallback
// against the names a real build leaves behind. It must find the SBOM under its
// actual name (spdx_manifest_*.json — which the old ".spdx.json" suffix rule
// missed entirely, dropping the SBOM) and must not report the chroot's working
// files. See TestDiscoverArtifacts in service_test.go for the nested-layout case.
func TestDiscoverArtifactsRealBuildDirectory(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"minimal-os-image-ubuntu-26.04.raw.gz",
		"spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json",
		"bash.bashrc",
		"debconf.conf",
		"debian_version",
		"chrootpkgs-pkgCache.dot",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	got := discoverArtifacts(dir)
	want := map[string]string{
		"minimal-os-image-ubuntu-26.04.raw.gz":                           "image",
		"spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json": "sbom",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d artifacts, got %d: %+v", len(want), len(got), got)
	}
	for _, a := range got {
		wantType, ok := want[a.Name]
		if !ok {
			t.Errorf("unexpected artifact %q (a build working file is not an output)", a.Name)
			continue
		}
		if a.Type != wantType {
			t.Errorf("artifact %q type = %q, want %q", a.Name, a.Type, wantType)
		}
		if a.Path != filepath.Join(dir, a.Name) {
			t.Errorf("artifact %q path = %q, want %q", a.Name, a.Path, filepath.Join(dir, a.Name))
		}
	}
}
