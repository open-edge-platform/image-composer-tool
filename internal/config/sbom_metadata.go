// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"gopkg.in/yaml.v3"
)

// SBOMMetadataFileName is the sidecar written beside template-dump.yaml, both in
// the image build directory and inside an installer ISO.
//
// It carries the per-package SBOM metadata (supplier, checksum, licence, URL)
// that an installer ISO's live-installer needs to emit a full SPDX document for
// the system it just installed. That payload used to be folded into
// template-dump.yaml itself, which made the dump thousands of lines long and
// unreadable as a template — the thing it is otherwise a faithful copy of. It
// lives here instead so the dump stays a plain template and the installer keeps
// its metadata.
const SBOMMetadataFileName = "sbom-metadata.yaml"

// sbomMetadataSidecar is the sidecar's on-disk shape. The key matches the
// template field this payload used to occupy, so the file reads the same as the
// block it replaced.
type sbomMetadataSidecar struct {
	SBOMPackageMetadata []ospackage.PackageInfo `yaml:"sbomPackageMetadata"`
}

// WriteSBOMMetadataSidecar writes the package metadata to path. Writing an empty
// list is not an error — an installer reading it back simply falls through to
// its own inventory scan.
func WriteSBOMMetadataSidecar(path string, pkgs []ospackage.PackageInfo) error {
	data, err := yaml.Marshal(sbomMetadataSidecar{SBOMPackageMetadata: pkgs})
	if err != nil {
		return fmt.Errorf("marshaling SBOM metadata sidecar: %w", err)
	}
	if err := os.WriteFile(filepath.Clean(path), data, 0o644); err != nil { //nolint:gosec // travels inside the ISO, read by the installer
		return fmt.Errorf("writing SBOM metadata sidecar %s: %w", path, err)
	}
	return nil
}

// ReadSBOMMetadataSidecar reads the package metadata written beside a template.
// A missing file returns (nil, nil): an ISO built before the sidecar existed
// carries its metadata inline in the template instead, and the caller falls back
// to that.
func ReadSBOMMetadataSidecar(path string) ([]ospackage.PackageInfo, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading SBOM metadata sidecar %s: %w", path, err)
	}
	var sidecar sbomMetadataSidecar
	if err := yaml.Unmarshal(data, &sidecar); err != nil {
		return nil, fmt.Errorf("parsing SBOM metadata sidecar %s: %w", path, err)
	}
	return sidecar.SBOMPackageMetadata, nil
}
