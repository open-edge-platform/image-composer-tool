package pkgcatalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ai/index"
)

// Ensure Bundle and CuratedPackage satisfy index.Item at compile time.
var (
	_ index.Item = (*Bundle)(nil)
	_ index.Item = (*CuratedPackage)(nil)
)

func TestLoadEmbeddedCatalog(t *testing.T) {
	cat, err := LoadCatalog("")
	if err != nil {
		t.Fatalf("LoadCatalog(\"\") failed: %v", err)
	}

	if len(cat.Bundles) == 0 {
		t.Error("expected non-empty Bundles in embedded catalog")
	}
	if len(cat.CuratedPackages) == 0 {
		t.Error("expected non-empty CuratedPackages in embedded catalog")
	}

	// Verify specific seed bundles
	var foundRealsense, foundROS2 bool
	for _, b := range cat.Bundles {
		if b.ID() == "realsense-camera" {
			foundRealsense = true
			if len(b.Packages) == 0 {
				t.Errorf("realsense-camera bundle has no packages")
			}
		}
		if b.ID() == "ros2-desktop" {
			foundROS2 = true
		}
	}
	if !foundRealsense {
		t.Error("realsense-camera bundle not found in catalog")
	}
	if !foundROS2 {
		t.Error("ros2-desktop bundle not found in catalog")
	}
}

func TestBundleItemMethods(t *testing.T) {
	b, err := NewBundle(
		"test-bundle",
		"Test Bundle Name",
		"Test Description",
		[]string{"key1", "key2"},
		[]string{"pkg1", "pkg2"},
		[]string{"repo1"},
	)
	if err != nil {
		t.Fatalf("NewBundle failed: %v", err)
	}

	if b.ID() != "test-bundle" {
		t.Errorf("expected ID 'test-bundle', got %s", b.ID())
	}
	if len(b.Keywords()) != 2 || b.Keywords()[0] != "key1" {
		t.Errorf("unexpected keywords: %v", b.Keywords())
	}
	if len(b.PackageNames()) != 2 || b.PackageNames()[0] != "pkg1" {
		t.Errorf("unexpected package names: %v", b.PackageNames())
	}

	st := b.SearchableText()
	if !strings.Contains(st, "Test Bundle Name") {
		t.Errorf("SearchableText missing name: %s", st)
	}
	if !strings.Contains(st, "Test Description") {
		t.Errorf("SearchableText missing description: %s", st)
	}
	if !strings.Contains(st, "key1, key2") {
		t.Errorf("SearchableText missing keywords: %s", st)
	}
	if !strings.Contains(st, "pkg1, pkg2") {
		t.Errorf("SearchableText missing packages: %s", st)
	}
}

func TestCuratedPackageItemMethods(t *testing.T) {
	pkg, err := NewCuratedPackage(
		"intel-level-zero-npu",
		"Intel NPU Level Zero runtime",
		"intel-eci",
		[]string{"intel-driver-compiler-npu", "libze1"},
	)
	if err != nil {
		t.Fatalf("NewCuratedPackage failed: %v", err)
	}

	if pkg.ID() != "intel-level-zero-npu" {
		t.Errorf("expected ID 'intel-level-zero-npu', got %s", pkg.ID())
	}
	if len(pkg.PackageNames()) != 1 || pkg.PackageNames()[0] != "intel-level-zero-npu" {
		t.Errorf("unexpected package names: %v", pkg.PackageNames())
	}

	st := pkg.SearchableText()
	if !strings.Contains(st, "intel-level-zero-npu") {
		t.Errorf("SearchableText missing name: %s", st)
	}
	if !strings.Contains(st, "Intel NPU Level Zero runtime") {
		t.Errorf("SearchableText missing description: %s", st)
	}
	if !strings.Contains(st, "intel-driver-compiler-npu") || !strings.Contains(st, "libze1") {
		t.Errorf("SearchableText missing co-occurs: %s", st)
	}
}

func TestLoadCustomCatalogDir(t *testing.T) {
	tmpDir := t.TempDir()

	customBundlesYAML := `bundles:
  - id: custom-bundle
    name: "Custom Bundle"
    description: "Custom Description"
    keywords: [custom]
    packages: [custom-pkg]
    repos: [custom-repo]
`
	customPackagesYAML := `packages:
  - name: custom-pkg
    description: "Custom Package"
    repo: custom-repo
    co_occurs: [other-pkg]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "bundles.yaml"), []byte(customBundlesYAML), 0644); err != nil {
		t.Fatalf("failed to write custom bundles.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "packages.yaml"), []byte(customPackagesYAML), 0644); err != nil {
		t.Fatalf("failed to write custom packages.yaml: %v", err)
	}

	cat, err := LoadCatalog(tmpDir)
	if err != nil {
		t.Fatalf("LoadCatalog with custom dir failed: %v", err)
	}

	if len(cat.Bundles) != 1 || cat.Bundles[0].ID() != "custom-bundle" {
		t.Errorf("expected custom bundle, got: %v", cat.Bundles)
	}
	if len(cat.CuratedPackages) != 1 || cat.CuratedPackages[0].ID() != "custom-pkg" {
		t.Errorf("expected custom package, got: %v", cat.CuratedPackages)
	}
}

func TestNewBundleValidation(t *testing.T) {
	_, err := NewBundle("", "Name", "Desc", nil, []string{"pkg1"}, nil)
	if err == nil {
		t.Error("expected error for empty ID")
	}

	_, err = NewBundle("id", "", "Desc", nil, []string{"pkg1"}, nil)
	if err == nil {
		t.Error("expected error for empty Name")
	}

	_, err = NewBundle("id", "Name", "Desc", nil, nil, nil)
	if err == nil {
		t.Error("expected error for empty Packages")
	}
}

func TestNewCuratedPackageValidation(t *testing.T) {
	_, err := NewCuratedPackage("", "Desc", "repo", nil)
	if err == nil {
		t.Error("expected error for empty Name")
	}
}
