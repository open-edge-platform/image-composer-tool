package pkgquery

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/rpmutils"
)

func TestNewQuerierRouter(t *testing.T) {
	debRepos := []debutils.RepoConfig{{Name: "ubuntu-main", PkgPrefix: "http://example.com/ubuntu"}}
	rpmRepos := []rpmutils.RepoConfig{{Name: "azl-base", URL: "http://example.com/azl"}}

	tests := []struct {
		os          string
		expectType  string
		expectError bool
	}{
		{"ubuntu", "*pkgquery.DebAdapter", false},
		{"ubuntu24", "*pkgquery.DebAdapter", false},
		{"debian", "*pkgquery.DebAdapter", false},
		{"debian13", "*pkgquery.DebAdapter", false},
		{"elxr", "*pkgquery.DebAdapter", false},
		{"elxr12", "*pkgquery.DebAdapter", false},
		{"azure-linux", "*pkgquery.RpmAdapter", false},
		{"azl3", "*pkgquery.RpmAdapter", false},
		{"emt", "*pkgquery.RpmAdapter", false},
		{"emt3", "*pkgquery.RpmAdapter", false},
		{"unsupported-os", "", true},
		{"windows", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.os, func(t *testing.T) {
			q, err := NewQuerier(tt.os, debRepos, rpmRepos)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for OS %q, got nil", tt.os)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for OS %q: %v", tt.os, err)
			}
			if q == nil {
				t.Fatalf("expected non-nil querier for OS %q", tt.os)
			}

			switch tt.expectType {
			case "*pkgquery.DebAdapter":
				if _, ok := q.(*DebAdapter); !ok {
					t.Errorf("expected *DebAdapter for OS %q, got %T", tt.os, q)
				}
			case "*pkgquery.RpmAdapter":
				if _, ok := q.(*RpmAdapter); !ok {
					t.Errorf("expected *RpmAdapter for OS %q, got %T", tt.os, q)
				}
			}
		})
	}
}

func TestDebAdapterLookupStates(t *testing.T) {
	ctx := context.Background()

	// 1. Success case: stub metadata contains curl and nginx
	debRepos := []debutils.RepoConfig{
		{Name: "main-repo", PkgPrefix: "http://example.com/deb"},
	}
	adapter, err := NewDebAdapter(debRepos)
	if err != nil {
		t.Fatalf("NewDebAdapter failed: %v", err)
	}

	adapter.parseMetadataFunc = func(baseURL, pkggz, releaseFile, releaseSign, pbGPGKey, buildPath, arch string, packageFilter []string) ([]ospackage.PackageInfo, error) {
		return []ospackage.PackageInfo{
			{Name: "curl", PkgName: "curl", Version: "7.88.1", Description: "Command line tool for transferring data"},
			{Name: "nginx", PkgName: "nginx", Version: "1.22.1", Description: "High-performance HTTP server"},
		}, nil
	}

	results, err := adapter.Lookup(ctx, []string{"curl", "nginx", "nonexistent-pkg"})
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// curl -> verified
	if results[0].State != StateVerified || results[0].Repo != "main-repo" || results[0].Version != "7.88.1" {
		t.Errorf("unexpected result for curl: %+v", results[0])
	}
	// nginx -> verified
	if results[1].State != StateVerified || results[1].Repo != "main-repo" || results[1].Version != "1.22.1" {
		t.Errorf("unexpected result for nginx: %+v", results[1])
	}
	// nonexistent-pkg -> not_available
	if results[2].State != StateNotFound {
		t.Errorf("expected StateNotFound for nonexistent-pkg, got %s", results[2].State)
	}

	// 2. Unverified case: repo fetch failure
	failAdapter, _ := NewDebAdapter(debRepos)
	failAdapter.parseMetadataFunc = func(baseURL, pkggz, releaseFile, releaseSign, pbGPGKey, buildPath, arch string, packageFilter []string) ([]ospackage.PackageInfo, error) {
		return nil, errors.New("network timeout connecting to repo")
	}

	failResults, err := failAdapter.Lookup(ctx, []string{"curl"})
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if len(failResults) != 1 || failResults[0].State != StateUnverified {
		t.Errorf("expected StateUnverified when repo fails, got %+v", failResults[0])
	}
}

func TestDebAdapterSearch(t *testing.T) {
	ctx := context.Background()
	debRepos := []debutils.RepoConfig{{Name: "main-repo", PkgPrefix: "http://example.com/deb"}}
	adapter, err := NewDebAdapter(debRepos)
	if err != nil {
		t.Fatalf("NewDebAdapter failed: %v", err)
	}

	adapter.parseMetadataFunc = func(baseURL, pkggz, releaseFile, releaseSign, pbGPGKey, buildPath, arch string, packageFilter []string) ([]ospackage.PackageInfo, error) {
		return []ospackage.PackageInfo{
			{Name: "librealsense2-dkms", PkgName: "librealsense2-dkms", Version: "2.56.5", Description: "RealSense kernel modules"},
			{Name: "librealsense2", PkgName: "librealsense2", Version: "2.56.5", Description: "RealSense SDK library"},
			{Name: "ros-jazzy-librealsense2", PkgName: "ros-jazzy-librealsense2", Version: "1.0.0", Description: "ROS 2 RealSense wrapper"},
			{Name: "nginx", PkgName: "nginx", Version: "1.22.1", Description: "Web server"},
		}, nil
	}

	results, err := adapter.Search(ctx, "realsense", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results for 'realsense', got %d", len(results))
	}
	for _, res := range results {
		if res.State != StateVerified {
			t.Errorf("expected StateVerified for search result, got %s", res.State)
		}
	}
}

func TestRpmAdapterLookupAndSearch(t *testing.T) {
	ctx := context.Background()
	rpmRepos := []rpmutils.RepoConfig{{Name: "azl-base", URL: "http://example.com/azl"}}
	adapter, err := NewRpmAdapter(rpmRepos)
	if err != nil {
		t.Fatalf("NewRpmAdapter failed: %v", err)
	}

	adapter.parseMetadataFunc = func(baseURL, gzHref string, packageFilter []string) ([]ospackage.PackageInfo, error) {
		return []ospackage.PackageInfo{
			{Name: "systemd", PkgName: "systemd", Version: "254.1", Description: "System and Service Manager"},
			{Name: "kernel-headers", PkgName: "kernel-headers", Version: "6.6.0", Description: "Linux kernel headers"},
		}, nil
	}

	results, err := adapter.Lookup(ctx, []string{"systemd", "missing-rpm"})
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].State != StateVerified || results[0].Repo != "azl-base" {
		t.Errorf("unexpected result for systemd: %+v", results[0])
	}
	if results[1].State != StateNotFound {
		t.Errorf("expected StateNotFound for missing-rpm, got %s", results[1].State)
	}

	searchResults, err := adapter.Search(ctx, "kernel", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(searchResults) != 1 || searchResults[0].Name != "kernel-headers" {
		t.Errorf("unexpected search results: %+v", searchResults)
	}
}

func TestDebAdapterConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	debRepos := []debutils.RepoConfig{{Name: "main-repo", PkgPrefix: "http://example.com/deb"}}
	adapter, err := NewDebAdapter(debRepos)
	if err != nil {
		t.Fatalf("NewDebAdapter failed: %v", err)
	}

	adapter.parseMetadataFunc = func(baseURL, pkggz, releaseFile, releaseSign, pbGPGKey, buildPath, arch string, packageFilter []string) ([]ospackage.PackageInfo, error) {
		return []ospackage.PackageInfo{
			{Name: "curl", PkgName: "curl", Version: "7.88.1", Description: "Command line tool"},
		}, nil
	}

	var wg sync.WaitGroup
	workers := 20
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, err := adapter.Lookup(ctx, []string{"curl", "unknown"})
			if err != nil {
				t.Errorf("concurrent lookup failed: %v", err)
			}
			_, err = adapter.Search(ctx, "curl", 5)
			if err != nil {
				t.Errorf("concurrent search failed: %v", err)
			}
		}()
	}

	wg.Wait()
}
