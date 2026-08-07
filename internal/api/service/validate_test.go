// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"errors"
	"net/http"
	"testing"
)

const validUserTemplate = `image:
  name: my-image
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw
`

// ValidateTemplate needs no manifest/config/ICT — it only exercises the schema
// validators, so a zero-value Service is sufficient.
func newValidateService() *Service { return &Service{} }

func TestValidateTemplate_Valid(t *testing.T) {
	res, err := newValidateService().ValidateTemplate(validUserTemplate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Valid {
		t.Fatalf("expected valid, got invalid with errors: %+v", res.Errors)
	}
	if len(res.Errors) != 0 {
		t.Errorf("expected no errors, got %+v", res.Errors)
	}
}

func TestValidateTemplate_InvalidReportsFieldPaths(t *testing.T) {
	const bad = `image:
  name: "-bad-name"
  version: "1.0"
target:
  os: not-an-os
  imageType: qcow2
`
	res, err := newValidateService().ValidateTemplate(bad)
	if err != nil {
		t.Fatalf("invalid template must not error (it is a successful call): %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid")
	}
	if len(res.Errors) < 3 {
		t.Fatalf("expected several field errors, got %d: %+v", len(res.Errors), res.Errors)
	}
	wantPaths := map[string]bool{"image.name": false, "target.os": false, "target.imageType": false}
	for _, e := range res.Errors {
		if _, ok := wantPaths[e.Path]; ok {
			wantPaths[e.Path] = true
		}
		if e.Message == "" || e.Severity != "error" {
			t.Errorf("issue %+v: want non-empty message and severity=error", e)
		}
	}
	for p, seen := range wantPaths {
		if !seen {
			t.Errorf("expected an error for field path %q", p)
		}
	}
}

// Malformed YAML is reported as an invalid result, not a service error — a
// failed validation is still a successful call.
func TestValidateTemplate_MalformedYAMLIsInvalidNotError(t *testing.T) {
	res, err := newValidateService().ValidateTemplate("image: [unterminated\n")
	if err != nil {
		t.Fatalf("malformed YAML must not return an error: %v", err)
	}
	if res.Valid {
		t.Fatal("expected invalid for malformed YAML")
	}
	if len(res.Errors) != 1 {
		t.Fatalf("expected a single parse issue, got %+v", res.Errors)
	}
}

// An empty or whitespace-only body is the one input that is a client error:
// there is nothing to validate, so it is a 400 rather than a (meaningless)
// invalid result. Whitespace-only must be rejected the same way as "" — not
// fall through to a "template is empty" 200 issue.
func TestValidateTemplate_EmptyIsBadRequest(t *testing.T) {
	for _, in := range []string{"", "\n", "   ", "  \n\t "} {
		_, err := newValidateService().ValidateTemplate(in)
		var se *Error
		if !errors.As(err, &se) {
			t.Fatalf("input %q: expected *Error, got %v", in, err)
		}
		if se.Status != http.StatusBadRequest {
			t.Errorf("input %q: status = %d, want 400", in, se.Status)
		}
	}
}
