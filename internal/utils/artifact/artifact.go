// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package artifact classifies build output files by name.
//
// ICT names its outputs deterministically — image formats by extension, SBOMs by
// an "spdx"/"sbom" marker — so the file name is the only signal the CLI summary
// (internal/utils/display) and the web API (internal/api/service) have in common.
// Both used to carry their own copy of these rules and had already drifted: the
// API's log parser recognised "spdx" while its directory scanner did not, so a
// real spdx_manifest_*.json SBOM was reported by one path and dropped by the other.
// Keeping the rules here is what stops them drifting again.
package artifact

import (
	"path/filepath"
	"slices"
	"strings"
)

// Type is the kind of a build output. The values mirror the ArtifactType enum in
// api/v1/openapi-template-builder.yaml — the web UI displays them verbatim.
type Type string

const (
	// TypeImage is a bootable/attachable disk or optical image.
	TypeImage Type = "image"
	// TypeSBOM is a software bill of materials (SPDX JSON).
	TypeSBOM Type = "sbom"
	// TypeUnknown is an output these rules do not recognise. It is reported as-is
	// rather than guessed at: a format added to ICT later must show up as
	// unclassified instead of silently masquerading as an image.
	TypeUnknown Type = "unknown"
)

// imageFormats are the disk/optical formats ICT can emit: the target.imageType
// values ("raw", "img", "iso") plus every disk.artifacts type the converter
// supports (see DiskOverrideArtifactsType in the API contract). "tar" is the
// wsl2 root filesystem archive.
var imageFormats = []string{
	"raw", "img", "iso", "qcow2", "vhd", "vhdx", "vmdk", "vdi", "tar",
}

// compressions are the disk.artifacts compression suffixes an image format may
// carry (DiskOverrideArtifactsCompression), e.g. "minimal-os.raw.gz".
var compressions = []string{"gz", "gzip", "xz", "zst", "zstd"}

// Classify returns the artifact type for an output file name. It accepts a bare
// name or a full path. Anything it cannot place is TypeUnknown — never TypeImage.
func Classify(name string) Type {
	lower := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	switch {
	case isSBOM(lower):
		return TypeSBOM
	case isImage(lower):
		return TypeImage
	default:
		return TypeUnknown
	}
}

// IsOutput reports whether a file is a recognised build output. Callers that scan
// a directory use this to skip the build's working files — a chroot leaves things
// like bash.bashrc, debconf.conf and debian_version behind, and none of them are
// artifacts the user asked for.
func IsOutput(name string) bool {
	return Classify(name) != TypeUnknown
}

// isSBOM matches the SPDX manifests ICT writes: create mode emits
// "spdx_manifest_<deb|rpm>_<image>_<timestamp>.json" and overlay mode emits
// "<image>.delta.spdx.json" / "<image>.complete.spdx.json".
//
// Every SBOM the tool produces is SPDX JSON, so the .json extension is required
// as well as the name marker. Matching the marker anywhere in the basename
// misclassified two different things: a real image whose own name contains it
// (`image.name` accepts ordinary hyphenated names, so `robotics-sbom` emits
// `robotics-sbom.raw.gz`, reported as an SBOM and sorted into the SBOM group),
// and the build directory's own plumbing — "sbom-metadata.yaml", the
// installer's package metadata sidecar written alongside template-dump.yaml.
func isSBOM(lower string) bool {
	if !strings.HasSuffix(lower, ".json") {
		return false
	}
	return strings.Contains(lower, "sbom") || strings.Contains(lower, "spdx")
}

// isImage matches a known image format extension, allowing one optional
// compression suffix on top of it (".raw.gz", ".qcow2.zst", ".tar.gz").
func isImage(lower string) bool {
	for _, c := range compressions {
		if strings.HasSuffix(lower, "."+c) {
			lower = strings.TrimSuffix(lower, "."+c)
			break
		}
	}
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")
	return ext != "" && slices.Contains(imageFormats, ext)
}
