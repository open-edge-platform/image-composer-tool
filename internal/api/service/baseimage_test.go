// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// overlayService is newTestService with robotics.yml made an overlay template
// whose base image is baselinePath.
func overlayService(t *testing.T, baselinePath string) *Service {
	t.Helper()
	s := newTestService(t)
	tmpl := minimalTemplate + "baseline:\n  mode: overlay\n  source:\n    path: " + baselinePath + "\n    format: raw\n"
	if err := os.WriteFile(filepath.Join(s.cfg.TemplatesDir, "robotics.yml"), []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}

// requireBadRequest fails unless err is a 400 BAD_REQUEST service error.
func requireBadRequest(t *testing.T, err error) *Error {
	t.Helper()
	var se *Error
	if !errors.As(err, &se) || se.Status != 400 || se.Code != "BAD_REQUEST" {
		t.Fatalf("error = %v, want 400 BAD_REQUEST", err)
	}
	return se
}

var roboticsRaw = Selection{Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso"}

func TestComposeReportsAMissingBaseImage(t *testing.T) {
	s := overlayService(t, "/path/to/ubuntu-26.04-base.raw")
	res, err := s.Compose(roboticsRaw)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if res.Summary.BaseImage != "/path/to/ubuntu-26.04-base.raw" || !res.Summary.BaseImageMissing {
		t.Errorf("summary base image = %q missing=%t, want the placeholder reported missing", res.Summary.BaseImage, res.Summary.BaseImageMissing)
	}
}

func TestComposeOverridesTheBaseImage(t *testing.T) {
	s := overlayService(t, "/path/to/ubuntu-26.04-base.raw")
	img := filepath.Join(t.TempDir(), "base.raw")
	if err := os.WriteFile(img, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	sel := roboticsRaw
	sel.BaseImage = img
	res, err := s.Compose(sel)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if res.Summary.BaseImage != img || res.Summary.BaseImageMissing {
		t.Errorf("summary base image = %q missing=%t, want the override, found", res.Summary.BaseImage, res.Summary.BaseImageMissing)
	}
	// extends replaces the whole baseline block, so the delta carries all of it.
	if !strings.Contains(res.DeltaYAML, "mode: overlay") || !strings.Contains(res.DeltaYAML, "format: raw") || !strings.Contains(res.DeltaYAML, img) {
		t.Errorf("delta doesn't carry the whole baseline block:\n%s", res.DeltaYAML)
	}
	if res.Summary.Advanced == nil || res.Summary.Advanced.BaseImage != img {
		t.Errorf("advanced changes = %+v, want the base image listed", res.Summary.Advanced)
	}
	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

func TestComposeRefusesABaseImageItCantUse(t *testing.T) {
	// A template not built on a base image.
	sel := roboticsRaw
	sel.BaseImage = "/images/base.raw"
	_, err := newTestService(t).Compose(sel)
	if se := requireBadRequest(t, err); !strings.Contains(se.Message, "overlay") {
		t.Errorf("message = %q, want it to say why", se.Message)
	}
	// A relative path.
	sel.BaseImage = "images/base.raw"
	_, err = overlayService(t, "/images/base.raw").Compose(sel)
	requireBadRequest(t, err)
}

func TestBuildRefusesAMissingBaseImage(t *testing.T) {
	s := overlayService(t, "/path/to/ubuntu-26.04-base.raw")
	sel := roboticsRaw
	if _, _, _, _, err := s.resolveBuildTemplate(&BuildRequest{Compose: &sel}, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "base image /path/to/ubuntu-26.04-base.raw is not on the server") {
		t.Errorf("build of a template whose base image is missing: err = %v, want it refused", err)
	}
	assertNoLeakedDeltas(t, s.cfg.TemplatesDir)
}

func TestComposeTakesAnHTTPSBaseImageURL(t *testing.T) {
	s := overlayService(t, "/path/to/ubuntu-26.04-base.raw")
	sel := roboticsRaw
	sel.BaseImage = "https://images.example.com/ubuntu-26.04-base.raw"
	res, err := s.Compose(sel)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	// Written as baseline.source.url, the template's path dropped.
	if !strings.Contains(res.DeltaYAML, "url: "+sel.BaseImage) || strings.Contains(res.DeltaYAML, "/path/to/") {
		t.Errorf("delta doesn't swap the path for the URL:\n%s", res.DeltaYAML)
	}
	// A URL is downloaded at build time, so it isn't reported missing.
	if res.Summary.BaseImage != sel.BaseImage || res.Summary.BaseImageMissing {
		t.Errorf("summary base image = %q missing=%t", res.Summary.BaseImage, res.Summary.BaseImageMissing)
	}

	// The build's rules: https only, with a host.
	for _, bad := range []string{"http://images.example.com/base.raw", "https://"} {
		sel.BaseImage = bad
		_, err := s.Compose(sel)
		requireBadRequest(t, err)
	}
}
