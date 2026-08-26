package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTemplate(t *testing.T) {
	// Create a temporary template file
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "test-template.yml")

	templateContent := `
metadata:
  description: "Cloud-ready image for VM deployment"
  use_cases:
    - "cloud VM deployment"
    - "AWS instances"
  keywords:
    - cloud
    - aws
    - azure
  capabilities:
    - security
    - monitoring
  recommendedFor:
    - "cloud VM deployment"

image:
  name: test-cloud-image
  version: "1.0.0"

target:
  os: test-os
  dist: test12
  arch: x86_64
  imageType: raw

systemConfig:
  name: test-config
  description: "Test configuration"
  packages:
    - nginx
    - docker-ce
    - openssh-server
`
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write test template: %v", err)
	}

	info, err := ParseTemplate(templatePath)
	if err != nil {
		t.Fatalf("ParseTemplate failed: %v", err)
	}

	// Verify parsed fields
	if info.FileName != "test-template.yml" {
		t.Errorf("expected FileName 'test-template.yml', got '%s'", info.FileName)
	}

	if info.ImageName != "test-cloud-image" {
		t.Errorf("expected ImageName 'test-cloud-image', got '%s'", info.ImageName)
	}

	if info.ImageVersion != "1.0.0" {
		t.Errorf("expected ImageVersion '1.0.0', got '%s'", info.ImageVersion)
	}

	if info.Distribution != "test12" {
		t.Errorf("expected Distribution 'test12', got '%s'", info.Distribution)
	}

	if info.Architecture != "x86_64" {
		t.Errorf("expected Architecture 'x86_64', got '%s'", info.Architecture)
	}

	if info.OS != "test-os" {
		t.Errorf("expected OS 'test-os', got '%s'", info.OS)
	}

	if info.ImageType != "raw" {
		t.Errorf("expected ImageType 'raw', got '%s'", info.ImageType)
	}

	// Verify metadata
	if len(info.Metadata.UseCases) != 2 {
		t.Errorf("expected 2 use cases, got %d", len(info.Metadata.UseCases))
	}

	if info.Metadata.Description != "Cloud-ready image for VM deployment" {
		t.Errorf("expected Description 'Cloud-ready image for VM deployment', got '%s'", info.Metadata.Description)
	}

	if len(info.Metadata.Keywords) != 3 {
		t.Errorf("expected 3 keywords, got %d", len(info.Metadata.Keywords))
	}

	if len(info.Packages) != 3 {
		t.Errorf("expected 3 packages, got %d", len(info.Packages))
	}
}

func TestParseTemplateWithoutMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "minimal-edge-template.yml")

	templateContent := `
image:
  name: minimal-edge-image
  version: "2.0.0"

target:
  os: elxr
  dist: elxr12
  arch: x86_64
  imageType: raw

systemConfig:
  name: minimal-edge
  description: "Minimal edge configuration"
  packages:
    - kernel
    - systemd
`
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write test template: %v", err)
	}

	info, err := ParseTemplate(templatePath)
	if err != nil {
		t.Fatalf("ParseTemplate failed: %v", err)
	}

	// Verify that systemConfig description is used as metadata description
	if info.Metadata.Description != "Minimal edge configuration" {
		t.Errorf("expected Description to fall back to systemConfig.description, got '%s'", info.Metadata.Description)
	}

	// Verify keywords are empty (will be inferred)
	if len(info.Metadata.Keywords) != 0 {
		t.Errorf("expected 0 metadata keywords, got %d", len(info.Metadata.Keywords))
	}
}

func TestBuildSearchableText(t *testing.T) {
	info := &TemplateInfo{
		FileName:     "cloud-edge-template.yml",
		ImageName:    "cloud-edge-image",
		Distribution: "elxr12",
		Architecture: "x86_64",
		OS:           "wind-river-elxr",
		ImageType:    "raw",
		Packages:     []string{"nginx", "docker-ce", "openssh-server"},
		Metadata: Metadata{
			Description: "Cloud-ready edge image",
			UseCases:    []string{"cloud-deployment", "edge computing"},
			Keywords:    []string{"cloud", "edge", "aws"},
		},
	}

	text := info.BuildSearchableText()

	// Check that key elements are present
	expectedParts := []string{
		"Template: cloud-edge-template.yml",
		"Name: cloud-edge-image",
		"Use cases: cloud-deployment; edge computing",
		"Description: Cloud-ready edge image",
		"Distribution: elxr12",
		"Architecture: x86_64",
		"Keywords: cloud, edge, aws",
		"Packages: nginx, docker-ce, openssh-server",
	}

	for _, part := range expectedParts {
		if !strings.Contains(text, part) {
			t.Errorf("expected searchable text to contain '%s', got:\n%s", part, text)
		}
	}
}

func TestBuildSearchableTextWithInferredKeywords(t *testing.T) {
	info := &TemplateInfo{
		FileName:     "elxr12-x86_64-minimal-edge-raw.yml",
		ImageName:    "minimal-edge",
		Distribution: "elxr12",
		Architecture: "x86_64",
		ImageType:    "raw",
		Packages:     []string{"kernel"},
		Metadata:     Metadata{}, // No explicit keywords
	}

	text := info.BuildSearchableText()

	// Should infer keywords from filename
	if !strings.Contains(text, "minimal") || !strings.Contains(text, "edge") || !strings.Contains(text, "raw") {
		t.Errorf("expected inferred keywords in searchable text, got:\n%s", text)
	}
}

func TestInferKeywordsFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		expected []string
	}{
		{
			filename: "elxr-cloud-amd64.yml",
			expected: []string{"cloud"},
		},
		{
			filename: "emt3-x86_64-minimal-raw.yml",
			expected: []string{"minimal", "raw"},
		},
		{
			filename: "ubuntu24-x86_64-minimal-desktop-raw.yml",
			expected: []string{"minimal", "raw", "desktop"},
		},
		{
			filename: "azl3-x86_64-edge-raw.yml",
			expected: []string{"edge", "raw"},
		},
		{
			filename: "emt3-x86_64-dlstreamer.yml",
			expected: []string{"dlstreamer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			info := &TemplateInfo{FileName: tt.filename}
			keywords := info.inferKeywordsFromFilename()

			for _, exp := range tt.expected {
				found := false
				for _, kw := range keywords {
					if kw == exp {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected keyword '%s' in %v", exp, keywords)
				}
			}
		})
	}
}

func TestGetAllKeywords(t *testing.T) {
	info := &TemplateInfo{
		FileName: "test-cloud-edge.yml",
		Metadata: Metadata{
			Keywords:     []string{"explicit1", "explicit2"},
			Capabilities: []string{"security", "monitoring"},
		},
	}

	keywords := info.GetAllKeywords()

	expected := []string{"explicit1", "explicit2", "security", "monitoring"}
	for _, exp := range expected {
		found := false
		for _, kw := range keywords {
			if kw == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected keyword '%s' in %v", exp, keywords)
		}
	}
}

func TestGetPackageSet(t *testing.T) {
	info := &TemplateInfo{
		Packages: []string{"nginx", "docker-ce=5:20.10.0", "openssh-server"},
	}

	set := info.GetPackageSet()

	expectedPackages := []string{"nginx", "docker-ce=5:20.10.0", "docker-ce", "openssh-server"}
	for _, pkg := range expectedPackages {
		if !set[pkg] {
			t.Errorf("expected package '%s' in set", pkg)
		}
	}
}

func TestScanTemplates(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple template files
	templates := map[string]string{
		"template1.yml": `
image:
  name: template1
  version: "1.0.0"
target:
  os: test
  dist: test1
  arch: x86_64
  imageType: raw
systemConfig:
  name: config1
  packages: []
`,
		"template2.yaml": `
image:
  name: template2
  version: "2.0.0"
target:
  os: test
  dist: test2
  arch: amd64
  imageType: iso
systemConfig:
  name: config2
  packages:
    - pkg1
`,
		"not-a-template.txt": "this is not a template",
	}

	for name, content := range templates {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	// Templates live in per-distribution subdirectories, so a template nested one
	// level down must be discovered too.
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	nested := `
image:
  name: template3
  version: "3.0.0"
target:
  os: test
  dist: test3
  arch: x86_64
  imageType: raw
systemConfig:
  name: config3
  packages: []
`
	if err := os.WriteFile(filepath.Join(subDir, "template3.yml"), []byte(nested), 0644); err != nil {
		t.Fatalf("failed to write nested template: %v", err)
	}

	scanned, err := ScanTemplates(tmpDir)
	if err != nil {
		t.Fatalf("ScanTemplates failed: %v", err)
	}

	if len(scanned) != 3 {
		t.Errorf("expected 3 templates, got %d", len(scanned))
	}

	// Verify every template was parsed, including the one in the subdirectory
	names := make(map[string]bool)
	for _, tmpl := range scanned {
		names[tmpl.ImageName] = true
	}

	for _, want := range []string{"template1", "template2", "template3"} {
		if !names[want] {
			t.Errorf("expected %s to be parsed, got %v", want, names)
		}
	}
}
