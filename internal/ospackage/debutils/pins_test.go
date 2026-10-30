package debutils

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

// TestPackagePriorityWithPins is not parallel: it sets the package's pins.
func TestPackagePriorityWithPins(t *testing.T) {
	ConfigurePins([]Pin{
		{Packages: []string{"*"}, Origin: "edge.example.com", Priority: 1001},
		{Packages: []string{"intel-oneapi-runtime-*"}, Version: "2025.3*", Priority: 1002},
		{Packages: []string{"blocked"}, Version: "*", Priority: -1},
	})
	defer ConfigurePins(nil)
	tests := []struct {
		name string
		pkg  ospackage.PackageInfo
		want int
	}{
		{"origin pin", ospackage.PackageInfo{Name: "intel-edge-npu", Version: "1.0.1",
			URL: "https://edge.example.com/pool/n.deb"}, 1001},
		{"specific version pin wins over origin", ospackage.PackageInfo{Name: "intel-oneapi-runtime-dpcpp",
			Version: "2025.3.1-1", URL: "https://edge.example.com/pool/o.deb"}, 1002},
		{"version pin matches without epoch", ospackage.PackageInfo{Name: "intel-oneapi-runtime-mkl",
			Version: "1:2025.3.0-2", URL: "https://other.example.com/pool/m.deb"}, 1002},
		{"other version falls back", ospackage.PackageInfo{Name: "intel-oneapi-runtime-mkl",
			Version: "2026.0.0-1", URL: "https://other.example.com/pool/m.deb"},
			getRepositoryPriority("https://other.example.com/pool/m.deb")},
		{"negative pin", ospackage.PackageInfo{Name: "blocked", Version: "1.0",
			URL: "https://other.example.com/pool/b.deb"}, -1},
	}
	for _, tt := range tests {
		if got := packagePriority(tt.pkg); got != tt.want {
			t.Errorf("%s: packagePriority = %d, want %d", tt.name, got, tt.want)
		}
	}
	candidates := []ospackage.PackageInfo{
		{Name: "intel-oneapi-runtime-dpcpp", Version: "2026.0.0-1", URL: "https://other.example.com/pool/new.deb"},
		{Name: "intel-oneapi-runtime-dpcpp", Version: "2025.3.1-1", URL: "https://other.example.com/pool/old.deb"},
		{Name: "blocked", Version: "9.9", URL: "https://other.example.com/pool/b.deb"},
	}
	got := filterCandidatesByPriority(candidates)
	if len(got) != 2 || got[0].Version != "2025.3.1-1" {
		t.Errorf("filterCandidatesByPriority = %+v; want the pinned 2025.3 first and blocked removed", got)
	}
}
