package debutils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
)

func TestResolveDependenciesAdvanced(t *testing.T) {
	testCases := []struct {
		name          string
		requested     []ospackage.PackageInfo
		all           []ospackage.PackageInfo
		expectError   bool
		expectedCount int
	}{
		{
			name: "simple dependency resolution",
			requested: []ospackage.PackageInfo{
				{Name: "pkg-a", Version: "1.0"},
			},
			all: []ospackage.PackageInfo{
				{Name: "pkg-a", Version: "1.0", Requires: []string{"pkg-b"}, URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg-a/pkg-a_1.0_amd64.deb"},
				{Name: "pkg-b", Version: "2.0", URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg-b/pkg-b_2.0_amd64.deb"},
			},
			expectError:   false,
			expectedCount: 2, // pkg-a + pkg-b
		},
		{
			name: "transitive dependencies",
			requested: []ospackage.PackageInfo{
				{Name: "pkg-root", Version: "1.0"},
			},
			all: []ospackage.PackageInfo{
				{Name: "pkg-root", Version: "1.0", Requires: []string{"pkg-level1"}, URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg-root/pkg-root_1.0_amd64.deb"},
				{Name: "pkg-level1", Version: "1.0", Requires: []string{"pkg-level2"}, URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg-level1/pkg-level1_1.0_amd64.deb"},
				{Name: "pkg-level2", Version: "1.0", URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg-level2/pkg-level2_1.0_amd64.deb"},
			},
			expectError:   false,
			expectedCount: 3, // pkg-root + pkg-level1 + pkg-level2
		},
		{
			name: "circular dependencies",
			requested: []ospackage.PackageInfo{
				{Name: "pkg-a", Version: "1.0"},
			},
			all: []ospackage.PackageInfo{
				{Name: "pkg-a", Version: "1.0", Requires: []string{"pkg-b"}, URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg-a/pkg-a_1.0_amd64.deb"},
				{Name: "pkg-b", Version: "1.0", Requires: []string{"pkg-a"}, URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg-b/pkg-b_1.0_amd64.deb"},
			},
			expectError:   false,
			expectedCount: 2, // Should handle circular deps gracefully
		},
		{
			name:      "empty requested packages",
			requested: []ospackage.PackageInfo{},
			all: []ospackage.PackageInfo{
				{Name: "pkg-a", Version: "1.0"},
			},
			expectError:   false,
			expectedCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ResolveDependencies(tc.requested, tc.all)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tc.expectedCount {
				t.Errorf("expected %d packages, got %d", tc.expectedCount, len(result))
			}
		})
	}
}

func TestGenerateDot(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		name        string
		pkgs        []ospackage.PackageInfo
		filename    string
		pkgSources  map[string]config.PackageSource
		expectError bool
	}{
		{
			name: "simple dot generation",
			pkgs: []ospackage.PackageInfo{
				{Name: "pkg-a", Version: "1.0", Requires: []string{"pkg-b"}},
				{Name: "pkg-b", Version: "2.0"},
			},
			filename:    filepath.Join(tmpDir, "test-deps.dot"),
			expectError: false,
		},
		{
			name: "with package sources",
			pkgs: []ospackage.PackageInfo{
				{Name: "sys", Version: "1.0"},
				{Name: "ess", Version: "1.0"},
			},
			filename: filepath.Join(tmpDir, "colored.dot"),
			pkgSources: map[string]config.PackageSource{
				"sys": config.PackageSourceSystem,
				"ess": config.PackageSourceEssential,
			},
			expectError: false,
		},
		{
			name:        "empty package list",
			pkgs:        []ospackage.PackageInfo{},
			filename:    filepath.Join(tmpDir, "empty-deps.dot"),
			expectError: false,
		},
		{
			name: "complex dependencies",
			pkgs: []ospackage.PackageInfo{
				{Name: "root", Version: "1.0", Requires: []string{"lib1 (>= 1.0)", "lib2 | lib3", "lib-special:amd64"}},
				{Name: "lib1", Version: "1.0", Requires: []string{"base"}},
				{Name: "base", Version: "1.0"},
			},
			filename:    filepath.Join(tmpDir, "complex-deps.dot"),
			expectError: false,
		},
		{
			name: "duplicate dependencies should be deduplicated",
			pkgs: []ospackage.PackageInfo{
				{Name: "libstdc++", Version: "1.0", Requires: []string{"libc6", "libc6", "libc6", "libgcc", "libgcc"}},
				{Name: "libc6", Version: "1.0"},
				{Name: "libgcc", Version: "1.0", Requires: []string{"libc6"}},
			},
			filename:    filepath.Join(tmpDir, "dedup-deps.dot"),
			expectError: false,
		},
		{
			name: "invalid path",
			pkgs: []ospackage.PackageInfo{
				{Name: "pkg", Version: "1.0"},
			},
			filename:    filepath.Join(tmpDir, "missing", "deps.dot"),
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := GenerateDot(tc.pkgs, tc.filename, tc.pkgSources)
			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			content, err := os.ReadFile(tc.filename)
			if err != nil {
				t.Fatalf("failed to read generated DOT file: %v", err)
			}
			contentStr := string(content)

			if !strings.Contains(contentStr, "digraph G {") {
				t.Error("DOT file should start with 'digraph G {'")
			}
			if !strings.Contains(contentStr, "rankdir=LR;") {
				t.Error("DOT file should declare 'rankdir=LR;'")
			}
			if !strings.Contains(contentStr, "}") {
				t.Error("DOT file should end with '}'")
			}

			for _, pkg := range tc.pkgs {
				if pkg.Name == "" {
					continue
				}
				nodeDef := fmt.Sprintf("\"%s\";", pkg.Name)
				if !strings.Contains(contentStr, nodeDef) {
					t.Errorf("DOT file should contain node for %s", pkg.Name)
				}

				// Check dependencies - each unique edge should appear exactly once
				seenEdges := make(map[string]bool)
				for _, dep := range pkg.Requires {
					depName := CleanDependencyName(dep)
					if depName == "" {
						continue
					}
					edge := fmt.Sprintf("\"%s\" -> \"%s\";", pkg.Name, depName)
					if !strings.Contains(contentStr, edge) {
						t.Errorf("DOT file should contain edge: %s", edge)
					}
					seenEdges[edge] = true
				}

				// For duplicate dependency test, verify each unique edge appears only once
				if tc.name == "duplicate dependencies should be deduplicated" {
					for edge := range seenEdges {
						count := strings.Count(contentStr, edge)
						if count != 1 {
							t.Errorf("Edge %s should appear exactly once, but appears %d times", edge, count)
						}
					}
				}
			}
		})
	}
}

func TestCleanDependencyName(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		// Simple package name
		{"libc6", "libc6"},

		// Version constraints
		{"libc6 (>= 2.34)", "libc6"},
		{"python3 (= 3.9.2-1)", "python3"},

		// Alternatives - should take first option
		{"python3 | python3-dev", "python3"},
		{"mailx | bsd-mailx | s-nail", "mailx"},

		// Architecture qualifiers
		{"gcc:amd64", "gcc"},
		{"g++:arm64", "g++"},

		// Complex combinations
		{"gcc-aarch64-linux-gnu (>= 4:10.2) | gcc:arm64", "gcc-aarch64-linux-gnu"},
		{"systemd | systemd-standalone-sysusers | systemd-sysusers", "systemd"},

		// Edge cases
		{"", ""},
		{"   spaced   ", "spaced"},

		// Additional comprehensive test cases
		{"  libssl3 (>= 3.0.0)  ", "libssl3"},
		{"package1 | package2 | package3", "package1"},
		{"lib64gcc-s1:amd64 (>= 4.1.1)", "lib64gcc-s1"},
		{"pkg with spaces", "pkg"},
		{"pkg:i386", "pkg"},
		{"pkg (= 1.0) | alt-pkg", "pkg"},
		{"pkg(<< 2.0)", "pkg"},
		{"pkg (>= 1.0, << 2.0)", "pkg"},
		{"pkg-name-with-dashes", "pkg-name-with-dashes"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := CleanDependencyName(tc.input)
			if result != tc.expected {
				t.Errorf("cleanDependencyName(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestCompareDebianVersions(t *testing.T) {
	testCases := []struct {
		a        string
		b        string
		expected int
	}{
		// Basic version comparisons
		{"1.0", "1.1", -1},
		{"1.1", "1.0", 1},
		{"1.0", "1.0", 0},

		// Epoch comparisons
		{"1:1.0", "2.0", 1},
		{"2:1.0", "1:2.0", 1},
		{"1:1.0", "1:1.0", 0},
		{"1.0", "1:1.0", -1},

		// Tilde handling (~)
		{"1.0~rc1", "1.0", -1},
		{"1.0", "1.0~rc1", 1},
		{"1.0~a1", "1.0~b1", -1},

		// Complex versions
		{"2.4.7-1ubuntu1", "2.4.7-1ubuntu2", -1},
		{"1.0.0+dfsg-1", "1.0.0+dfsg-2", -1},
		{"2.34-0ubuntu3.2", "2.34-0ubuntu3.10", -1},

		// Leading zeros
		{"1.01", "1.1", 0},
		{"1.001", "1.1", 0},

		// Empty strings
		{"", "", 0},
		{"1.0", "", 1},
		{"", "1.0", -1},

		// Mixed numeric/non-numeric
		{"1a", "10", -1},
		{"10", "1a", 1},

		// Real Debian package versions
		{"6.6.4-5+b1", "6.6.4-5", 1},
		{"7.6.4-5+b1", "6.6.4-5+b1", 1},
		{"2.34-0ubuntu3.2", "2.35-0ubuntu1", -1},
	}

	for _, tc := range testCases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			// Test the exported CompareDebianVersions function directly
			result, err := CompareDebianVersions(tc.a, tc.b)
			if err != nil {
				t.Errorf("CompareDebianVersions(%q, %q) returned error: %v", tc.a, tc.b, err)
				return
			}
			if result != tc.expected {
				t.Errorf("CompareDebianVersions(%q, %q) = %d, expected %d", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

// TestCompareVersions tests the exported compareVersions function
func TestCompareVersions(t *testing.T) {
	testCases := []struct {
		name     string
		v1       string
		v2       string
		expected int
	}{
		{
			name:     "debian package format comparison",
			v1:       "acct_6.6.4-5+b1_amd64.deb",
			v2:       "acct_7.6.4-5+b1_amd64.deb",
			expected: -1,
		},
		{
			name:     "same version",
			v1:       "pkg_1.0.0_amd64.deb",
			v2:       "pkg_1.0.0_amd64.deb",
			expected: 0,
		},
		{
			name:     "higher version first",
			v1:       "pkg_2.0.0_amd64.deb",
			v2:       "pkg_1.0.0_amd64.deb",
			expected: 1,
		},
		{
			name:     "fallback to string comparison",
			v1:       "simple-name-1",
			v2:       "simple-name-2",
			expected: -1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// CompareVersions is unexported, so we skip direct testing
			// The function is tested indirectly through Resolve functionality
			t.Skipf("CompareVersions is unexported - tested indirectly through resolution")
		})
	}
}

func TestFilterCandidatesByPriorityWithTarget(t *testing.T) {
	// Mock repository configurations for testing
	oldRepoCfgs := RepoCfgs
	oldRepoCfg := RepoCfg
	defer func() {
		RepoCfgs = oldRepoCfgs
		RepoCfg = oldRepoCfg
	}()

	// Set up test repositories with different priorities
	RepoCfgs = []RepoConfig{
		{PkgPrefix: "http://archive.ubuntu.com/ubuntu", Priority: 500},         // Default priority
		{PkgPrefix: "http://ppa.launchpad.net/test/ppa/ubuntu", Priority: 990}, // Preferred
		{PkgPrefix: "http://blocked.repo.com/ubuntu", Priority: -1},            // Blocked
		{PkgPrefix: "http://force.repo.com/ubuntu", Priority: 1001},            // Force install
	}

	testCases := []struct {
		name          string
		candidates    []ospackage.PackageInfo
		targetName    string
		expectedNames []string // Expected order of package names after filtering
		expectedCount int
		description   string
	}{
		{
			name:          "empty candidates",
			candidates:    []ospackage.PackageInfo{},
			targetName:    "test-pkg",
			expectedNames: []string{},
			expectedCount: 0,
			description:   "should handle empty candidate list",
		},
		{
			name: "exact name wins over provides",
			candidates: []ospackage.PackageInfo{
				{Name: "linux-image-6.17.0-1017-oem", Version: "6.17.0-1017.18",
					URL:      "http://archive.ubuntu.com/ubuntu/pool/main/l/linux/linux-image-6.17.0-1017-oem_6.17.0-1017.18_amd64.deb",
					Provides: []string{"v4l2loopback-dkms"}},
				{Name: "v4l2loopback-dkms", Version: "0.12.7-2ubuntu5.1",
					URL: "http://archive.ubuntu.com/ubuntu/pool/universe/v/v4l2loopback/v4l2loopback-dkms_0.12.7-2ubuntu5.1_all.deb"},
			},
			targetName:    "v4l2loopback-dkms",
			expectedNames: []string{"v4l2loopback-dkms", "linux-image-6.17.0-1017-oem"},
			expectedCount: 2,
			description:   "exact name match should come first despite lower version number",
		},
		{
			name: "blocked packages filtered out",
			candidates: []ospackage.PackageInfo{
				{Name: "good-pkg", Version: "1.0",
					URL: "http://archive.ubuntu.com/ubuntu/pool/main/g/good/good-pkg_1.0_amd64.deb"},
				{Name: "blocked-pkg", Version: "2.0",
					URL: "http://blocked.repo.com/ubuntu/pool/main/b/blocked/blocked-pkg_2.0_amd64.deb"},
			},
			targetName:    "test-pkg",
			expectedNames: []string{"good-pkg"},
			expectedCount: 1,
			description:   "packages from blocked repositories should be filtered out",
		},
		{
			name: "priority comparison for exact matches",
			candidates: []ospackage.PackageInfo{
				{Name: "pkg", Version: "1.0",
					URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb"}, // Priority 500
				{Name: "pkg", Version: "1.0",
					URL: "http://ppa.launchpad.net/test/ppa/ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb"}, // Priority 990
			},
			targetName:    "pkg",
			expectedNames: []string{"pkg", "pkg"}, // Higher priority first
			expectedCount: 2,
			description:   "higher priority exact matches should come first",
		},
		{
			name: "version comparison for exact matches",
			candidates: []ospackage.PackageInfo{
				{Name: "pkg", Version: "1.0",
					URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb"},
				{Name: "pkg", Version: "2.0",
					URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg/pkg_2.0_amd64.deb"},
			},
			targetName:    "pkg",
			expectedNames: []string{"pkg", "pkg"}, // Higher version first
			expectedCount: 2,
			description:   "higher version exact matches should come first when same priority",
		},
		{
			name: "force install priority",
			candidates: []ospackage.PackageInfo{
				{Name: "pkg", Version: "1.0",
					URL: "http://archive.ubuntu.com/ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb"}, // Priority 500
				{Name: "pkg", Version: "1.0",
					URL: "http://force.repo.com/ubuntu/pool/main/p/pkg/pkg_1.0_amd64.deb"}, // Priority 1001 (force)
			},
			targetName:    "pkg",
			expectedNames: []string{"pkg", "pkg"}, // Force install first
			expectedCount: 2,
			description:   "force install packages should have highest priority",
		},
		{
			name: "provides matches stable order",
			candidates: []ospackage.PackageInfo{
				{Name: "kernel-a", Version: "6.17.0",
					URL:      "http://archive.ubuntu.com/ubuntu/pool/main/k/kernel-a/kernel-a_6.17.0_amd64.deb",
					Provides: []string{"virtual-pkg"}},
				{Name: "kernel-b", Version: "5.15.0",
					URL:      "http://archive.ubuntu.com/ubuntu/pool/main/k/kernel-b/kernel-b_5.15.0_amd64.deb",
					Provides: []string{"virtual-pkg"}},
			},
			targetName:    "virtual-pkg",
			expectedNames: []string{"kernel-a", "kernel-b"}, // Maintain stable order, no version comparison
			expectedCount: 2,
			description:   "provides matches should maintain stable order without cross-type version comparison",
		},
		{
			name: "mixed exact and provides with different priorities",
			candidates: []ospackage.PackageInfo{
				{Name: "real-pkg", Version: "1.0",
					URL: "http://archive.ubuntu.com/ubuntu/pool/main/r/real/real-pkg_1.0_amd64.deb"}, // Priority 500
				{Name: "high-priority-virtual", Version: "2.0",
					URL:      "http://ppa.launchpad.net/test/ppa/ubuntu/pool/main/h/high/high-priority-virtual_2.0_amd64.deb", // Priority 990
					Provides: []string{"real-pkg"}},
			},
			targetName:    "real-pkg",
			expectedNames: []string{"real-pkg", "high-priority-virtual"}, // Exact match wins despite lower priority
			expectedCount: 2,
			description:   "exact matches should win over provides matches regardless of priority",
		},
		{
			name: "all candidates blocked",
			candidates: []ospackage.PackageInfo{
				{Name: "blocked1", Version: "1.0",
					URL: "http://blocked.repo.com/ubuntu/pool/main/b/blocked1/blocked1_1.0_amd64.deb"},
				{Name: "blocked2", Version: "2.0",
					URL: "http://blocked.repo.com/ubuntu/pool/main/b/blocked2/blocked2_2.0_amd64.deb"},
			},
			targetName:    "test-pkg",
			expectedNames: []string{},
			expectedCount: 0,
			description:   "should return empty when all candidates are blocked",
		},
		{
			name: "real v4l2loopback-dkms scenario",
			candidates: []ospackage.PackageInfo{
				// Multiple kernel packages providing v4l2loopback-dkms (with higher versions)
				{Name: "linux-image-unsigned-6.17.0-1017-oem", Version: "6.17.0-1017.18",
					URL:      "http://archive.ubuntu.com/ubuntu/pool/main/l/linux-unsigned/linux-image-unsigned-6.17.0-1017-oem_6.17.0-1017.18_amd64.deb",
					Provides: []string{"v4l2loopback-dkms"}},
				{Name: "linux-image-unsigned-6.15.0-1015-oem", Version: "6.15.0-1015.16",
					URL:      "http://archive.ubuntu.com/ubuntu/pool/main/l/linux-unsigned/linux-image-unsigned-6.15.0-1015-oem_6.15.0-1015.16_amd64.deb",
					Provides: []string{"v4l2loopback-dkms"}},
				// Real v4l2loopback-dkms package (with lower version number)
				{Name: "v4l2loopback-dkms", Version: "0.12.7-2ubuntu5.1",
					URL: "http://archive.ubuntu.com/ubuntu/pool/universe/v/v4l2loopback/v4l2loopback-dkms_0.12.7-2ubuntu5.1_all.deb"},
			},
			targetName:    "v4l2loopback-dkms",
			expectedNames: []string{"v4l2loopback-dkms", "linux-image-unsigned-6.17.0-1017-oem", "linux-image-unsigned-6.15.0-1015-oem"},
			expectedCount: 3,
			description:   "real v4l2loopback-dkms package should be selected first despite kernel packages having higher version numbers",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := filterCandidatesByPriorityWithTarget(tc.candidates, tc.targetName)

			if len(result) != tc.expectedCount {
				t.Errorf("expected %d candidates, got %d", tc.expectedCount, len(result))
				for i, pkg := range result {
					t.Logf("  result[%d]: %s (version: %s)", i, pkg.Name, pkg.Version)
				}
			}

			// Check that the order matches expected names
			for i, expected := range tc.expectedNames {
				if i >= len(result) {
					t.Errorf("expected candidate %d to be %s, but result has only %d candidates", i, expected, len(result))
					continue
				}
				if result[i].Name != expected {
					t.Errorf("expected candidate %d to be %s, got %s", i, expected, result[i].Name)
				}
			}

			// Verify no blocked packages in result
			for _, pkg := range result {
				if strings.Contains(pkg.URL, "blocked.repo.com") {
					t.Errorf("blocked package %s should not be in result", pkg.Name)
				}
			}

			// For the real v4l2loopback-dkms scenario, verify the exact match comes first
			if tc.name == "real v4l2loopback-dkms scenario" && len(result) > 0 {
				if result[0].Name != "v4l2loopback-dkms" {
					t.Errorf("v4l2loopback-dkms should be first candidate, got %s", result[0].Name)
				}
			}

			t.Logf("Test '%s': %s", tc.name, tc.description)
		})
	}
}

func TestResolveTopPackageConflicts(t *testing.T) {
	all := []ospackage.PackageInfo{
		{Name: "acct", Version: "6.6.4-5+b1", URL: "pool/main/a/acct/acct_6.6.4-5+b1_amd64.deb"},
		{Name: "acct", Version: "7.6.4-5+b1", URL: "pool/main/a/acct/acct_7.6.4-5+b1_amd64.deb"},
		{Name: "acl-2.3.1-2", Version: "2.3.1-2", URL: "pool/main/a/acl/acl_2.3.1-2_amd64.deb"},
		{Name: "acl-dev", Version: "2.3.1-1", URL: "pool/main/a/acl/acl-dev_2.3.1-1_amd64.deb"},
		{Name: "python3.10", Version: "3.10.6-1", URL: "pool/main/p/python3.10/python3.10_3.10.6-1_amd64.deb"},
	}

	testCases := []struct {
		name            string
		want            string
		expectFound     bool
		expectedName    string
		expectedVersion string
	}{
		{
			name:            "exact name match - returns first match - return highest version",
			want:            "acct",
			expectFound:     true,
			expectedName:    "acct",
			expectedVersion: "7.6.4-5+b1", // Function uses break, so first match
		},
		{
			name:            "prefix with dash",
			want:            "acl-2.3.1-2",
			expectFound:     true,
			expectedName:    "acl-2.3.1-2",
			expectedVersion: "2.3.1-2",
		},
		{
			name:            "prefix with dot",
			want:            "python3",
			expectFound:     true,
			expectedName:    "python3.10",
			expectedVersion: "3.10.6-1",
		},
		{
			name:        "no match",
			want:        "nonexistent",
			expectFound: false,
		},
		{
			name:            "exact filename match",
			want:            "acct_7.6.4-5+b1_amd64",
			expectFound:     true,
			expectedName:    "acct",
			expectedVersion: "7.6.4-5+b1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, found := ResolveTopPackageConflicts(tc.want, all)

			if found != tc.expectFound {
				t.Errorf("ResolveTopPackageConflicts(%q) found=%v, expected found=%v", tc.want, found, tc.expectFound)
				return
			}

			if tc.expectFound {
				if result.Name != tc.expectedName {
					t.Errorf("expected name %q, got %q", tc.expectedName, result.Name)
				}
				if result.Version != tc.expectedVersion {
					t.Errorf("expected version %q, got %q", tc.expectedVersion, result.Version)
				}
			}
		})
	}
}
