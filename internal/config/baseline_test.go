package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
)

func TestValidateBaseline(t *testing.T) {
	tests := []struct {
		name          string
		baseline      *Baseline
		overlayPolicy *OverlayPolicy
		wantErr       string
		wantNoErr     bool
	}{
		{
			name:      "nil baseline is allowed (equivalent to create)",
			baseline:  nil,
			wantNoErr: true,
		},
		{
			name:      "explicit create with no source is allowed",
			baseline:  &Baseline{Mode: BaselineModeCreate},
			wantNoErr: true,
		},
		{
			name:      "empty mode defaults to create",
			baseline:  &Baseline{},
			wantNoErr: true,
		},
		{
			name:     "create rejects source",
			baseline: &Baseline{Mode: BaselineModeCreate, Source: &BaselineSource{Path: "/tmp/x.raw"}},
			wantErr:  "source must not be set",
		},
		{
			name:          "create rejects overlayPolicy",
			baseline:      &Baseline{Mode: BaselineModeCreate},
			overlayPolicy: &OverlayPolicy{},
			wantErr:       "overlayPolicy must not be set",
		},
		{
			name:          "overlayPolicy without baseline (default create) is rejected",
			baseline:      nil,
			overlayPolicy: &OverlayPolicy{},
			wantErr:       "overlayPolicy must not be set",
		},
		{
			name:     "overlay requires source",
			baseline: &Baseline{Mode: BaselineModeOverlay},
			wantErr:  "source is required",
		},
		{
			name:      "overlay with valid local source path passes",
			baseline:  &Baseline{Mode: BaselineModeOverlay, Source: &BaselineSource{Path: "/tmp/u.raw"}},
			wantNoErr: true,
		},
		{
			name: "overlay accepts https URL source",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{URL: "https://example.com/u.raw"},
			},
			wantNoErr: true,
		},
		{
			name: "overlay rejects plain http URL source",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{URL: "http://example.com/u.raw"},
			},
			wantErr: "must use https",
		},
		{
			name: "overlay rejects URL written into path field",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "https://example.com/u.raw"},
			},
			wantErr: "use baseline.source.url for remote images",
		},
		{
			name: "overlay rejects single-slash scheme in path field",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "file:/tmp/u.raw"},
			},
			wantErr: "use baseline.source.url for remote images",
		},
		{
			name: "overlay rejects non-https URL scheme",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{URL: "file:///tmp/u.raw"},
			},
			wantErr: "must use https",
		},
		{
			name: "overlay rejects https URL with no host",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{URL: "https://"},
			},
			wantErr: "must include a host",
		},
		{
			name: "overlay rejects https URL with query but no host",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{URL: "https://?q=x"},
			},
			wantErr: "must include a host",
		},
		{
			name: "overlay rejects https URL with fragment but no host",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{URL: "https://#frag"},
			},
			wantErr: "must include a host",
		},
		{
			name: "overlay rejects source with neither path nor url",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "   "},
			},
			wantErr: "must set either",
		},
		{
			name: "overlay rejects source with both path and url",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "/tmp/u.raw", URL: "https://example.com/u.raw"},
			},
			wantErr: "must set only one",
		},
		{
			name: "overlay rejects unsupported format",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "/tmp/u.vmdk", Format: "vmdk"},
			},
			wantErr: "baseline.source.format must be one of",
		},
		{
			name: "overlay accepts qcow2 format",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "/tmp/u.qcow2", Format: "qcow2"},
			},
			wantNoErr: true,
		},
		{
			name: "overlay accepts vhd format",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "/tmp/u.vhd", Format: "vhd"},
			},
			wantNoErr: true,
		},
		{
			name: "overlay accepts vhdx format",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "/tmp/u.vhdx", Format: "vhdx"},
			},
			wantNoErr: true,
		},
		{
			// validateBaseline calls Source.Validate(), which lower-cases Format;
			// this documents that normalization for programmatically-built templates.
			// (YAML templates are constrained to the lower-case schema enum earlier.)
			name: "overlay accepts upper-case format via Validate normalization",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "/tmp/u.qcow2", Format: "QCOW2"},
			},
			wantNoErr: true,
		},
		{
			name: "overlay accepts default (empty) format",
			baseline: &Baseline{
				Mode:   BaselineModeOverlay,
				Source: &BaselineSource{Path: "/tmp/u.raw"},
			},
			wantNoErr: true,
		},
		{
			name:          "overlay policy rejects unknown packageOperation",
			baseline:      &Baseline{Mode: BaselineModeOverlay, Source: &BaselineSource{Path: "/tmp/u.raw"}},
			overlayPolicy: &OverlayPolicy{PackageOperation: "destructive"},
			wantErr:       "packageOperation must be",
		},
		{
			name:          "overlay policy rejects unknown conflictPolicy",
			baseline:      &Baseline{Mode: BaselineModeOverlay, Source: &BaselineSource{Path: "/tmp/u.raw"}},
			overlayPolicy: &OverlayPolicy{ConflictPolicy: "ignore"},
			wantErr:       "conflictPolicy must be",
		},
		{
			name:          "overlay policy with explicit fail conflictPolicy is allowed",
			baseline:      &Baseline{Mode: BaselineModeOverlay, Source: &BaselineSource{Path: "/tmp/u.raw"}},
			overlayPolicy: &OverlayPolicy{ConflictPolicy: OverlayConflictPolicyFail},
			wantNoErr:     true,
		},
		{
			name:          "overlay policy with allow-explicit is allowed",
			baseline:      &Baseline{Mode: BaselineModeOverlay, Source: &BaselineSource{Path: "/tmp/u.raw"}},
			overlayPolicy: &OverlayPolicy{ConflictPolicy: OverlayConflictPolicyAllowExplicit},
			wantNoErr:     true,
		},
		{
			name:          "overlay policy accepts additive-and-upgrade",
			baseline:      &Baseline{Mode: BaselineModeOverlay, Source: &BaselineSource{Path: "/tmp/u.raw"}},
			overlayPolicy: &OverlayPolicy{PackageOperation: OverlayPackageOpAdditiveAndUpgrade},
			wantNoErr:     true,
		},
		{
			name:     "unknown mode is rejected",
			baseline: &Baseline{Mode: "rebuild"},
			wantErr:  "baseline.mode must be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := &ImageTemplate{Baseline: tt.baseline, OverlayPolicy: tt.overlayPolicy}
			err := tmpl.validateBaseline()
			if tt.wantNoErr {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

// TestOverlayPolicyDerivesAllowUpgrade confirms validate() derives the internal
// AllowUpgrade gate from packageOperation: on for additive-and-upgrade, off for
// additive-only (and the empty default).
func TestOverlayPolicyDerivesAllowUpgrade(t *testing.T) {
	cases := []struct {
		op   string
		want bool
	}{
		{"", false},
		{OverlayPackageOpAdditiveOnly, false},
		{OverlayPackageOpAdditiveAndUpgrade, true},
	}
	for _, c := range cases {
		p := &OverlayPolicy{PackageOperation: c.op}
		if err := p.validate(); err != nil {
			t.Fatalf("validate(%q): unexpected error %v", c.op, err)
		}
		if p.AllowUpgrade != c.want {
			t.Errorf("packageOperation %q: AllowUpgrade = %v, want %v", c.op, p.AllowUpgrade, c.want)
		}
	}
}

func TestBaselineSourceValidateNormalizesWhitespace(t *testing.T) {
	t.Run("path is trimmed in place", func(t *testing.T) {
		s := &BaselineSource{Path: "  /tmp/u.raw\n"}
		if err := s.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Path != "/tmp/u.raw" {
			t.Errorf("Path = %q, want trimmed %q", s.Path, "/tmp/u.raw")
		}
	})

	t.Run("url is trimmed in place", func(t *testing.T) {
		s := &BaselineSource{URL: "  https://example.com/u.raw  "}
		if err := s.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.URL != "https://example.com/u.raw" {
			t.Errorf("URL = %q, want trimmed %q", s.URL, "https://example.com/u.raw")
		}
	})
}

func TestIsOverlayMode(t *testing.T) {
	cases := []struct {
		name string
		tmpl *ImageTemplate
		want bool
	}{
		{"nil baseline", &ImageTemplate{}, false},
		{"create mode", &ImageTemplate{Baseline: &Baseline{Mode: BaselineModeCreate}}, false},
		{"overlay mode", &ImageTemplate{Baseline: &Baseline{Mode: BaselineModeOverlay}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.tmpl.IsOverlayMode(); got != c.want {
				t.Fatalf("IsOverlayMode = %v, want %v", got, c.want)
			}
		})
	}
}

// TestSchemaAcceptsBaseline verifies the JSON schema recognises the new
// baseline / overlayPolicy fields.
func TestSchemaAcceptsBaseline(t *testing.T) {
	tmpl := `{
		"image": {"name": "ub", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"},
		"baseline": {
			"mode": "overlay",
			"source": {"path": "/tmp/u.raw", "format": "raw"}
		},
		"overlayPolicy": {
			"packageOperation": "additive-only",
			"conflictPolicy": "fail",
			"kernelCmdline": "quiet"
		}
	}`
	if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err != nil {
		t.Fatalf("user template with baseline should validate: %v", err)
	}
}

// TestSchemaRejectsAllowRemoval ensures `allowRemoval` (intentionally absent
// from OverlayPolicy) is rejected by additionalProperties:false.
func TestSchemaRejectsAllowRemoval(t *testing.T) {
	tmpl := `{
		"image": {"name": "ub", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"},
		"baseline": {
			"mode": "overlay",
			"source": {"path": "/tmp/u.raw"}
		},
		"overlayPolicy": {"allowRemoval": true}
	}`
	if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err == nil {
		t.Fatalf("template with allowRemoval should be rejected by schema")
	}
}

// TestSchemaAcceptsSourceURL ensures `source.url` (an http(s) baseline image)
// is accepted by the schema.
func TestSchemaAcceptsSourceURL(t *testing.T) {
	tmpl := `{
		"image": {"name": "ub", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"},
		"baseline": {
			"mode": "overlay",
			"source": {"url": "https://example.com/u.raw"}
		}
	}`
	if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err != nil {
		t.Fatalf("template with baseline.source.url should validate: %v", err)
	}
}

// TestSchemaRejectsUnsupportedFormat ensures the format enum rejects a format
// outside the supported set (raw/qcow2/vhd/vhdx), e.g. vmdk.
func TestSchemaRejectsUnsupportedFormat(t *testing.T) {
	tmpl := `{
		"image": {"name": "ub", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"},
		"baseline": {
			"mode": "overlay",
			"source": {"path": "/tmp/u.vmdk", "format": "vmdk"}
		}
	}`
	if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err == nil {
		t.Fatalf("template with format=vmdk should be rejected by schema")
	}
}

// TestSchemaAcceptsSupportedFormats ensures the format enum accepts every
// supported baseline format.
func TestSchemaAcceptsSupportedFormats(t *testing.T) {
	for _, format := range []string{"raw", "qcow2", "vhd", "vhdx"} {
		t.Run(format, func(t *testing.T) {
			tmpl := fmt.Sprintf(`{
				"image": {"name": "ub", "version": "1.0.0"},
				"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"},
				"baseline": {
					"mode": "overlay",
					"source": {"path": "/tmp/u.img", "format": %q}
				}
			}`, format)
			if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err != nil {
				t.Fatalf("template with format=%s should validate: %v", format, err)
			}
		})
	}
}

// TestSchemaAcceptsCreateMode_NoBaseline verifies that omitting baseline
// (the legacy default) still validates.
func TestSchemaAcceptsCreateMode_NoBaseline(t *testing.T) {
	tmpl := `{
		"image": {"name": "ub", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"}
	}`
	if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err != nil {
		t.Fatalf("template without baseline should validate: %v", err)
	}
}

// TestSchemaRejectsURLInPath ensures the path pattern guard rejects a URL
// (e.g. an https:// value) written into baseline.source.path.
func TestSchemaRejectsURLInPath(t *testing.T) {
	tmpl := `{
		"image": {"name": "ub", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"},
		"baseline": {
			"mode": "overlay",
			"source": {"path": "https://example.com/u.raw"}
		}
	}`
	if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err == nil {
		t.Fatalf("template with a URL in baseline.source.path should be rejected by schema")
	}
}

// TestSchemaEnforcesModeSourceCoupling exercises the schema-layer allOf/if-then
// rules: overlay requires source; create (explicit or defaulted) forbids it.
func TestSchemaEnforcesModeSourceCoupling(t *testing.T) {
	base := `{
		"image": {"name": "ub", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"},
		"baseline": %s
	}`
	cases := []struct {
		name     string
		baseline string
		wantErr  bool
	}{
		{"overlay without source is rejected", `{"mode": "overlay"}`, true},
		{"overlay with source is accepted", `{"mode": "overlay", "source": {"path": "/tmp/u.raw"}}`, false},
		{"create with source is rejected", `{"mode": "create", "source": {"path": "/tmp/u.raw"}}`, true},
		{"create without source is accepted", `{"mode": "create"}`, false},
		{"defaulted mode with source is rejected", `{"source": {"path": "/tmp/u.raw"}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl := fmt.Sprintf(base, c.baseline)
			err := validate.ValidateUserTemplateJSON([]byte(tmpl))
			if c.wantErr && err == nil {
				t.Fatalf("expected schema rejection, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected schema acceptance, got %v", err)
			}
		})
	}
}
