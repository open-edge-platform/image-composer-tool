package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/provider"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

// gateFakeProvider is a create-only provider: it deliberately does NOT implement
// provider.OverlayCapable, so provider.SupportsOverlay reports false for it. It
// records whether the phases the capability gate is supposed to prevent were
// entered, and lets a test dictate what PostProcess returns.
type gateFakeProvider struct {
	os, dist, arch string

	preEntered   bool
	buildEntered bool
	postEntered  bool

	postBuildErr error // buildErr as PostProcess received it
	postResult   func(buildErr error) error
}

func (f *gateFakeProvider) Name(dist, arch string) string {
	return system.GetProviderId(f.os, dist, arch)
}

func (f *gateFakeProvider) Init(dist, arch string) error { return nil }

func (f *gateFakeProvider) PreProcess(t *config.ImageTemplate) error {
	f.preEntered = true
	return nil
}

func (f *gateFakeProvider) BuildImage(t *config.ImageTemplate) error {
	f.buildEntered = true
	return nil
}

func (f *gateFakeProvider) PostProcess(t *config.ImageTemplate, buildErr error) error {
	f.postEntered = true
	f.postBuildErr = buildErr
	if f.postResult != nil {
		return f.postResult(buildErr)
	}
	return buildErr // azl/emt/rcd-style passthrough on successful cleanup
}

// compile-time guard: the fake must stay create-only for these tests to mean
// anything. If provider.OverlayCapable is ever satisfied incidentally (e.g. a
// SupportsOverlay method is added to the Provider interface itself), this test
// file should fail to express its intent loudly rather than silently pass.
func TestGateFakeProviderIsCreateOnly(t *testing.T) {
	var p provider.Provider = &gateFakeProvider{}
	if _, ok := p.(provider.OverlayCapable); ok {
		t.Fatal("gateFakeProvider must not implement provider.OverlayCapable")
	}
	if provider.SupportsOverlay(p, "ubuntu24", "x86_64") {
		t.Fatal("provider.SupportsOverlay must report false for a create-only provider")
	}
}

// withFakeProvider swaps the initProvider seam for the duration of a test so the
// capability gate can be exercised without the network and disk I/O the real
// provider Init performs.
func withFakeProvider(t *testing.T, f *gateFakeProvider) {
	t.Helper()
	orig := initProvider
	initProvider = func(os, dist, arch string) (provider.Provider, error) {
		f.os, f.dist, f.arch = os, dist, arch
		return f, nil
	}
	t.Cleanup(func() { initProvider = orig })
}

// createOverlayTestTemplate writes a minimal overlay-mode template targeting the
// given OS/dist/arch, with a real (empty) file as the baseline source so template
// loading and validation succeed and the build reaches the capability gate.
func createOverlayTestTemplate(t *testing.T, osName, dist, arch string) string {
	t.Helper()
	tmpDir := t.TempDir()

	baseline := filepath.Join(tmpDir, "baseline.raw")
	if err := os.WriteFile(baseline, []byte("not-a-real-image"), 0o644); err != nil {
		t.Fatalf("failed to create baseline stub: %v", err)
	}

	templatePath := filepath.Join(tmpDir, "overlay-template.yml")
	templateContent := "image:\n" +
		"  name: \"overlay-test-image\"\n" +
		"  version: \"1.0.0\"\n" +
		"\n" +
		"target:\n" +
		"  os: \"" + osName + "\"\n" +
		"  dist: \"" + dist + "\"\n" +
		"  arch: \"" + arch + "\"\n" +
		"  imageType: \"raw\"\n" +
		"\n" +
		"baseline:\n" +
		"  mode: overlay\n" +
		"  source:\n" +
		"    path: \"" + baseline + "\"\n" +
		"    format: raw\n" +
		"\n" +
		"systemConfig:\n" +
		"  packages:\n" +
		"    - \"bash\"\n"

	if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
		t.Fatalf("failed to create overlay test template: %v", err)
	}
	return templatePath
}

// TestExecuteBuild_OverlayOnCreateOnlyProviderFailsFast asserts the capability
// gate in executeBuild rejects an overlay-mode template aimed at a provider that
// does not implement provider.OverlayCapable, with an actionable message, and
// crucially that it does so BEFORE entering the provider's PreProcess/BuildImage.
// Without the gate the build would fall through to the create-mode pipeline on a
// package list that template merging already stripped, producing either a failure
// deep in image assembly or an image missing its base toolchain.
func TestExecuteBuild_OverlayOnCreateOnlyProviderFailsFast(t *testing.T) {
	defer resetBuildFlags()

	fake := &gateFakeProvider{}
	withFakeProvider(t, fake)

	tmpl := createOverlayTestTemplate(t, "ubuntu", "ubuntu24", "x86_64")

	cmd := createBuildCommand()
	err := executeBuild(cmd, []string{tmpl})
	if err == nil {
		t.Fatal("expected executeBuild to fail for an overlay template on a create-only provider")
	}

	// The message must name overlay mode and point at a remedy — this is the
	// user-facing contract that replaces the silent create-mode fallback.
	for _, want := range []string{"overlay mode", "only supports create-mode"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %v", want, err)
		}
	}

	// The whole point of the gate is that no build work starts.
	if fake.preEntered {
		t.Error("PreProcess was entered; the gate must reject before pre-processing")
	}
	if fake.buildEntered {
		t.Error("BuildImage was entered; the gate must reject before the build")
	}

	// PostProcess still runs (via the goto post path) so cleanup is not skipped,
	// and it must receive the gate's error as the incoming buildErr.
	if !fake.postEntered {
		t.Error("PostProcess was not entered; cleanup must still run after the gate rejects")
	}
	if fake.postBuildErr == nil {
		t.Error("PostProcess should have received the gate error as buildErr")
	}
}

// TestExecuteBuild_PostProcessErrorClassification covers how executeBuild
// classifies what PostProcess returns. The distinction matters because two
// providers behave differently on a failed build:
//
//   - azl/emt/rcd hand the exact buildErr value straight back once cleanup
//     succeeded; relabelling that as "post-processing failed" would misattribute
//     a build failure to cleanup.
//   - overlay.Builder.Postprocess returns errors.Join(buildErr, cleanupErr) when
//     cleanup fails after a failed build, so the caller sees both the root cause
//     and the leaked mount/loop device.
//
// An errors.Is check cannot tell these apart: it walks the joined tree, matches
// buildErr and drops the cleanup failure. Identity comparison distinguishes them.
func TestExecuteBuild_PostProcessErrorClassification(t *testing.T) {
	t.Run("identical buildErr passthrough is not relabelled as post-processing failure", func(t *testing.T) {
		defer resetBuildFlags()

		fake := &gateFakeProvider{
			// azl/emt/rcd behavior: return the received buildErr unchanged.
			postResult: func(buildErr error) error { return buildErr },
		}
		withFakeProvider(t, fake)

		tmpl := createOverlayTestTemplate(t, "ubuntu", "ubuntu24", "x86_64")
		err := executeBuild(createBuildCommand(), []string{tmpl})
		if err == nil {
			t.Fatal("expected the underlying build error to be returned")
		}
		if strings.Contains(err.Error(), "post-processing failed") {
			t.Errorf("passthrough buildErr must not be relabelled as a post-processing failure; got: %v", err)
		}
		// The original cause must survive intact.
		if !strings.Contains(err.Error(), "overlay mode") {
			t.Errorf("expected the original build error to be surfaced; got: %v", err)
		}
	})

	t.Run("joined build and cleanup error surfaces the cleanup failure", func(t *testing.T) {
		defer resetBuildFlags()

		cleanupErr := errors.New("leaked loop device /dev/loop7")
		fake := &gateFakeProvider{
			// overlay.Builder.Postprocess behavior on build-failed + cleanup-failed.
			postResult: func(buildErr error) error {
				return errors.Join(buildErr, fmt.Errorf("overlay postprocess: cleanup failed: %w", cleanupErr))
			},
		}
		withFakeProvider(t, fake)

		tmpl := createOverlayTestTemplate(t, "ubuntu", "ubuntu24", "x86_64")
		err := executeBuild(createBuildCommand(), []string{tmpl})
		if err == nil {
			t.Fatal("expected an error combining the build failure and the cleanup failure")
		}
		// This is the assertion that fails against an errors.Is check: the joined
		// error would be treated as "just the build error" and returned unlabelled,
		// hiding the leak.
		if !strings.Contains(err.Error(), "post-processing failed") {
			t.Errorf("a joined build+cleanup error must be surfaced as a post-processing failure; got: %v", err)
		}
		if !errors.Is(err, cleanupErr) {
			t.Errorf("the cleanup error must remain matchable via errors.Is; got: %v", err)
		}
	})
}
