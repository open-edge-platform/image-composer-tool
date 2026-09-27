package config

import (
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config/validate"
)

func TestIsInstallerPayloadMode(t *testing.T) {
	cases := []struct {
		name string
		tmpl *ImageTemplate
		want bool
	}{
		{"nil installerPayload", &ImageTemplate{Target: TargetInfo{ImageType: "iso"}}, false},
		{
			"iso and enabled",
			&ImageTemplate{
				Target:       TargetInfo{ImageType: "iso"},
				SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Enabled: true}},
			},
			true,
		},
		{
			"iso and disabled",
			&ImageTemplate{
				Target:       TargetInfo{ImageType: "iso"},
				SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Enabled: false}},
			},
			false,
		},
		{
			"raw imageType with enabled block is not payload mode",
			&ImageTemplate{
				Target:       TargetInfo{ImageType: "raw"},
				SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Enabled: true}},
			},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.tmpl.IsInstallerPayloadMode(); got != c.want {
				t.Fatalf("IsInstallerPayloadMode = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPayloadCompression(t *testing.T) {
	cases := []struct {
		name string
		tmpl *ImageTemplate
		want string
	}{
		{"nil installerPayload defaults to zstd", &ImageTemplate{}, PayloadCompressionZstd},
		{"empty compression defaults to zstd", &ImageTemplate{SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{}}}, PayloadCompressionZstd},
		{
			"explicit xz",
			&ImageTemplate{SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Compression: PayloadCompressionXz}}},
			PayloadCompressionXz,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.tmpl.PayloadCompression(); got != c.want {
				t.Fatalf("PayloadCompression = %q, want %q", got, c.want)
			}
		})
	}
}

func TestResetInstanceIdentity(t *testing.T) {
	falseVal := false
	cases := []struct {
		name string
		tmpl *ImageTemplate
		want bool
	}{
		{"nil installerPayload defaults true", &ImageTemplate{}, true},
		{"unset field defaults true", &ImageTemplate{SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{}}}, true},
		{
			"explicit false",
			&ImageTemplate{SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{ResetInstanceIdentity: &falseVal}}},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.tmpl.ResetInstanceIdentity(); got != c.want {
				t.Fatalf("ResetInstanceIdentity = %v, want %v", got, c.want)
			}
		})
	}
}

func TestValidateInstallerPayload(t *testing.T) {
	tests := []struct {
		name      string
		tmpl      *ImageTemplate
		wantErr   string
		wantNoErr bool
	}{
		{
			name:      "nil installerPayload is allowed",
			tmpl:      &ImageTemplate{Target: TargetInfo{ImageType: "raw"}},
			wantNoErr: true,
		},
		{
			name: "iso with enabled payload is allowed",
			tmpl: &ImageTemplate{
				Target:       TargetInfo{ImageType: "iso"},
				SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Enabled: true}},
			},
			wantNoErr: true,
		},
		{
			name: "raw imageType with installerPayload is rejected",
			tmpl: &ImageTemplate{
				Target:       TargetInfo{ImageType: "raw"},
				SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Enabled: true}},
			},
			wantErr: "only supported when target.imageType",
		},
		{
			name: "extends deferred: no imageType restriction yet",
			tmpl: &ImageTemplate{
				Extends:      "parent.yml",
				SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Enabled: true}},
			},
			wantNoErr: true,
		},
		{
			name: "installerPayload with overlay baseline is rejected",
			tmpl: &ImageTemplate{
				Target:       TargetInfo{ImageType: "iso"},
				Baseline:     &Baseline{Mode: BaselineModeOverlay},
				SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Enabled: true}},
			},
			wantErr: "not supported with baseline.mode",
		},
		{
			name: "unsupported compression is rejected",
			tmpl: &ImageTemplate{
				Target:       TargetInfo{ImageType: "iso"},
				SystemConfig: SystemConfig{InstallerPayload: &InstallerPayload{Enabled: true, Compression: "bogus"}},
			},
			wantErr: "not supported: must be one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tmpl.validateInstallerPayload()
			if tt.wantNoErr {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// TestSchemaAcceptsInstallerPayload verifies the JSON schema recognises the new
// systemConfig.installerPayload field on an iso template.
func TestSchemaAcceptsInstallerPayload(t *testing.T) {
	tmpl := `{
		"image": {"name": "payload-test-image", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "iso"},
		"systemConfig": {
			"name": "payload-test-image",
			"description": "payload test stack",
			"bootloader": {"bootType": "efi", "provider": "grub2"},
			"packages": [],
			"additionalFiles": [],
			"configurations": [],
			"installerPayload": {"enabled": true, "compression": "zstd"}
		}
	}`
	if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err != nil {
		t.Fatalf("user template with installerPayload on iso should validate: %v", err)
	}
}

// TestSchemaRejectsInstallerPayloadOnNonISO verifies the JSON schema rejects
// installerPayload on a non-iso imageType.
func TestSchemaRejectsInstallerPayloadOnNonISO(t *testing.T) {
	tmpl := `{
		"image": {"name": "payload-test-image", "version": "1.0.0"},
		"target": {"os": "ubuntu", "dist": "ubuntu24", "arch": "x86_64", "imageType": "raw"},
		"systemConfig": {
			"name": "payload-test-image",
			"description": "payload test stack",
			"bootloader": {"bootType": "efi", "provider": "grub2"},
			"packages": [],
			"additionalFiles": [],
			"configurations": [],
			"installerPayload": {"enabled": true}
		}
	}`
	if err := validate.ValidateUserTemplateJSON([]byte(tmpl)); err == nil {
		t.Fatalf("user template with installerPayload on imageType=raw should be rejected")
	}
}
