// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/google/uuid"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
	"gopkg.in/yaml.v3"
)

// imageNameRe mirrors the schema pattern for image.name
// (#/$defs/Image/properties/name): starts and ends with an alphanumeric,
// dashes/underscores allowed in between. Enforced here too because the name
// is embedded in a generated delta before the schema ever sees it, and
// because it lands in build artifact filenames.
var imageNameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-_]*[a-zA-Z0-9])?$`)

// maxImageNameLen bounds the override independently of the schema's general
// string limits: the name becomes part of an artifact filename on disk.
const maxImageNameLen = 64

// ValidateImageName reports whether name is a legal image.name override, or ""
// (not overridden).
func ValidateImageName(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > maxImageNameLen {
		return fmt.Errorf("image name exceeds %d characters", maxImageNameLen)
	}
	if !imageNameRe.MatchString(name) {
		return fmt.Errorf("image name %q must match %s", name, imageNameRe.String())
	}
	return nil
}

// deltaTemplate is the minimal on-disk shape of a generated extends delta.
// Deliberately its own type rather than config.ImageTemplate: that struct's
// Image/Target/SystemConfig/Disk fields mostly lack `omitempty`, so marshaling
// it emits empty collections and zero-valued blocks that the schema's
// additionalProperties:false would then have to tolerate. This type only ever
// carries what Advanced mode currently allows a user to change.
type deltaTemplate struct {
	Extends string            `yaml:"extends"`
	Image   config.ImageInfo  `yaml:"image"`
	Target  config.TargetInfo `yaml:"target"`
}

// buildDelta renders the extends delta for a selection against its curated
// parent, applying the requested overrides. Deterministic: the same parent,
// image info, and selection always produce the same bytes, so the file the
// Review step resolves and the file a build runs can be generated
// independently and still agree.
//
// parentTemplate is the manifest's template filename (a bare, already
// safeTemplatePath-validated name) — used as-is as the `extends` value so the
// delta references its parent as a sibling, satisfying the containment guard
// in resolveExtendsParentPath.
func buildDelta(parentTemplate string, parentImage config.ImageInfo, parentTarget config.TargetInfo, sel Selection) ([]byte, error) {
	name := parentImage.Name
	if sel.ImageName != "" {
		name = sel.ImageName
	}
	d := deltaTemplate{
		Extends: parentTemplate,
		Image:   config.ImageInfo{Name: name, Version: parentImage.Version},
		Target:  parentTarget,
	}
	data, err := yaml.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("marshaling delta template: %w", err)
	}
	jsonData, convErr := yamlToJSONForValidation(string(data))
	if convErr != "" {
		return nil, fmt.Errorf("generated delta is not valid YAML: %s", convErr)
	}
	if issues := validate.ValidateUserTemplateIssues(jsonData); hasErrorIssue(issues) {
		return nil, fmt.Errorf("generated delta failed validation: %v", issues)
	}
	return data, nil
}

// hasErrorIssue reports whether any issue is error-severity (warnings are
// tolerated; the delta is machine-generated and minimal by construction, so an
// error here means a generator bug, not a user mistake).
func hasErrorIssue(issues []validate.Issue) bool {
	for _, iss := range issues {
		if iss.Severity == validate.SeverityError {
			return true
		}
	}
	return false
}

// writeDelta writes a generated delta into TemplatesDir under a server-chosen
// name, never a user-derived one. The extends containment guard
// (resolveExtendsParentPath) requires the parent to resolve at or below the
// child's directory, so the delta cannot live in the per-build work
// directory — it must be a sibling of the curated templates it extends.
//
// The returned cleanup func removes the file; callers must invoke it once the
// delta is no longer needed (compose: immediately after use; build: once the
// build finishes and the resolved template has been archived).
func (s *Service) writeDelta(data []byte) (path string, cleanup func(), err error) {
	base, absErr := filepath.Abs(s.cfg.TemplatesDir)
	if absErr != nil {
		return "", nil, fmt.Errorf("resolving templates dir: %w", absErr)
	}
	name := ".ict-adv-" + uuid.NewString() + ".yml"
	full := filepath.Join(base, name)
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return "", nil, fmt.Errorf("writing delta template: %w", err)
	}
	return full, func() { _ = os.Remove(full) }, nil
}
