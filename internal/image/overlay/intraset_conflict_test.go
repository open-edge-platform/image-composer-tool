package overlay

import "testing"

// TestClassifyIntraSetConflicts covers the pure intra-set conflict classifier:
// two DIFFERENT to-install packages that conflict with each other must be
// flagged even though neither is in the baseline, whether the conflict names a
// real package directly (libcurl4-gnutls-dev vs libcurl4-openssl-dev) or a
// virtual capability shared via Provides: (the ROS 2 univloc "-sse"/"-lze"
// hardware variants). A package's own Conflicts: is never flagged against
// itself, and a duplicate/bidirectional declaration reports the pair once.
func TestClassifyIntraSetConflicts(t *testing.T) {
	tests := []struct {
		name      string
		resolved  []ResolvedPackage
		conflicts []ArtifactConflict
		provides  []ArtifactProvides
		wantPairs [][2]string // unordered pairs, order within pair does not matter
	}{
		{
			name: "direct conflict between two to-install packages fires",
			resolved: []ResolvedPackage{
				{Name: "libcurl4-gnutls-dev", Version: "8.5.0-2ubuntu10.11"},
				{Name: "libcurl4-openssl-dev", Version: "8.5.0-2ubuntu10.11"},
			},
			conflicts: []ArtifactConflict{
				{Package: "libcurl4-gnutls-dev", Conflicts: DependencyAlternative{Name: "libcurl4-openssl-dev"}},
			},
			wantPairs: [][2]string{{"libcurl4-gnutls-dev", "libcurl4-openssl-dev"}},
		},
		{
			name: "conflict against a virtual name is resolved through Provides",
			resolved: []ResolvedPackage{
				{Name: "ros-jazzy-univloc-slam-sse", Version: "2.3-2"},
				{Name: "ros-jazzy-univloc-slam-lze", Version: "2.3-2"},
			},
			conflicts: []ArtifactConflict{
				{Package: "ros-jazzy-univloc-slam-lze", Conflicts: DependencyAlternative{Name: "ros-jazzy-univloc-slam"}},
			},
			provides: []ArtifactProvides{
				{Package: "ros-jazzy-univloc-slam-sse", Provides: []string{"ros-jazzy-univloc-slam"}},
			},
			wantPairs: [][2]string{{"ros-jazzy-univloc-slam-lze", "ros-jazzy-univloc-slam-sse"}},
		},
		{
			name: "a package never conflicts with itself",
			resolved: []ResolvedPackage{
				{Name: "solo-pkg", Version: "1.0"},
			},
			conflicts: []ArtifactConflict{
				{Package: "solo-pkg", Conflicts: DependencyAlternative{Name: "solo-pkg"}},
			},
			wantPairs: nil,
		},
		{
			name: "conflict against a package not in the to-install set is a no-op",
			resolved: []ResolvedPackage{
				{Name: "newpkg", Version: "1.0"},
			},
			conflicts: []ArtifactConflict{
				{Package: "newpkg", Conflicts: DependencyAlternative{Name: "unrelated-pkg"}},
			},
			wantPairs: nil,
		},
		{
			name: "a bidirectional declaration reports the pair once",
			resolved: []ResolvedPackage{
				{Name: "pkg-a", Version: "1.0"},
				{Name: "pkg-b", Version: "1.0"},
			},
			conflicts: []ArtifactConflict{
				{Package: "pkg-a", Conflicts: DependencyAlternative{Name: "pkg-b"}},
				{Package: "pkg-b", Conflicts: DependencyAlternative{Name: "pkg-a"}},
			},
			wantPairs: [][2]string{{"pkg-a", "pkg-b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions := classifyIntraSetConflicts(PackageManagerAPT, tt.resolved, tt.conflicts, tt.provides)
			if len(actions) != len(tt.wantPairs) {
				t.Fatalf("got %d action(s), want %d: %+v", len(actions), len(tt.wantPairs), actions)
			}
			gotPairs := make(map[[2]string]bool, len(actions))
			for _, a := range actions {
				if a.Type != ActionConflict {
					t.Errorf("type = %s, want %s", a.Type, ActionConflict)
				}
				pair := [2]string{a.Package, a.ConflictWith}
				if pair[0] > pair[1] {
					pair[0], pair[1] = pair[1], pair[0]
				}
				gotPairs[pair] = true
			}
			for _, want := range tt.wantPairs {
				if want[0] > want[1] {
					want[0], want[1] = want[1], want[0]
				}
				if !gotPairs[want] {
					t.Errorf("missing expected conflicting pair %v in %+v", want, actions)
				}
			}
		})
	}
}
