package main

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/provider"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

// fakeProvider implements the Provider interface and records lifecycle calls.
type fakeProvider struct {
	nameCalled bool
	inited     bool
	pre        bool
	built      bool
	post       bool
}

func (f *fakeProvider) Name(dist, arch string) string {
	f.nameCalled = true
	// Register under the same key pattern as azl to match registry expectations.
	return system.GetProviderId("azure-linux", dist, arch)
}

func (f *fakeProvider) Init(dist, arch string) error             { f.inited = true; return nil }
func (f *fakeProvider) PreProcess(t *config.ImageTemplate) error { f.pre = true; return nil }
func (f *fakeProvider) BuildImage(t *config.ImageTemplate) error { f.built = true; return nil }
func (f *fakeProvider) PostProcess(t *config.ImageTemplate, err error) error {
	f.post = true
	return nil
}

func TestProvider_Register_Get_AndLifecycle(t *testing.T) {
	f := &fakeProvider{}
	dist := "testdist"
	arch := "testarch"

	// Register fake
	provider.Register(f, dist, arch)

	// Retrieve by computed id
	id := system.GetProviderId("azure-linux", dist, arch)
	got, ok := provider.Get(id)
	if !ok {
		t.Fatalf("expected provider.Get(%q) to succeed", id)
	}

	// Run lifecycle
	tmpl := &config.ImageTemplate{}
	if err := got.Init(dist, arch); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := got.PreProcess(tmpl); err != nil {
		t.Fatalf("PreProcess failed: %v", err)
	}
	if err := got.BuildImage(tmpl); err != nil {
		t.Fatalf("BuildImage failed: %v", err)
	}
	if err := got.PostProcess(tmpl, nil); err != nil {
		t.Fatalf("PostProcess failed: %v", err)
	}

	// Verify calls recorded
	if !f.nameCalled || !f.inited || !f.pre || !f.built || !f.post {
		t.Fatalf("expected all lifecycle methods to be called; got name=%v init=%v pre=%v built=%v post=%v",
			f.nameCalled, f.inited, f.pre, f.built, f.post)
	}
}
