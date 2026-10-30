package debutils

import (
	"slices"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
)

var edgePackVersions = []ospackage.PackageInfo{
	{Name: "intel-edge-base-standard", Version: "1.0.2-1", URL: "http://edge.example.com/b_1.0.2-1_amd64.deb"},
	{Name: "intel-edge-base-standard", Version: "1.0.10-1", URL: "http://edge.example.com/b_1.0.10-1_amd64.deb"},
	{Name: "intel-edge-base-standard", Version: "2:2.0.0-1", URL: "http://edge.example.com/b_2.0.0-1_amd64.deb"},
	{Name: "intel-edge-npu", Version: "1:1.0.1-3", URL: "http://edge.example.com/n_1.0.1-3_amd64.deb"},
}

func TestSplitVersionPrefixRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		want, name, prefix string
		ok                 bool
	}{
		{"intel-edge-base-standard_1.0*", "intel-edge-base-standard", "1.0", true},
		{"libfoo2.1_2.1-3ubuntu*", "libfoo2.1", "2.1-3ubuntu", true},
		{"intel-edge-*", "", "", false},
		{"curl", "", "", false},
		{"curl_8.5.0-2ubuntu10", "", "", false},
		{"curl_*", "", "", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			name, prefix, ok := splitVersionPrefixRequest(tt.want)
			if name != tt.name || prefix != tt.prefix || ok != tt.ok {
				t.Errorf("splitVersionPrefixRequest(%q) = %q, %q, %v", tt.want, name, prefix, ok)
			}
		})
	}
}

func TestResolveVersionPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, prefix, wantVersion string
		found                     bool
	}{
		{"intel-edge-base-standard", "1.0", "1.0.10-1", true},
		{"intel-edge-base-standard", "2.0", "2:2.0.0-1", true},
		{"intel-edge-npu", "1.0", "1:1.0.1-3", true},
		{"intel-edge-base-standard", "3", "", false},
		{"intel-edge-ipu", "1.0", "", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name+"_"+tt.prefix, func(t *testing.T) {
			t.Parallel()
			got, found := resolveVersionPrefix(tt.name, tt.prefix, edgePackVersions)
			if found != tt.found || got.Version != tt.wantVersion {
				t.Errorf("resolveVersionPrefix(%q, %q) = %q, %v; want %q, %v",
					tt.name, tt.prefix, got.Version, found, tt.wantVersion, tt.found)
			}
		})
	}
}

func TestMatchRequestedVersionPrefix(t *testing.T) {
	got, err := MatchRequested([]string{"intel-edge-base-standard_1.0*", "intel-edge-npu_1.0*"}, edgePackVersions)
	if err != nil {
		t.Fatalf("MatchRequested: %v", err)
	}
	var versions []string
	for _, pi := range got {
		versions = append(versions, pi.Name+"="+pi.Version)
	}
	want := []string{"intel-edge-base-standard=1.0.10-1", "intel-edge-npu=1:1.0.1-3"}
	if !slices.Equal(versions, want) {
		t.Errorf("MatchRequested = %v, want %v", versions, want)
	}
}

func TestPinVersionPrefixRequests(t *testing.T) {
	t.Parallel()
	pkgs := []string{"curl", "intel-edge-base-standard_1.0*", "intel-edge-npu_1.0*", "libtbb12_2021.11.0-2ubuntu2",
		"intel-edge-ipu_1.0*"}
	got := PinVersionPrefixRequests(pkgs, edgePackVersions)
	want := []string{"curl", "intel-edge-base-standard_1.0.10-1", "intel-edge-npu_1:1.0.1-3",
		"libtbb12_2021.11.0-2ubuntu2", "intel-edge-ipu_1.0*"}
	if !slices.Equal(got, want) {
		t.Errorf("PinVersionPrefixRequests = %v, want %v", got, want)
	}
}
