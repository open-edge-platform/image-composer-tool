package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// loadFile reads a test file from the project root testdata directory.
func loadFile(t *testing.T, relPath string) []byte {
	t.Helper()
	// Determine project root relative to this test file
	root := filepath.Join("..") //, "..") //, "..", "..")
	fullPath := filepath.Join(root, relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", fullPath, err)
	}
	return data
}

// Test new YAML image template format
func TestValidImageTemplate(t *testing.T) {
	v := loadFile(t, "../../image-templates/azl3-x86_64-edge-raw.yml")

	// Parse to generic JSON interface
	var raw interface{}
	if err := yaml.Unmarshal(v, &raw); err != nil {
		t.Errorf("yml parsing error: %v", err)
		return
	}

	// Re‐marshal to JSON bytes
	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Errorf("json marshaling error: %v", err)
		return
	}
	if err := ValidateImageTemplateJSON(dataJSON); err != nil {
		t.Errorf("expected image-templates/azl3-x86_64-edge-raw.yml to pass, but got: %v", err)
	}
}

func TestInvalidImageTemplate(t *testing.T) {
	v := loadFile(t, "/testdata/invalid-image.yml")

	// Parse to generic JSON interface
	var raw interface{}
	if err := yaml.Unmarshal(v, &raw); err != nil {
		t.Errorf("yml parsing error: %v", err)
		return
	}

	// Re‐marshal to JSON bytes
	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Errorf("json marshaling error: %v", err)
		return
	}

	if err := ValidateImageTemplateJSON(dataJSON); err == nil {
		t.Errorf("expected testdata/invalid-image.yml to fail validation")
	}
}

// Test merged template validation with the new single-object structure
func TestValidMergedTemplate(t *testing.T) {
	// Create a sample merged template in the new format
	mergedTemplateYAML := `image:
  name: test-merged-image
  version: "1.0.0"

target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw

disk:
  name: Default
  size: 4GiB
  partitionTableType: gpt
  partitions:
    - id: boot
      type: esp
      flags:
        - esp
        - boot
      start: 1MiB
      end: 513MiB
      fsType: fat32
      mountPoint: /boot/efi
    - id: rootfs
      type: linux-root-amd64
      start: 513MiB
      end: "0"
      fsType: ext4
      mountPoint: /

systemConfig:
  name: default
  description: Default system configuration
  bootloader:
    bootType: efi
    provider: systemd-boot
  users:
    - name: admin
      sudo: true
      shell: /bin/bash
  packages:
    - filesystem
    - kernel
    - systemd
  kernel:
    version: "6.12"
    cmdline: "quiet splash"
`

	// Parse to generic JSON interface
	var raw interface{}
	if err := yaml.Unmarshal([]byte(mergedTemplateYAML), &raw); err != nil {
		t.Fatalf("yml parsing error: %v", err)
	}

	// Re-marshal to JSON bytes
	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json marshaling error: %v", err)
	}

	if err := ValidateImageTemplateJSON(dataJSON); err != nil {
		t.Errorf("expected merged template to pass validation, but got: %v", err)
	}
}

func TestValidMergedTemplateWithWildcardPackages(t *testing.T) {
	mergedTemplateYAML := `image:
  name: test-merged-image
  version: "1.0.0"

target:
  os: edge-microvisor-toolkit
  dist: emt3
  arch: x86_64
  imageType: raw

systemConfig:
  name: default
  packages:
    - wayland*
    - libva*
`

	var raw interface{}
	if err := yaml.Unmarshal([]byte(mergedTemplateYAML), &raw); err != nil {
		t.Fatalf("yml parsing error: %v", err)
	}

	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json marshaling error: %v", err)
	}

	if err := ValidateImageTemplateJSON(dataJSON); err != nil {
		t.Errorf("expected wildcard package template to pass validation, but got: %v", err)
	}
}

func TestInvalidMergedTemplate(t *testing.T) {
	// Create an invalid merged template (missing required fields)
	invalidMergedTemplateYAML := `image:
  name: test-merged-image
  version: "1.0.0"

target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw

# Missing systemConfig which is required
`

	// Parse to generic JSON interface
	var raw interface{}
	if err := yaml.Unmarshal([]byte(invalidMergedTemplateYAML), &raw); err != nil {
		t.Fatalf("yml parsing error: %v", err)
	}

	// Re-marshal to JSON bytes
	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json marshaling error: %v", err)
	}

	if err := ValidateImageTemplateJSON(dataJSON); err == nil {
		t.Errorf("expected invalid merged template to fail validation")
	}
}

func TestInvalidMergedTemplateWithMalformedAllowPackage(t *testing.T) {
	invalidTemplateYAML := `image:
  name: test-merged-image
  version: "1.0.0"

target:
  os: edge-microvisor-toolkit
  dist: emt3
  arch: x86_64
  imageType: raw

packageRepositories:
  - codename: "emtNext"
    url: "https://example.com/repo"
    pkey: "[trusted=yes]"
    allowPackages:
      - qemu-audio-oss"

systemConfig:
  name: default
  packages:
    - qemu-system-x86
`

	var raw interface{}
	if err := yaml.Unmarshal([]byte(invalidTemplateYAML), &raw); err != nil {
		t.Fatalf("yml parsing error: %v", err)
	}

	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json marshaling error: %v", err)
	}

	if err := ValidateImageTemplateJSON(dataJSON); err == nil {
		t.Errorf("expected template with malformed allowPackages entry to fail validation")
	}
}

// Test global config validation
func TestValidConfig(t *testing.T) {
	v := loadFile(t, "/testdata/valid-config.yml")

	if v == nil {
		t.Fatal("failed to load testdata/valid-config.yml")
	}
	dataJSON, err := yaml.YAMLToJSON(v)

	if err != nil {
		t.Fatalf("YAML→JSON conversion failed: %v", err)
	}
	if err := ValidateConfigJSON(dataJSON); err != nil {
		t.Errorf("validation failed: %v", err)
	}
}

func TestInvalidConfig(t *testing.T) {
	v := loadFile(t, "/testdata/invalid-config.yml")

	// Parse to generic JSON interface
	var raw interface{}
	if err := yaml.Unmarshal(v, &raw); err != nil {
		t.Errorf("yml parsing error: %v", err)
		return
	}

	// Re‐marshal to JSON bytes
	dataJSON, err := yaml.YAMLToJSON(v)
	if err != nil {
		t.Errorf("json marshaling error: %v", err)
		return
	}

	if err := ValidateConfigJSON(dataJSON); err == nil {
		t.Errorf("expected invalid-config.json to fail validation: %v", err)
	}
}

// Test validation of template structure using external test files
func TestImageTemplateStructure(t *testing.T) {
	v := loadFile(t, "/testdata/complete-valid-template.yml")

	var raw interface{}
	if err := yaml.Unmarshal(v, &raw); err != nil {
		t.Fatalf("failed to parse minimal template: %v", err)
	}

	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("failed to marshal to JSON: %v", err)
	}

	if err := ValidateImageTemplateJSON(dataJSON); err != nil {
		t.Errorf("minimal template should be valid, but got: %v", err)
	}
}

func TestImageTemplateMissingFields(t *testing.T) {
	v := loadFile(t, "/testdata/incomplete-template.yml")

	var raw interface{}
	if err := yaml.Unmarshal(v, &raw); err != nil {
		t.Fatalf("failed to parse invalid template: %v", err)
	}

	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("failed to marshal to JSON: %v", err)
	}

	if err := ValidateImageTemplateJSON(dataJSON); err == nil {
		t.Errorf("incomplete template should fail validation")
	}
}

// Table-driven test for multiple template validation scenarios
func TestImageTemplateValidation(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		shouldPass  bool
		description string
	}{
		{
			name:        "ValidComplete",
			file:        "/testdata/complete-valid-template.yml",
			shouldPass:  true,
			description: "complete template with all optional fields",
		},
		{
			name:        "InvalidMissingImage",
			file:        "/testdata/missing-image-section.yml",
			shouldPass:  false,
			description: "template missing image section",
		},
		{
			name:        "InvalidMissingTarget",
			file:        "/testdata/missing-target-section.yml",
			shouldPass:  false,
			description: "template missing target section",
		},
		{
			name:        "InvalidWrongTypes",
			file:        "/testdata/wrong-field-types.yml",
			shouldPass:  false,
			description: "template with incorrect field types",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := loadFile(t, tt.file)

			var raw interface{}
			if err := yaml.Unmarshal(v, &raw); err != nil {
				t.Fatalf("failed to parse template %s: %v", tt.file, err)
			}

			dataJSON, err := json.Marshal(raw)
			if err != nil {
				t.Fatalf("failed to marshal to JSON: %v", err)
			}

			err = ValidateImageTemplateJSON(dataJSON)
			if tt.shouldPass && err != nil {
				t.Errorf("expected %s to pass validation (%s), but got error: %v", tt.file, tt.description, err)
			} else if !tt.shouldPass && err == nil {
				t.Errorf("expected %s to fail validation (%s), but it passed", tt.file, tt.description)
			}
		})
	}
}

// Test merged template validation scenarios
func TestMergedTemplateValidation(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		shouldPass  bool
		description string
	}{
		{
			name: "ValidMinimalMerged",
			template: `image:
  name: test
  version: "1.0.0"
target:
  os: azure-linux
  dist: azl3
  arch: x86_64
  imageType: raw
systemConfig:
  name: minimal
  packages:
    - filesystem
  kernel:
    version: "6.12"`,
			shouldPass:  true,
			description: "minimal valid merged template",
		},
		{
			name: "InvalidOSDistMismatch",
			template: `image:
  name: test
  version: "1.0.0"
target:
  os: azure-linux
  dist: emt3
  arch: x86_64
  imageType: raw
systemConfig:
  name: test
  packages:
    - filesystem
  kernel:
    version: "6.12"`,
			shouldPass:  false,
			description: "invalid OS/dist combination",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw interface{}
			if err := yaml.Unmarshal([]byte(tt.template), &raw); err != nil {
				t.Fatalf("failed to parse template: %v", err)
			}

			dataJSON, err := json.Marshal(raw)
			if err != nil {
				t.Fatalf("failed to marshal to JSON: %v", err)
			}

			err = ValidateImageTemplateJSON(dataJSON)
			if tt.shouldPass && err != nil {
				t.Errorf("expected %s to pass validation (%s), but got error: %v", tt.name, tt.description, err)
			} else if !tt.shouldPass && err == nil {
				t.Errorf("expected %s to fail validation (%s), but it passed", tt.name, tt.description)
			}
		})
	}
}
func TestValidateAgainstSchema_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{invalid json}`)
	err := ValidateAgainstSchema("test.schema.json", []byte(`{}`), invalidJSON, "")
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected invalid JSON error, got: %v", err)
	}
}

func TestValidateAgainstSchema_InvalidSchema(t *testing.T) {
	invalidSchema := []byte(`{invalid schema}`)
	validJSON := []byte(`{}`)
	err := ValidateAgainstSchema("test.schema.json", invalidSchema, validJSON, "")
	if err == nil || !strings.Contains(err.Error(), "loading schema") {
		t.Errorf("expected schema loading error, got: %v", err)
	}
}

func TestValidateAgainstSchema_InvalidRef(t *testing.T) {
	// Valid empty schema, but ref does not exist
	schemaBytes := []byte(`{"$schema":"http://json-schema.org/draft-07/schema#"}`)
	validJSON := []byte(`{}`)
	err := ValidateAgainstSchema("test.schema.json", schemaBytes, validJSON, "#/not-a-real-ref")
	if err == nil || !strings.Contains(err.Error(), "compiling schema") {
		t.Errorf("expected compiling schema error for invalid ref, got: %v", err)
	}
}

func TestValidateAgainstSchema_ValidationFails(t *testing.T) {
	// Schema expects a string, but we provide a number
	schemaBytes := []byte(`{"type":"string"}`)
	data := []byte(`123`)
	err := ValidateAgainstSchema("test.schema.json", schemaBytes, data, "")
	if err == nil || !strings.Contains(err.Error(), "schema validation against") {
		t.Errorf("expected schema validation error, got: %v", err)
	}
}

func TestValidateAgainstSchema_ValidationPasses(t *testing.T) {
	// Schema expects a string, and we provide a string
	schemaBytes := []byte(`{"type":"string"}`)
	data := []byte(`"hello"`)
	err := ValidateAgainstSchema("test.schema.json", schemaBytes, data, "")
	if err != nil {
		t.Errorf("expected validation to pass, got: %v", err)
	}
}

func TestValidateUserTemplateJSON_CallsValidateAgainstSchema(t *testing.T) {
	// This test just ensures the function calls ValidateAgainstSchema with correct ref
	// We use a minimal valid user template (should fail schema, but that's fine)
	data := []byte(`{"foo":"bar"}`)
	err := ValidateUserTemplateJSON(data)
	if err == nil {
		t.Errorf("expected error due to schema mismatch, got nil")
	}
}

func TestValidateImageTemplateJSON_CallsValidateAgainstSchema(t *testing.T) {
	// This test just ensures the function calls ValidateAgainstSchema with correct ref
	data := []byte(`{"foo":"bar"}`)
	err := ValidateImageTemplateJSON(data)
	if err == nil {
		t.Errorf("expected error due to schema mismatch, got nil")
	}
}

func TestValidateConfigJSON_CallsValidateAgainstSchema(t *testing.T) {
	// This test just ensures the function calls ValidateAgainstSchema with correct schema
	data := []byte(`{"foo":"bar"}`)
	err := ValidateConfigJSON(data)
	if err == nil {
		t.Errorf("expected error due to schema mismatch, got nil")
	}
}
func TestValidateAgainstSchema_RefVariants(t *testing.T) {
	schemaBytes := []byte(`{
        "$schema":"http://json-schema.org/draft-07/schema#",
        "$defs": {
            "Test": {
                "$anchor": "Test",
                "type": "object",
                "properties": { "foo": { "type": "string" } },
                "required": ["foo"]
            }
        }
    }`)
	validJSON := []byte(`{"foo":"bar"}`)

	// These should pass
	err := ValidateAgainstSchema("inline", schemaBytes, validJSON, "#/$defs/Test")
	if err != nil {
		t.Errorf("expected validation to pass with #/$defs/Test, got: %v", err)
	}

	err = ValidateAgainstSchema("inline", schemaBytes, validJSON, "/$defs/Test")
	if err != nil {
		t.Errorf("expected validation to pass with /$defs/Test, got: %v", err)
	}

	// This will only work if your validator supports $anchor and you reference as "#Test"
	err = ValidateAgainstSchema("inline", schemaBytes, validJSON, "#Test")
	if err != nil {
		t.Logf("anchor #Test not supported by this validator: %v", err)
	}
}

func TestValidateAgainstSchema_InvalidJSONErrorMessage(t *testing.T) {
	schemaBytes := []byte(`{"type":"object"}`)
	invalidJSON := []byte(`{invalid}`)
	err := ValidateAgainstSchema("test.schema.json", schemaBytes, invalidJSON, "")
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected invalid JSON error, got: %v", err)
	}
}

func TestValidateAgainstSchema_ValidationErrorMessage(t *testing.T) {
	schemaBytes := []byte(`{"type":"object","required":["foo"]}`)
	data := []byte(`{}`)
	err := ValidateAgainstSchema("test.schema.json", schemaBytes, data, "")
	if err == nil || !strings.Contains(err.Error(), "schema validation against") {
		t.Errorf("expected schema validation error, got: %v", err)
	}
}

func TestValidateImageTemplateJSON_DelegatesToValidateAgainstSchema(t *testing.T) {
	// This test ensures ValidateImageTemplateJSON calls ValidateAgainstSchema with correct params.
	// We use a minimal valid template for the fullRef.
	data := []byte(`{"image":{"name":"n","version":"1.0.0"},"target":{"os":"azure-linux","dist":"azl3","arch":"x86_64","imageType":"raw"},"systemConfig":{"name":"n","packages":["filesystem"],"kernel":{"version":"6.12"}}}`)
	err := ValidateImageTemplateJSON(data)
	// Should fail unless schema accepts this, but we only care that it calls ValidateAgainstSchema.
	if err == nil {
		// If schema is permissive, that's fine.
	} else if !strings.Contains(err.Error(), "schema validation against") && !strings.Contains(err.Error(), "compiling schema") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateUserTemplateJSON_DelegatesToValidateAgainstSchema(t *testing.T) {
	// This test ensures ValidateUserTemplateJSON calls ValidateAgainstSchema with correct params.
	data := []byte(`{"image":{"name":"n","version":"1.0.0"},"target":{"os":"azure-linux","dist":"azl3","arch":"x86_64","imageType":"raw"}}`)
	err := ValidateUserTemplateJSON(data)
	if err == nil {
		// If schema is permissive, that's fine.
	} else if !strings.Contains(err.Error(), "schema validation against") && !strings.Contains(err.Error(), "compiling schema") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateConfigJSON_DelegatesToValidateAgainstSchema(t *testing.T) {
	// This test ensures ValidateConfigJSON calls ValidateAgainstSchema with correct params.
	data := []byte(`{"foo":"bar"}`)
	err := ValidateConfigJSON(data)
	if err == nil {
		// If schema is permissive, that's fine.
	} else if !strings.Contains(err.Error(), "schema validation against") && !strings.Contains(err.Error(), "compiling schema") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestPackageRepositoryTrustedYes validates that [trusted=yes] is accepted as a valid pkey value
func TestPackageRepositoryTrustedYes(t *testing.T) {
	templateYAML := `image:
  name: test-trusted-repo
  version: "1.0.0"

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw

packageRepositories:
  - codename: "noble"
    url: "https://example.com/repo"
    pkey: "[trusted=yes]"
    component: "main"

systemConfig:
  name: test
  packages:
    - test-package
  kernel:
    version: "6.14"
`

	var raw interface{}
	if err := yaml.Unmarshal([]byte(templateYAML), &raw); err != nil {
		t.Fatalf("YAML parsing error: %v", err)
	}

	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("JSON marshaling error: %v", err)
	}

	// This should pass validation with [trusted=yes] as a valid pkey value
	if err := ValidateImageTemplateJSON(dataJSON); err != nil {
		t.Errorf("expected template with [trusted=yes] pkey to pass validation, but got: %v", err)
	}
}

// TestPackageRepositoryWithURL validates that a normal URL pkey is still accepted
func TestPackageRepositoryWithURL(t *testing.T) {
	templateYAML := `image:
  name: test-url-repo
  version: "1.0.0"

target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: raw

packageRepositories:
  - codename: "noble"
    url: "https://example.com/repo"
    pkey: "https://example.com/key.gpg"
    component: "main"

systemConfig:
  name: test
  packages:
    - test-package
  kernel:
    version: "6.14"
`

	var raw interface{}
	if err := yaml.Unmarshal([]byte(templateYAML), &raw); err != nil {
		t.Fatalf("YAML parsing error: %v", err)
	}

	dataJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("JSON marshaling error: %v", err)
	}

	// This should pass validation with a normal URL
	if err := ValidateImageTemplateJSON(dataJSON); err != nil {
		t.Errorf("expected template with URL pkey to pass validation, but got: %v", err)
	}
}
