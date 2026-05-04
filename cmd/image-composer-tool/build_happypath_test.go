//go:build provider_integration

// NOTE: This test is guarded by a build tag because it depends on internal/provider
// interfaces and registry functions in your repository. Enable with:
//    go test -tags provider_integration ./cmd/image-composer-tool -run TestBuild_HappyPath -v
//
// You may need to tweak method names to match your internal provider interface.

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/provider"
)

// ---- Fake provider that records calls ----
type fakeProvider struct {
	inited bool
	built  bool
	valid  bool
}

func (p *fakeProvider) Init(dist, arch string) error {
	p.inited = true
	return nil
}

// Adjust the signatures below if your interface differs.
func (p *fakeProvider) Build(cfg *config.Config, tmpl *config.Template) error {
	p.built = true
	return nil
}
func (p *fakeProvider) Validate(cfg *config.Config, tmpl *config.Template) error {
	p.valid = true
	return nil
}

func TestBuild_HappyPath_WithFakeProvider(t *testing.T) {
	// Arrange: register fake in the provider registry under a known ID
	id := "test-os:test-dist:test-arch"
	fp := &fakeProvider{}
	provider.Register(fp, "test-dist", "test-arch")
	config.ProviderId = id

	// Minimal CLI setup
	tmp := t.TempDir()
	template := filepath.Join(tmp, "template.yaml")
	if err := os.WriteFile(template, []byte("name: test-template\n"), 0644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	cmd := createBuildCommand()
	// We bypass InitProvider by NOT setting --os/--dist/--arch; the build code should use config.ProviderId
	// which we've already set above. If your build flow forces InitProvider, set flags accordingly.
	cmd.SetArgs([]string{
		"--template", template,
		"--workers", "2",
		"--cache-dir", filepath.Join(tmp, "cache"),
	})

	// Act
	if err := cmd.Execute(); err != nil {
		t.Fatalf("build command failed: %v", err)
	}

	// Assert
	if !fp.inited {
		t.Fatalf("expected provider.Init to be called")
	}
	if !fp.valid {
		t.Fatalf("expected provider.Validate to be called")
	}
	if !fp.built {
		t.Fatalf("expected provider.Build to be called")
	}
}
