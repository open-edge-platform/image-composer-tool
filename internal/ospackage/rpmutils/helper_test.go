package rpmutils

import (
	"regexp"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
)

func TestExtractRepoBase(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		expected string
		wantErr  bool
	}{
		{
			name:     "Debian pool URL",
			rawURL:   "https://example.com/debian/pool/main/a/acct/acct_6.6.4-5+b1_amd64.deb",
			expected: "https://example.com/debian/pool/",
			wantErr:  false,
		},
		{
			name:     "RPM Packages URL",
			rawURL:   "https://example.com/rpm/Packages/curl-8.8.0-2.azl3.x86_64.rpm",
			expected: "https://example.com/rpm/Packages/",
			wantErr:  false,
		},
		{
			name:     "RPM file direct URL",
			rawURL:   "https://example.com/repo/x86_64/curl-8.8.0-2.azl3.x86_64.rpm",
			expected: "https://example.com/repo/x86_64/",
			wantErr:  false,
		},
		{
			name:     "DEB file direct URL",
			rawURL:   "https://example.com/repo/binary-amd64/acct_6.6.4-5+b1_amd64.deb",
			expected: "https://example.com/repo/binary-amd64/",
			wantErr:  false,
		},
		{
			name:     "URL without recognized pattern",
			rawURL:   "https://example.com/some/path",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "Invalid URL",
			rawURL:   "not-a-url",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractRepoBase(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Errorf("extractRepoBase() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("extractRepoBase() unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("extractRepoBase() = %q, want %q", result, tt.expected)
				}
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected int
	}{
		{
			name:     "v1 greater than v2",
			v1:       "acct_6.6.5-1_amd64.deb",
			v2:       "acct_6.6.4-5+b1_amd64.deb",
			expected: 1,
		},
		{
			name:     "v1 less than v2",
			v1:       "acct_6.6.4-1_amd64.deb",
			v2:       "acct_6.6.5-1_amd64.deb",
			expected: -1,
		},
		{
			name:     "v1 equal to v2",
			v1:       "acct_6.6.4-5+b1_amd64.deb",
			v2:       "acct_6.6.4-5+b1_amd64.deb",
			expected: 0,
		},
		{
			name:     "simple version comparison",
			v1:       "1.0.0",
			v2:       "2.0.0",
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareVersions(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.v1, tt.v2, result, tt.expected)
			}
		})
	}
}

func TestExtractBasePackageNameFromFile(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		expected string
	}{
		{
			name:     "RPM with version",
			fullName: "curl-8.8.0-2.azl3.x86_64.rpm",
			expected: "curl",
		},
		{
			name:     "RPM with devel suffix",
			fullName: "curl-devel-8.8.0-1.azl3.x86_64.rpm",
			expected: "curl-devel",
		},
		{
			name:     "RPM without .rpm extension",
			fullName: "curl-8.8.0-2.azl3.x86_64",
			expected: "curl",
		},
		{
			name:     "Package with multiple dashes",
			fullName: "python3-some-package-1.2.3-4.el8.noarch.rpm",
			expected: "python3-some-package",
		},
		{
			name:     "Simple package name without version",
			fullName: "simple-package",
			expected: "simple-package",
		},
		{
			name:     "Single word package",
			fullName: "curl",
			expected: "curl",
		},
		{
			name:     "Package name with no version part",
			fullName: "some-package-name-without-version",
			expected: "some-package-name-without-version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBasePackageNameFromFile(tt.fullName)
			if result != tt.expected {
				t.Errorf("extractBasePackageNameFromFile(%q) = %q, want %q", tt.fullName, result, tt.expected)
			}
		})
	}
}

func TestExtractBaseNameFromDep(t *testing.T) {
	tests := []struct {
		name     string
		req      string
		expected string
	}{
		{
			name:     "Simple requirement",
			req:      "curl",
			expected: "curl",
		},
		{
			name:     "Requirement with parentheses and space",
			req:      "(python3 >= 3.6)",
			expected: "python3",
		},
		{
			name:     "Requirement with complex expression",
			req:      "systemd (= 0:255-29.emt3)",
			expected: "systemd",
		},
		{
			name:     "Empty requirement",
			req:      "",
			expected: "",
		},
		{
			name:     "Requirement with only spaces",
			req:      "   ",
			expected: "",
		},
		{
			name:     "Package with version constraint",
			req:      "glibc >= 2.17",
			expected: "glibc",
		},
		{
			name:     "Complex conditional dependency",
			req:      "((kernel-modules-extra-uname-r = 6.12.0-174.el10.x86_64) if kernel-modules-extra-matched)",
			expected: "kernel-modules-extra-uname-r",
		},
		{
			name:     "Simple parentheses without spaces",
			req:      "(linux-firmware)",
			expected: "linux-firmware",
		},
		{
			name:     "Simple parentheses with version constraint",
			req:      "(glibc >= 2.17)",
			expected: "glibc",
		},
		{
			name:     "Complex conditional dependency with >= operator",
			req:      "((linux-firmware >= 20150904-56.git6ebf5d57) if linux-firmware)",
			expected: "linux-firmware",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBaseNameFromDep(tt.req)
			if result != tt.expected {
				t.Errorf("extractBaseNameFromDep(%q) = %q, want %q", tt.req, result, tt.expected)
			}
		})
	}
}

func TestExtractVersionRequirement(t *testing.T) {
	tests := []struct {
		name          string
		reqVers       []string
		depName       string
		expectedOp    string
		expectedVer   string
		expectedFound bool
	}{
		{
			name:          "Exact version requirement",
			reqVers:       []string{"systemd (= 0:255-29.emt3)"},
			depName:       "systemd",
			expectedOp:    "=",
			expectedVer:   "0:255-29.emt3",
			expectedFound: true,
		},
		{
			name:          "Greater than requirement",
			reqVers:       []string{"glibc (>= 2.17)"},
			depName:       "glibc",
			expectedOp:    ">=",
			expectedVer:   "2.17",
			expectedFound: true,
		},
		{
			name:          "Alternative dependencies",
			reqVers:       []string{"curl (>= 7.0) | wget"},
			depName:       "curl",
			expectedOp:    ">=",
			expectedVer:   "7.0",
			expectedFound: true,
		},
		{
			name:          "Dependency not found",
			reqVers:       []string{"other-package (>= 1.0)"},
			depName:       "missing-package",
			expectedOp:    "",
			expectedVer:   "",
			expectedFound: false,
		},
		{
			name:          "Dependency without version constraint",
			reqVers:       []string{"curl"},
			depName:       "curl",
			expectedOp:    "",
			expectedVer:   "",
			expectedFound: false,
		},
		{
			name:          "Empty requirements",
			reqVers:       []string{},
			depName:       "curl",
			expectedOp:    "",
			expectedVer:   "",
			expectedFound: false,
		},
		{
			name:          "Multiple version parts",
			reqVers:       []string{"package (>= 1.2.3-4.el8)"},
			depName:       "package",
			expectedOp:    ">=",
			expectedVer:   "1.2.3-4.el8",
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, ver, found := extractVersionRequirement(tt.reqVers, tt.depName)
			if op != tt.expectedOp {
				t.Errorf("extractVersionRequirement() op = %q, want %q", op, tt.expectedOp)
			}
			if ver != tt.expectedVer {
				t.Errorf("extractVersionRequirement() ver = %q, want %q", ver, tt.expectedVer)
			}
			if found != tt.expectedFound {
				t.Errorf("extractVersionRequirement() found = %v, want %v", found, tt.expectedFound)
			}
		})
	}
}

func TestComparePackageVersions(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected int
		wantErr  bool
	}{
		{
			name:     "Empty versions",
			a:        "",
			b:        "",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "First empty",
			a:        "",
			b:        "1.0",
			expected: -1,
			wantErr:  false,
		},
		{
			name:     "Second empty",
			a:        "1.0",
			b:        "",
			expected: 1,
			wantErr:  false,
		},
		{
			name:     "Equal versions",
			a:        "1.0.0",
			b:        "1.0.0",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "First greater",
			a:        "2.0.0",
			b:        "1.0.0",
			expected: 1,
			wantErr:  false,
		},
		{
			name:     "Second greater",
			a:        "1.0.0",
			b:        "2.0.0",
			expected: -1,
			wantErr:  false,
		},
		{
			name:     "With epoch - first greater",
			a:        "2:1.0.0",
			b:        "1:2.0.0",
			expected: 1,
			wantErr:  false,
		},
		{
			name:     "With epoch - second greater",
			a:        "1:1.0.0",
			b:        "2:1.0.0",
			expected: -1,
			wantErr:  false,
		},
		{
			name:     "Complex version with revision",
			a:        "1.19-1.emt3",
			b:        "1.19",
			expected: 0, // Should be treated as equal due to prefix logic
			wantErr:  false,
		},
		{
			name:     "Debian-style versions",
			a:        "6.6.4-5+b1",
			b:        "6.6.4-5",
			expected: 0, // Due to prefix logic, these are treated as equal
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comparePackageVersions(tt.a, tt.b)
			if tt.wantErr {
				if err == nil {
					t.Errorf("comparePackageVersions() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("comparePackageVersions() unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("comparePackageVersions(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
				}
			}
		})
	}
}

func TestFindAllCandidates(t *testing.T) {
	// Create test data
	parent := ospackage.PackageInfo{
		Name: "parent-package",
		URL:  "https://example.com/repo/parent.rpm",
	}

	allPackages := []ospackage.PackageInfo{
		{
			Name:    "curl-8.8.0-2.azl3.x86_64.rpm",
			Version: "8.8.0-2.azl3",
		},
		{
			Name:    "curl-7.8.0-1.azl3.x86_64.rpm",
			Version: "7.8.0-1.azl3",
		},
		{
			Name:     "another-package-1.0-1.rpm",
			Version:  "1.0-1",
			Provides: []string{"provided-capability"},
		},
		{
			Name:  "file-provider-1.0-1.rpm",
			Files: []string{"/usr/bin/curl"},
		},
	}

	tests := []struct {
		name          string
		depName       string
		expectedCount int
		expectedFirst string // Name of the first candidate (highest version)
	}{
		{
			name:          "Direct name match",
			depName:       "curl",
			expectedCount: 2,                              // curl packages
			expectedFirst: "curl-8.8.0-2.azl3.x86_64.rpm", // Higher version
		},
		{
			name:          "Provides match",
			depName:       "provided-capability",
			expectedCount: 1, // Only the package that provides this capability
			expectedFirst: "another-package-1.0-1.rpm",
		},
		{
			name:          "File match",
			depName:       "/usr/bin/curl",
			expectedCount: 1,
			expectedFirst: "file-provider-1.0-1.rpm",
		},
		{
			name:          "No match",
			depName:       "nonexistent",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := findAllCandidates(parent, tt.depName, allPackages)
			if err != nil {
				t.Errorf("findAllCandidates() unexpected error: %v", err)
			}
			if len(candidates) != tt.expectedCount {
				t.Errorf("findAllCandidates() returned %d candidates, want %d", len(candidates), tt.expectedCount)
			}
			if tt.expectedCount > 0 && candidates[0].Name != tt.expectedFirst {
				t.Errorf("findAllCandidates() first candidate = %q, want %q", candidates[0].Name, tt.expectedFirst)
			}
		})
	}
}

func TestResolveTopPackageConflicts(t *testing.T) {
	// Save original Dist value and restore after test
	originalDist := Dist
	defer func() { Dist = originalDist }()

	allPackages := []ospackage.PackageInfo{
		{
			Name:    "acct-6.6.4-5+b1-amd64.rpm",
			Version: "6.6.4-5+b1",
			URL:     "https://example.com/acct-6.6.4-5+b1-amd64.rpm",
		},
		{
			Name:    "acct-205-25.azl3.noarch.rpm",
			Version: "205-25.azl3",
		},
		{
			Name:    "acct-tools",
			Version: "1.0-1.azl3",
		},
		{
			Name:    "acct-new",
			Version: "1.0-1.emt3",
		},
		{
			Name:    "acct-other",
			Version: "2.0-1.azl3",
		},
		{
			Name:    "wayland-1.20.0-1.azl3.x86_64.rpm",
			Version: "1.20.0-1.azl3",
		},
		{
			Name:    "wayland-devel-1.22.0-1.azl3.x86_64.rpm",
			Version: "1.22.0-1.azl3",
		},
	}

	tests := []struct {
		name        string
		want        string
		pkgType     string
		dist        string
		expectedPkg string
		expectFound bool
	}{
		{
			name:        "Exact match with file extension",
			want:        "acct-6.6.4-5+b1-amd64.rpm",
			dist:        "",
			expectedPkg: "acct-6.6.4-5+b1-amd64.rpm",
			expectFound: true,
		},
		{
			name:        "Base name match",
			want:        "acct",
			dist:        "",
			expectedPkg: "acct-205-25.azl3.noarch.rpm", // Should find the first acct package
			expectFound: true,
		},
		{
			name:        "Base name match with dist filter",
			want:        "acct",
			dist:        "azl3",
			expectedPkg: "acct-205-25.azl3.noarch.rpm", // The exact package name returned might be different due to filtering logic
			expectFound: true,
		},
		{
			name:        "No match",
			want:        "nonexistent",
			dist:        "",
			expectFound: false,
		},
		{
			name:        "Wildcard match",
			want:        "wayland*",
			dist:        "",
			expectedPkg: "wayland-devel-1.22.0-1.azl3.x86_64.rpm",
			expectFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Dist = tt.dist
			pkg, found := ResolveTopPackageConflicts(tt.want, allPackages)
			if found != tt.expectFound {
				t.Errorf("ResolveTopPackageConflicts() found = %v, want %v", found, tt.expectFound)
			}
			if tt.expectFound && pkg.Name != tt.expectedPkg {
				t.Logf("ResolveTopPackageConflicts() found pkg: Name=%q, Version=%q", pkg.Name, pkg.Version)
				t.Errorf("ResolveTopPackageConflicts() pkg.Name = %q, want %q", pkg.Name, tt.expectedPkg)
			}
		})
	}
}

func TestResolveMultiCandidates(t *testing.T) {
	tests := []struct {
		name         string
		parentPkg    ospackage.PackageInfo
		candidates   []ospackage.PackageInfo
		userRepos    []config.PackageRepository
		expectedName string
		expectError  bool
	}{
		{
			name: "No candidates",
			parentPkg: ospackage.PackageInfo{
				URL: "https://example.com/repo/parent.rpm",
			},
			candidates:  []ospackage.PackageInfo{},
			expectError: true,
		},
		{
			name: "Single candidate",
			parentPkg: ospackage.PackageInfo{
				URL: "https://example.com/repo/parent.rpm",
			},
			candidates: []ospackage.PackageInfo{
				{Name: "single-candidate", URL: "https://example.com/repo/single.rpm"},
			},
			expectedName: "single-candidate",
			expectError:  false,
		},
		{
			name: "Multiple candidates same repo",
			parentPkg: ospackage.PackageInfo{
				URL: "https://example.com/repo/parent.rpm",
			},
			candidates: []ospackage.PackageInfo{
				{Name: "candidate1", Version: "1.0", URL: "https://example.com/repo/candidate1.rpm"},
				{Name: "candidate2", Version: "2.0", URL: "https://example.com/repo/candidate2.rpm"},
			},
			expectedName: "candidate2", // Should pick the latest version
			expectError:  false,
		},
		{
			name: "Multiple candidates different repos",
			parentPkg: ospackage.PackageInfo{
				URL: "https://example.com/repo1/parent.rpm",
			},
			candidates: []ospackage.PackageInfo{
				{Name: "candidate1", Version: "1.0", URL: "https://example.com/repo1/candidate1.rpm"},
				{Name: "candidate2", Version: "2.0", URL: "https://example.com/repo2/candidate2.rpm"},
			},
			expectedName: "candidate1", // Should prefer same repo
			expectError:  false,
		},
		{
			name: "Version constraint satisfied",
			parentPkg: ospackage.PackageInfo{
				URL:         "https://example.com/repo/parent.rpm",
				RequiresVer: []string{"testpkg (>= 1.5)"},
			},
			candidates: []ospackage.PackageInfo{
				{Name: "testpkg-1.0-1.rpm", Version: "1.0", URL: "https://example.com/repo/testpkg1.rpm"},
				{Name: "testpkg-2.0-1.rpm", Version: "2.0", URL: "https://example.com/repo/testpkg2.rpm"},
			},
			expectedName: "testpkg-2.0-1.rpm", // Should pick the one that satisfies constraint
			expectError:  false,
		},
		{
			name: "Higher repo priority overrides parent base",
			parentPkg: ospackage.PackageInfo{
				URL: "https://example.com/repo1/parent.rpm",
			},
			candidates: []ospackage.PackageInfo{
				{Name: "candidate1", Version: "1.0", URL: "https://example.com/repo1/candidate1.rpm"},
				{Name: "candidate2", Version: "2.0", URL: "https://example.com/repo2/candidate2.rpm"},
			},
			userRepos: []config.PackageRepository{
				{URL: "https://example.com/repo1", Priority: 500},
				{URL: "https://example.com/repo2", Priority: 900},
			},
			expectedName: "candidate2",
			expectError:  false,
		},
		{
			name: "Default priority 500 keeps parent base preference",
			parentPkg: ospackage.PackageInfo{
				URL: "https://example.com/repo1/parent.rpm",
			},
			candidates: []ospackage.PackageInfo{
				{Name: "candidate1", Version: "1.0", URL: "https://example.com/repo1/candidate1.rpm"},
				{Name: "candidate2", Version: "2.0", URL: "https://example.com/repo2/candidate2.rpm"},
			},
			userRepos: []config.PackageRepository{
				{URL: "https://example.com/repo1"},
				{URL: "https://example.com/repo2"},
			},
			expectedName: "candidate1",
			expectError:  false,
		},
	}

	origUserRepo := UserRepo
	defer func() {
		UserRepo = origUserRepo
	}()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.userRepos != nil {
				UserRepo = tt.userRepos
			} else {
				UserRepo = nil
			}

			result, err := resolveMultiCandidates(tt.parentPkg, tt.candidates)
			if tt.expectError {
				if err == nil {
					t.Errorf("resolveMultiCandidates() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("resolveMultiCandidates() unexpected error: %v", err)
				}
				if result.Name != tt.expectedName {
					t.Errorf("resolveMultiCandidates() result.Name = %q, want %q", result.Name, tt.expectedName)
				}
			}
		})
	}
}

// TestExtractBaseRequirementAdvanced tests advanced cases for the extractBaseRequirement function
func TestExtractBaseRequirementAdvanced(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple requirement",
			input:    "bash",
			expected: "bash",
		},
		{
			name:     "requirement with version",
			input:    "glibc >= 2.30",
			expected: "glibc",
		},
		{
			name:     "complex requirement with parentheses",
			input:    "(libssl.so.1.1 >= 1.1.0)",
			expected: "libssl.so.1.1",
		},
		{
			name:     "requirement with 64bit suffix",
			input:    "libpthread.so.0()(64bit)",
			expected: "libpthread.so.0",
		},
		{
			name:     "complex requirement with multiple conditions",
			input:    "(gcc-c++ and make)",
			expected: "gcc-c++",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "requirement with complex versioning",
			input:    "python3-devel >= 3.8.0",
			expected: "python3-devel",
		},
		{
			name:     "parentheses with spaces",
			input:    "( openssl-libs )",
			expected: "openssl-libs",
		},
		{
			name:     "file path requirement",
			input:    "/bin/sh",
			expected: "/bin/sh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBaseRequirement(tt.input)
			if result != tt.expected {
				t.Errorf("extractBaseRequirement(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestVersionRequirementEdgeCases tests edge cases in version requirement extraction
func TestVersionRequirementEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		reqVers       []string
		depName       string
		expectedOp    string
		expectedVer   string
		expectedFound bool
	}{
		{
			name:          "Empty requirements list",
			reqVers:       []string{},
			depName:       "package",
			expectedOp:    "",
			expectedVer:   "",
			expectedFound: false,
		},
		{
			name:          "Malformed version requirement",
			reqVers:       []string{"malformed requirement"},
			depName:       "package",
			expectedOp:    "",
			expectedVer:   "",
			expectedFound: false,
		},
		{
			name:          "Version with special characters",
			reqVers:       []string{"package (>= 1.0+build.123)"},
			depName:       "package",
			expectedOp:    ">=",
			expectedVer:   "1.0+build.123",
			expectedFound: true,
		},
		{
			name:          "Multiple version constraints",
			reqVers:       []string{"package (>= 1.0)", "package (<< 2.0)"},
			depName:       "package",
			expectedOp:    ">=",
			expectedVer:   "1.0",
			expectedFound: true,
		},
		{
			name:          "Version with epoch",
			reqVers:       []string{"package (= 2:1.0-1)"},
			depName:       "package",
			expectedOp:    "=",
			expectedVer:   "2:1.0-1",
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, ver, found := extractVersionRequirement(tt.reqVers, tt.depName)

			if found != tt.expectedFound {
				t.Errorf("extractVersionRequirement() found = %v, want %v", found, tt.expectedFound)
			}

			if tt.expectedFound {
				if op != tt.expectedOp {
					t.Errorf("extractVersionRequirement() op = %q, want %q", op, tt.expectedOp)
				}
				if ver != tt.expectedVer {
					t.Errorf("extractVersionRequirement() ver = %q, want %q", ver, tt.expectedVer)
				}
			}
		})
	}
}

// TestFindAllCandidatesEdgeCases tests edge cases in candidate finding
func TestFindAllCandidatesEdgeCases(t *testing.T) {
	parent := ospackage.PackageInfo{
		Name:        "parent-package",
		Type:        "rpm",
		Version:     "1.0-1.azl3",
		Arch:        "x86_64",
		URL:         "https://example.com/repo/Packages/parent-package-1.0-1.azl3.x86_64.rpm",
		Requires:    []string{"glibc", "systemd"},
		RequiresVer: []string{"glibc (>= 2.17)", "systemd (= 1:255-29.emt3)"},
	}

	tests := []struct {
		name          string
		depName       string
		allPackages   []ospackage.PackageInfo
		expectedCount int
		expectedNames []string
		expectError   bool
	}{
		{
			name:          "Empty package list",
			depName:       "nonexistent",
			allPackages:   []ospackage.PackageInfo{},
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:    "Package with provides field only",
			depName: "virtual-capability",
			allPackages: []ospackage.PackageInfo{
				{
					Name:     "provider1",
					Type:     "rpm",
					Version:  "1.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/provider1-1.0-1.azl3.x86_64.rpm",
					Provides: []string{"virtual-capability", "other-capability"},
					Requires: []string{"glibc"},
				},
				{
					Name:     "provider2",
					Type:     "rpm",
					Version:  "2.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/provider2-2.0-1.azl3.x86_64.rpm",
					Provides: []string{"virtual-capability"},
					Requires: []string{"glibc"},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"provider2", "provider1"}, // Sorted by version (highest first)
			expectError:   false,
		},
		{
			name:    "Package providing file",
			depName: "/usr/bin/special-tool",
			allPackages: []ospackage.PackageInfo{
				{
					Name:     "tool-package",
					Type:     "rpm",
					Version:  "1.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/tool-package-1.0-1.azl3.x86_64.rpm",
					Files:    []string{"/usr/bin/special-tool", "/usr/share/tool/config"},
					Requires: []string{"glibc"},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"tool-package"},
			expectError:   false,
		},
		{
			name:    "Multiple matches with different types",
			depName: "common-name",
			allPackages: []ospackage.PackageInfo{
				{
					Name:     "common-name",
					Type:     "rpm",
					Version:  "1.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/common-name-1.0-1.azl3.x86_64.rpm",
					Requires: []string{"glibc"},
				},
				{
					Name:     "different-package",
					Type:     "rpm",
					Version:  "2.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/different-package-2.0-1.azl3.x86_64.rpm",
					Provides: []string{"common-name"},
					Requires: []string{"glibc"},
				},
				{
					Name:     "another-package",
					Type:     "rpm",
					Version:  "1.5-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/another-package-1.5-1.azl3.x86_64.rpm",
					Files:    []string{"/usr/bin/common-name"},
					Requires: []string{"glibc"},
				},
			},
			expectedCount: 1,                       // Only the exact name match
			expectedNames: []string{"common-name"}, // Only exact match is returned
			expectError:   false,
		},
		{
			name:    "Provides matching when no exact name match",
			depName: "virtual-service",
			allPackages: []ospackage.PackageInfo{
				{
					Name:     "service-impl-a",
					Type:     "rpm",
					Version:  "1.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/service-impl-a-1.0-1.azl3.x86_64.rpm",
					Provides: []string{"virtual-service", "other-capability"},
					Requires: []string{"glibc"},
				},
				{
					Name:     "service-impl-b",
					Type:     "rpm",
					Version:  "2.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/service-impl-b-2.0-1.azl3.x86_64.rpm",
					Provides: []string{"virtual-service"},
					Requires: []string{"glibc"},
				},
				{
					Name:     "file-provider",
					Type:     "rpm",
					Version:  "1.5-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/file-provider-1.5-1.azl3.x86_64.rpm",
					Files:    []string{"/usr/bin/virtual-service"},
					Requires: []string{"glibc"},
				},
			},
			expectedCount: 2,                                            // Both packages that provide virtual-service
			expectedNames: []string{"service-impl-b", "service-impl-a"}, // Sorted by version (highest first)
			expectError:   false,
		},
		{
			name:    "File matching when no exact name or provides match",
			depName: "/usr/bin/unique-tool",
			allPackages: []ospackage.PackageInfo{
				{
					Name:     "unrelated-package",
					Type:     "rpm",
					Version:  "1.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/unrelated-package-1.0-1.azl3.x86_64.rpm",
					Provides: []string{"some-capability"},
					Requires: []string{"glibc"},
				},
				{
					Name:     "tool-provider-a",
					Type:     "rpm",
					Version:  "1.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/tool-provider-a-1.0-1.azl3.x86_64.rpm",
					Files:    []string{"/usr/bin/unique-tool", "/usr/share/tools/config"},
					Requires: []string{"glibc"},
				},
				{
					Name:     "tool-provider-b",
					Type:     "rpm",
					Version:  "2.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/tool-provider-b-2.0-1.azl3.x86_64.rpm",
					Files:    []string{"/usr/bin/unique-tool", "/usr/bin/other-tool"},
					Requires: []string{"glibc"},
				},
			},
			expectedCount: 2,                                              // Both packages that provide the file
			expectedNames: []string{"tool-provider-b", "tool-provider-a"}, // Sorted by version (highest first)
			expectError:   false,
		},
		{
			name:    "Packages with complex dependency requirements",
			depName: "complex-dep",
			allPackages: []ospackage.PackageInfo{
				{
					Name:        "complex-dep-1.0-1.azl3.x86_64.rpm",
					Type:        "rpm",
					Version:     "1.0-1.azl3",
					Arch:        "x86_64",
					URL:         "https://example.com/repo/Packages/complex-dep-1.0-1.azl3.x86_64.rpm",
					Requires:    []string{"glibc", "systemd", "openssl"},
					RequiresVer: []string{"glibc (>= 2.17)", "systemd (= 1:255-29.emt3)", "openssl (>= 1.1.1)"},
				},
				{
					Name:        "complex-dep-2.0-1.emt3.x86_64.rpm",
					Type:        "rpm",
					Version:     "2.0-1.emt3",
					Arch:        "x86_64",
					URL:         "https://example.com/other-repo/Packages/complex-dep-2.0-1.emt3.x86_64.rpm",
					Requires:    []string{"glibc", "systemd"},
					RequiresVer: []string{"glibc (>= 2.28)", "systemd (>= 1:250)"},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"complex-dep-2.0-1.emt3.x86_64.rpm", "complex-dep-1.0-1.azl3.x86_64.rpm"}, // Sorted by version
			expectError:   false,
		},
		{
			name:    "Packages with epoch in version",
			depName: "epoch-package",
			allPackages: []ospackage.PackageInfo{
				{
					Name:     "epoch-package-1:1.0-1.azl3.x86_64.rpm",
					Type:     "rpm",
					Version:  "1:1.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/epoch-package-1:1.0-1.azl3.x86_64.rpm",
					Requires: []string{"glibc"},
				},
				{
					Name:     "epoch-package-2.0-1.azl3.x86_64.rpm",
					Type:     "rpm",
					Version:  "2.0-1.azl3",
					Arch:     "x86_64",
					URL:      "https://example.com/repo/Packages/epoch-package-2.0-1.azl3.x86_64.rpm",
					Requires: []string{"glibc"},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"epoch-package-1:1.0-1.azl3.x86_64.rpm", "epoch-package-2.0-1.azl3.x86_64.rpm"}, // Epoch version should be higher
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := findAllCandidates(parent, tt.depName, tt.allPackages)

			if tt.expectError {
				if err == nil {
					t.Errorf("findAllCandidates() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("findAllCandidates() unexpected error: %v", err)
			}

			if len(candidates) != tt.expectedCount {
				t.Errorf("findAllCandidates() returned %d candidates, want %d", len(candidates), tt.expectedCount)
			}

			for i, expectedName := range tt.expectedNames {
				if i < len(candidates) && candidates[i].Name != expectedName {
					t.Errorf("findAllCandidates() candidate[%d].Name = %q, want %q", i, candidates[i].Name, expectedName)
				}
			}
		})
	}
}

// TestPackageNameExtractionEdgeCases tests edge cases in package name extraction
func TestPackageNameExtractionEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		expected string
	}{
		{
			name:     "Package with multiple version parts",
			fullName: "kernel-modules-extra-5.15.0-25.44.azl3.x86_64.rpm",
			expected: "kernel-modules-extra",
		},
		{
			name:     "Package with plus in version",
			fullName: "gcc-11.2.1+20220127-1.azl3.x86_64.rpm",
			expected: "gcc",
		},
		{
			name:     "Package with tilde in version",
			fullName: "python3-3.9.16~1.azl3.x86_64.rpm",
			expected: "python3",
		},
		{
			name:     "Package with colon in version (epoch)",
			fullName: "systemd-1:255-29.emt3.x86_64.rpm",
			expected: "systemd",
		},
		{
			name:     "Package name with underscores",
			fullName: "lib_special_package-1.0-1.el8.noarch.rpm",
			expected: "lib_special_package",
		},
		{
			name:     "Very complex package name",
			fullName: "perl-DBD-MySQL-4.050-5.module+el8.5.0+20651+a25e96c4.x86_64.rpm",
			expected: "perl-DBD-MySQL",
		},
		{
			name:     "Package without rpm extension",
			fullName: "simple-package-1.0-1.noarch",
			expected: "simple-package",
		},
		{
			name:     "Empty string",
			fullName: "",
			expected: "",
		},
		{
			name:     "Just extension",
			fullName: ".rpm",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBasePackageNameFromFile(tt.fullName)
			if result != tt.expected {
				t.Errorf("extractBasePackageNameFromFile(%q) = %q, want %q", tt.fullName, result, tt.expected)
			}
		})
	}
}

func TestGenerateSPDXFileName(t *testing.T) {
	tests := []struct {
		name         string
		repoName     string
		wantPrefix   string
		wantSuffix   string
		wantContains []string
	}{
		{
			name:         "Simple repository name",
			repoName:     "Azure_Linux",
			wantPrefix:   "spdx_manifest_rpm_Azure_Linux_",
			wantSuffix:   ".json",
			wantContains: []string{"spdx_manifest_rpm", "Azure_Linux"},
		},
		{
			name:         "Repository name with spaces",
			repoName:     "Azure Linux 3.0",
			wantPrefix:   "spdx_manifest_rpm_Azure_Linux_3.0_",
			wantSuffix:   ".json",
			wantContains: []string{"spdx_manifest_rpm", "Azure_Linux_3.0"},
		},
		{
			name:         "Empty repository name",
			repoName:     "",
			wantPrefix:   "spdx_manifest_rpm__",
			wantSuffix:   ".json",
			wantContains: []string{"spdx_manifest_rpm"},
		},
		{
			name:         "Repository name with multiple spaces",
			repoName:     "My Test Repo Name",
			wantPrefix:   "spdx_manifest_rpm_My_Test_Repo_Name_",
			wantSuffix:   ".json",
			wantContains: []string{"spdx_manifest_rpm", "My_Test_Repo_Name"},
		},
		{
			name:         "Repository name with special characters",
			repoName:     "Ubuntu-22.04 LTS",
			wantPrefix:   "spdx_manifest_rpm_Ubuntu-22.04_LTS_",
			wantSuffix:   ".json",
			wantContains: []string{"spdx_manifest_rpm", "Ubuntu-22.04_LTS"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateSPDXFileName(tt.repoName)

			// Check prefix
			if !strings.HasPrefix(result, tt.wantPrefix) {
				t.Errorf("GenerateSPDXFileName() result %q does not start with expected prefix %q", result, tt.wantPrefix)
			}

			// Check suffix
			if !strings.HasSuffix(result, tt.wantSuffix) {
				t.Errorf("GenerateSPDXFileName() result %q does not end with expected suffix %q", result, tt.wantSuffix)
			}

			// Check that all expected substrings are present
			for _, expected := range tt.wantContains {
				if !strings.Contains(result, expected) {
					t.Errorf("GenerateSPDXFileName() result %q does not contain expected substring %q", result, expected)
				}
			}

			// Validate timestamp format in the filename (YYYYMMDD_HHMMSS pattern)
			// The timestamp should be at the end before .json
			// Expected format: spdx_manifest_rpm_<reponame>_YYYYMMDD_HHMMSS.json
			if !strings.HasSuffix(result, ".json") {
				t.Errorf("GenerateSPDXFileName() result %q does not end with .json", result)
				return
			}

			// Remove .json and get the last part which should be the timestamp
			withoutExt := strings.TrimSuffix(result, ".json")
			parts := strings.Split(withoutExt, "_")
			if len(parts) < 2 {
				t.Errorf("GenerateSPDXFileName() result %q does not contain expected format", result)
				return
			}

			// The last two parts should be date and time: YYYYMMDD and HHMMSS
			if len(parts) >= 2 {
				dateTime := parts[len(parts)-2] + "_" + parts[len(parts)-1]
				// Validate timestamp format: should be exactly 15 characters (YYYYMMDD_HHMMSS)
				timestampPattern := `^\d{8}_\d{6}$`
				matched, err := regexp.MatchString(timestampPattern, dateTime)
				if err != nil {
					t.Errorf("Error matching timestamp pattern: %v", err)
					return
				}
				if !matched {
					t.Errorf("GenerateSPDXFileName() timestamp %q does not match expected pattern YYYYMMDD_HHMMSS", dateTime)
				}
			}

			// Ensure the filename doesn't contain any spaces (they should be replaced with underscores)
			if strings.Contains(result, " ") {
				t.Errorf("GenerateSPDXFileName() result %q contains spaces, but they should be replaced with underscores", result)
			}
		})
	}
}

// TestGenerateSPDXFileNameConsistency ensures the function generates consistent patterns
func TestGenerateSPDXFileNameConsistency(t *testing.T) {
	repoName := "Test Repo"

	// Generate multiple filenames
	result1 := GenerateSPDXFileName(repoName)
	result2 := GenerateSPDXFileName(repoName)

	// They should have the same structure but potentially different timestamps
	expectedPattern := `^spdx_manifest_rpm_Test_Repo_\d{8}_\d{6}\.json$`

	matched1, err := regexp.MatchString(expectedPattern, result1)
	if err != nil {
		t.Errorf("Error matching pattern for result1: %v", err)
	}
	if !matched1 {
		t.Errorf("First result %q does not match expected pattern", result1)
	}

	matched2, err := regexp.MatchString(expectedPattern, result2)
	if err != nil {
		t.Errorf("Error matching pattern for result2: %v", err)
	}
	if !matched2 {
		t.Errorf("Second result %q does not match expected pattern", result2)
	}
}
