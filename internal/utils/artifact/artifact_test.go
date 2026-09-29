// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package artifact_test

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/artifact"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		want artifact.Type
	}{
		// Images: every target.imageType and disk.artifacts format.
		{"raw", "minimal-os-image-ubuntu-26.04.raw", artifact.TypeImage},
		{"raw gzipped", "minimal-os-image-ubuntu-26.04.raw.gz", artifact.TypeImage},
		{"iso", "debian13-x86_64-desktop-virtualization-13.0.iso", artifact.TypeImage},
		{"img", "azl3-x86_64-edge-1.0.img", artifact.TypeImage},
		{"qcow2", "edge-1.0.qcow2", artifact.TypeImage},
		{"qcow2 zstd", "edge-1.0.qcow2.zst", artifact.TypeImage},
		{"vhdx", "minimal-os-image-ubuntu-26.04.vhdx", artifact.TypeImage},
		{"vhd", "edge-1.0.vhd", artifact.TypeImage},
		{"vmdk", "edge-1.0.vmdk", artifact.TypeImage},
		{"vdi", "edge-1.0.vdi", artifact.TypeImage},
		{"wsl2 tar archive", "my-custom-ubuntu-1.0.tar.gz", artifact.TypeImage},
		{"uppercase extension", "EDGE-1.0.ISO", artifact.TypeImage},
		{"full path", "/var/tmp/ict/builds/abc/minimal/image.raw.gz", artifact.TypeImage},

		// SBOMs: the create-mode manifest and both overlay sidecars.
		{"create-mode deb manifest", "spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json", artifact.TypeSBOM},
		{"create-mode rpm manifest", "spdx_manifest_rpm_azl3-x86_64-edge_20260706_130245.json", artifact.TypeSBOM},
		{"overlay delta sidecar", "edge-1.0.delta.spdx.json", artifact.TypeSBOM},
		{"overlay complete sidecar", "edge-1.0.complete.spdx.json", artifact.TypeSBOM},
		{"sbom marker", "image-sbom.json", artifact.TypeSBOM},

		// An image whose own name carries an SBOM marker is still an image: the
		// marker alone is not enough, the file has to be SPDX JSON.
		{"image named for its sbom", "robotics-sbom.raw.gz", artifact.TypeImage},
		{"iso named for its sbom", "robotics-sbom.iso", artifact.TypeImage},
		{"image with spdx in the name", "my-spdx-image.raw", artifact.TypeImage},
		{"sbom of an image named for its sbom",
			"spdx_manifest_deb_robotics-sbom_20260707_165343.json", artifact.TypeSBOM},

		// Working files a chroot leaves in the build directory. These are the
		// names the web UI used to report as IMAGE.
		{"chroot bashrc", "bash.bashrc", artifact.TypeUnknown},
		{"chroot debconf", "debconf.conf", artifact.TypeUnknown},
		{"chroot version stamp", "debian_version", artifact.TypeUnknown},
		{"chroot blacklist", "bindresvport.blacklist", artifact.TypeUnknown},
		{"package cache graph", "chrootpkgs-pkgCache.dot", artifact.TypeUnknown},
		{"upload manifest", "UPLOAD-MANIFEST.txt", artifact.TypeUnknown},
		{"overlay inspect report", "edge-1.0.inspect.txt", artifact.TypeUnknown},
		{"template dump", "template-dump.yaml", artifact.TypeUnknown},
		{"version-like suffix", "libfoo-1.0", artifact.TypeUnknown},
		{"empty", "", artifact.TypeUnknown},

		// A compression suffix alone is not an image.
		{"bare tarball of logs", "logs.gz", artifact.TypeUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := artifact.Classify(tc.file); got != tc.want {
				t.Errorf("Classify(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

// TestClassifyNeverGuessesImage is the regression guard for the reported bug:
// an unrecognised file must never be labelled an image.
func TestClassifyNeverGuessesImage(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"bash.bashrc", "debconf.conf", "debian_version", "UPLOAD-MANIFEST.txt",
		"bindresvport.blacklist", "chrootpkgs-pkgCache.dot", "some.future.format",
	} {
		if got := artifact.Classify(name); got == artifact.TypeImage {
			t.Errorf("Classify(%q) = %q, want anything but %q", name, got, artifact.TypeImage)
		}
	}
}

func TestIsOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		file string
		want bool
	}{
		{"image.raw.gz", true},
		{"spdx_manifest_deb_minimal_20260707_165343.json", true},
		{"bash.bashrc", false},
		{"debian_version", false},
	}

	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			if got := artifact.IsOutput(tc.file); got != tc.want {
				t.Errorf("IsOutput(%q) = %v, want %v", tc.file, got, tc.want)
			}
		})
	}
}
