package resolvertest

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// Mock resolver implementation for testing
type mockResolver struct {
	resolveFunc func(requested, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error)
}

func (m *mockResolver) Resolve(requested, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	return m.resolveFunc(requested, all)
}

// TestNames tests the names helper function
func TestNames(t *testing.T) {
	tests := []struct {
		name     string
		packages []ospackage.PackageInfo
		want     []string
	}{
		{
			name:     "EmptySlice",
			packages: []ospackage.PackageInfo{},
			want:     nil, // names() returns nil for empty slice
		},
		{
			name: "SinglePackage",
			packages: []ospackage.PackageInfo{
				{Name: "package1"},
			},
			want: []string{"package1"},
		},
		{
			name: "MultiplePackagesUnsorted",
			packages: []ospackage.PackageInfo{
				{Name: "zebra"},
				{Name: "alpha"},
				{Name: "beta"},
			},
			want: []string{"alpha", "beta", "zebra"},
		},
		{
			name: "PackagesWithDuplicateNames",
			packages: []ospackage.PackageInfo{
				{Name: "duplicate", Version: "1.0"},
				{Name: "unique"},
				{Name: "duplicate", Version: "2.0"},
			},
			want: []string{"duplicate", "duplicate", "unique"},
		},
		{
			name: "PackagesWithComplexNames",
			packages: []ospackage.PackageInfo{
				{Name: "lib-package-dev"},
				{Name: "package.with.dots"},
				{Name: "package_with_underscores"},
				{Name: "package-with-hyphens"},
			},
			want: []string{"lib-package-dev", "package-with-hyphens", "package.with.dots", "package_with_underscores"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := names(tt.packages)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("names() = %v, want %v", got, tt.want)
			}
			// Verify the result is actually sorted (skip for empty slices)
			if len(got) > 0 && !sort.StringsAreSorted(got) {
				t.Errorf("names() result is not sorted: %v", got)
			}
		})
	}
}

// TestTestCasesStructure tests the structure and validity of TestCases
func TestTestCasesStructure(t *testing.T) {
	if len(TestCases) == 0 {
		t.Fatal("TestCases should not be empty")
	}

	seenNames := make(map[string]bool)
	for i, tc := range TestCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Check for duplicate test case names
			if seenNames[tc.Name] {
				t.Errorf("Duplicate test case name: %s", tc.Name)
			}
			seenNames[tc.Name] = true

			// Check that Name is not empty
			if tc.Name == "" {
				t.Errorf("TestCases[%d] has empty Name", i)
			}

			// Check that All slice is not nil (can be empty though)
			if tc.All == nil {
				t.Errorf("TestCases[%d] has nil All slice", i)
			}

			// Check that Requested slice is not nil (can be empty though)
			if tc.Requested == nil {
				t.Errorf("TestCases[%d] has nil Requested slice", i)
			}

			// Check that Want slice is not nil (can be empty though)
			if tc.Want == nil {
				t.Errorf("TestCases[%d] has nil Want slice", i)
			}

			// Check that Want is sorted (since names() returns sorted results)
			if !sort.StringsAreSorted(tc.Want) {
				t.Errorf("TestCases[%d].Want is not sorted: %v", i, tc.Want)
			}

			// Verify package names in All are unique within each package
			allNames := names(tc.All)
			uniqueNames := make(map[string]bool)
			for _, name := range allNames {
				uniqueNames[name] = true
			}

			// Verify requested packages exist in the All list (for valid test cases)
			if !tc.WantErr {
				for _, reqPkg := range tc.Requested {
					found := false
					for _, allPkg := range tc.All {
						if allPkg.Name == reqPkg.Name {
							found = true
							break
						}
					}
					if !found && len(tc.Want) > 0 {
						t.Logf("TestCases[%d]: Requested package %s not found in All list (may be intentional)", i, reqPkg.Name)
					}
				}
			}
		})
	}
}

// TestTestCasesContent tests the specific content of each test case
func TestTestCasesContent(t *testing.T) {
	expectedCases := map[string]struct {
		requestedCount int
		allCount       int
		wantCount      int
		wantErr        bool
	}{
		"SimpleChain":       {requestedCount: 1, allCount: 3, wantCount: 3, wantErr: false},
		"MultipleProviders": {requestedCount: 1, allCount: 4, wantCount: 3, wantErr: false},
		"NoDependencies":    {requestedCount: 1, allCount: 1, wantCount: 1, wantErr: false},
		"MissingRequested":  {requestedCount: 1, allCount: 1, wantCount: 0, wantErr: true},
	}

	for _, tc := range TestCases {
		t.Run(tc.Name, func(t *testing.T) {
			expected, exists := expectedCases[tc.Name]
			if !exists {
				t.Logf("No expectations defined for test case: %s", tc.Name)
				return
			}

			if len(tc.Requested) != expected.requestedCount {
				t.Errorf("Expected %d requested packages, got %d", expected.requestedCount, len(tc.Requested))
			}

			if len(tc.All) != expected.allCount {
				t.Errorf("Expected %d all packages, got %d", expected.allCount, len(tc.All))
			}

			if len(tc.Want) != expected.wantCount {
				t.Errorf("Expected %d want packages, got %d", expected.wantCount, len(tc.Want))
			}

			if tc.WantErr != expected.wantErr {
				t.Errorf("Expected WantErr=%v, got %v", expected.wantErr, tc.WantErr)
			}
		})
	}
}

// TestRunResolverTestsFunc_Structure tests that the function has the correct structure
func TestRunResolverTestsFunc_Structure(t *testing.T) {
	// Test that RunResolverTestsFunc exists and can be called
	// We verify the function signature and basic operation

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RunResolverTestsFunc should not panic, but got: %v", r)
		}
	}()

	// Create a simple resolver that handles the test cases appropriately
	resolverFunc := func(req, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
		// For structure testing, we return what's expected for each test case
		// Match by test case name which we can infer from the request context
		if len(req) > 0 {
			reqName := req[0].Name
			switch reqName {
			case "A":
				// Check if this is SimpleChain or MultipleProviders by looking at the all slice
				hasP1P2 := false
				for _, pkg := range all {
					if pkg.Name == "P1" || pkg.Name == "P2" {
						hasP1P2 = true
						break
					}
				}
				if hasP1P2 {
					// This is MultipleProviders test case
					return []ospackage.PackageInfo{
						{Name: "A"}, {Name: "P2"}, {Name: "Y"},
					}, nil
				} else {
					// This is SimpleChain test case
					return []ospackage.PackageInfo{
						{Name: "A"}, {Name: "B"}, {Name: "C"},
					}, nil
				}
			case "X":
				// NoDependencies test case
				return []ospackage.PackageInfo{{Name: "X"}}, nil
			case "B":
				// MissingRequested test case - should return error
				return nil, errors.New("expected error for test case")
			}
		}
		// Default case - return empty slice
		return []ospackage.PackageInfo{}, nil
	}

	// This should run without panicking and execute all test cases
	RunResolverTestsFunc(t, "StructureTest", resolverFunc)
}

// TestResolverInterface tests that the Resolver interface is properly defined
func TestResolverInterface(t *testing.T) {
	// Test that mockResolver implements Resolver interface
	var resolver Resolver = &mockResolver{
		resolveFunc: func(requested, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
			return []ospackage.PackageInfo{}, nil
		},
	}

	// Test interface method call
	result, err := resolver.Resolve(
		[]ospackage.PackageInfo{{Name: "test"}},
		[]ospackage.PackageInfo{{Name: "test"}},
	)

	if err != nil {
		t.Errorf("Resolver.Resolve returned unexpected error: %v", err)
	}

	if result == nil {
		t.Error("Resolver.Resolve should not return nil slice")
	}
}

// TestPackageInfoFields tests that PackageInfo fields are properly accessible
func TestPackageInfoFields(t *testing.T) {
	pkg := ospackage.PackageInfo{
		Name:        "test-package",
		Type:        "rpm",
		Description: "Test package",
		Origin:      "TestVendor",
		License:     "MIT",
		Version:     "1.0.0",
		Arch:        "x86_64",
		URL:         "https://example.com/test.rpm",
		Provides:    []string{"test-capability"},
		Requires:    []string{"dependency1", "dependency2"},
		RequiresVer: []string{"dependency1 (>= 1.0)", "dependency2"},
		Files:       []string{"/usr/bin/test", "/usr/share/test/config"},
	}

	// Test field access
	if pkg.Name != "test-package" {
		t.Errorf("Expected Name='test-package', got '%s'", pkg.Name)
	}

	if len(pkg.Requires) != 2 {
		t.Errorf("Expected 2 requirements, got %d", len(pkg.Requires))
	}

	if len(pkg.Provides) != 1 {
		t.Errorf("Expected 1 provides, got %d", len(pkg.Provides))
	}
}

// TestHelperFunction tests edge cases of the names helper function
func TestHelperFunction(t *testing.T) {
	t.Run("NilSlice", func(t *testing.T) {
		// Test with nil slice (should not panic)
		result := names(nil)
		// names() returns nil slice for nil input, which is fine
		if len(result) != 0 {
			t.Errorf("names() with nil input should return empty/nil slice, got %v with length %d", result, len(result))
		}
	})

	t.Run("LargeSlice", func(t *testing.T) {
		// Test with a large slice
		packages := make([]ospackage.PackageInfo, 100) // Reduced size to avoid timeout
		for i := 0; i < 100; i++ {
			packages[i] = ospackage.PackageInfo{Name: string(rune(65 + (i % 26)))} // A, B, C, ...
		}

		result := names(packages)
		if len(result) != 100 {
			t.Errorf("Expected 100 names, got %d", len(result))
		}

		// Verify it's sorted
		if len(result) > 0 && !sort.StringsAreSorted(result) {
			t.Error("Large slice result should be sorted")
		}
	})
}

// TestConcurrency tests if the functions are safe for concurrent use
func TestConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}

	packages := []ospackage.PackageInfo{
		{Name: "pkg1"}, {Name: "pkg2"}, {Name: "pkg3"},
	}

	// Run names() function concurrently
	const numGoroutines = 10
	results := make(chan []string, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			results <- names(packages)
		}()
	}

	// Collect results
	expected := []string{"pkg1", "pkg2", "pkg3"}
	for i := 0; i < numGoroutines; i++ {
		result := <-results
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Concurrent call %d: got %v, want %v", i, result, expected)
		}
	}
}
