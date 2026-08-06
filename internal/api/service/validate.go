// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"

	"sigs.k8s.io/yaml"
)

// ValidationIssue is one problem found in a template, tied to a field path.
type ValidationIssue struct {
	Path     string
	Message  string
	Severity string // "error" or "warning"
}

// ValidationResult is the outcome of validating a template. Valid is true when
// there are no error-severity issues. A syntactically broken document (bad YAML)
// still yields Valid=false with a single issue — never a service error, since a
// failed validation is a successful call.
type ValidationResult struct {
	Valid    bool
	Errors   []ValidationIssue
	Warnings []ValidationIssue
}

// ValidateTemplate validates a complete template YAML and returns a structured,
// field-level result. It reports problems rather than failing: a malformed or
// invalid template is a successful call with Valid=false. The only *Error it
// returns is for input the caller could not have intended to validate (empty
// body).
//
// The verdict mirrors what building the same YAML would reach. A build writes
// the YAML to disk and runs ICT's LoadAndMergeTemplate, which validates it as a
// user (minimal) template — the same subschema and the same post-schema semantic
// checks this method runs via validate.ValidateUserTemplateIssues. So a template
// that validates here builds without a validation failure, and one that fails
// here fails the build for the same reason, only reported field-by-field.
func (s *Service) ValidateTemplate(templateYAML string) (*ValidationResult, error) {
	// A body that is empty or only whitespace carries no template to validate;
	// treat it as a malformed request (400), not a validation verdict. Trimming
	// first means "\n" and "   " are rejected the same way "" is, rather than
	// falling through to a "template is empty" 200 issue.
	if strings.TrimSpace(templateYAML) == "" {
		return nil, newError(http.StatusBadRequest, "BAD_REQUEST", "template yaml is required")
	}

	jsonData, converr := yamlToJSONForValidation(templateYAML)
	if converr != "" {
		// Not parseable as a template document: report as a single invalid result,
		// not a server/client error. The user still gets actionable feedback.
		return &ValidationResult{
			Valid:  false,
			Errors: []ValidationIssue{{Message: converr, Severity: validate.SeverityError}},
		}, nil
	}

	issues := validate.ValidateUserTemplateIssues(jsonData)

	res := &ValidationResult{Valid: true}
	for _, iss := range issues {
		vi := ValidationIssue{Path: iss.Path, Message: iss.Message, Severity: iss.Severity}
		if iss.Severity == validate.SeverityWarning {
			res.Warnings = append(res.Warnings, vi)
			continue
		}
		res.Valid = false
		res.Errors = append(res.Errors, vi)
	}
	return res, nil
}

// yamlToJSONForValidation converts template YAML to the JSON bytes the schema
// validators consume, applying the same string-safety limits the build path
// applies in parseYAMLTemplate. On failure it returns an empty []byte and a
// human-readable reason (the two are mutually exclusive) — the reason becomes a
// validation issue rather than an error, so bad YAML is reported, not thrown.
func yamlToJSONForValidation(templateYAML string) ([]byte, string) {
	var raw interface{}
	if err := yaml.Unmarshal([]byte(templateYAML), &raw); err != nil {
		return nil, "invalid YAML: " + err.Error()
	}
	if raw == nil {
		return nil, "template is empty"
	}
	if err := security.ValidateStructStrings(&raw, security.DefaultLimits()); err != nil {
		return nil, "invalid template: " + err.Error()
	}
	jsonData, err := json.Marshal(raw)
	if err != nil {
		return nil, "invalid template: " + err.Error()
	}
	return jsonData, ""
}
