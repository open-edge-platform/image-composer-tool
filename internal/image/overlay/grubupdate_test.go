package overlay

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// grubTemplate builds an overlay-mode template carrying the given kernel command
// line (empty for none).
func grubTemplate(cmdline string) *config.ImageTemplate {
	return grubTemplateFull(cmdline, "")
}

// grubTemplateFull builds an overlay-mode template carrying the given kernel
// command line and GRUB_DEFAULT override (either empty for none).
func grubTemplateFull(cmdline, grubDefault string) *config.ImageTemplate {
	return &config.ImageTemplate{
		OverlayPolicy: &config.OverlayPolicy{KernelCmdline: cmdline, GrubDefault: grubDefault},
	}
}

// stubGrubRegen wires the GRUB-regen seams for a run: commandExistsFn reports the
// generator present, sysfs mounts are no-ops, and grubRegenExec records the command
// it was asked to run. It returns a pointer to the recorded command (empty until
// the generator runs) and restores every seam on cleanup.
func stubGrubRegen(t *testing.T) *string {
	t.Helper()
	origExec, origCmdExist := grubRegenExec, commandExistsFn
	t.Cleanup(func() { grubRegenExec, commandExistsFn = origExec, origCmdExist })
	commandExistsFn = func(string, string) (bool, error) { return true, nil }
	stubSysfsMounts(t)
	var gotCmd string
	grubRegenExec = func(cmd, _ string) (string, error) { gotCmd = cmd; return "", nil }
	return &gotCmd
}

// stubDetectKernels swaps the kernel re-scan seam to return the given post-install
// kernel set, restoring it on cleanup.
func stubDetectKernels(t *testing.T, after []string) {
	t.Helper()
	orig := detectKernelsFn
	t.Cleanup(func() { detectKernelsFn = orig })
	detectKernelsFn = func(string) []string { return after }
}

// stubGrubDefaults swaps the /etc/default/grub resolve/read/write seams: resolution
// is a filesystem-free pass-through (join rootMount + rootPath, so the confinement
// walk is not exercised against a non-existent fake root), reads return the given
// content, and writes are captured into the returned pointer. A nil content makes
// reads fail with os.ErrNotExist.
func stubGrubDefaults(t *testing.T, content *string) *string {
	t.Helper()
	origResolve, origRead, origWrite := grubDefaultsResolveFn, grubDefaultReadFn, grubDefaultWriteFn
	t.Cleanup(func() {
		grubDefaultsResolveFn, grubDefaultReadFn, grubDefaultWriteFn = origResolve, origRead, origWrite
	})
	grubDefaultsResolveFn = func(rootMount, rootPath string) (string, error) {
		return filepath.Join(rootMount, rootPath), nil
	}
	grubDefaultReadFn = func(string) ([]byte, error) {
		if content == nil {
			return nil, os.ErrNotExist
		}
		return []byte(*content), nil
	}
	var written string
	grubDefaultWriteFn = func(_ string, data []byte) error { written = string(data); return nil }
	return &written
}

func TestReplaceGrubAssignment(t *testing.T) {
	const base = "GRUB_TIMEOUT=5\n" +
		"GRUB_CMDLINE_LINUX=\"quiet splash\"\n" +
		"GRUB_CMDLINE_LINUX_DEFAULT=\"loglevel=3\"\n" +
		"# trailing comment\n"

	t.Run("replaces existing and preserves _DEFAULT and comments", func(t *testing.T) {
		got := replaceGrubAssignment(base, grubCmdlineKey, "console=ttyS0")
		if !strings.Contains(got, "GRUB_CMDLINE_LINUX=\"console=ttyS0\"\n") {
			t.Errorf("GRUB_CMDLINE_LINUX not replaced: %q", got)
		}
		if !strings.Contains(got, "GRUB_CMDLINE_LINUX_DEFAULT=\"loglevel=3\"") {
			t.Errorf("_DEFAULT must be preserved: %q", got)
		}
		if !strings.Contains(got, "GRUB_TIMEOUT=5") || !strings.Contains(got, "# trailing comment") {
			t.Errorf("other lines/comments must be preserved: %q", got)
		}
		if !strings.HasSuffix(got, "\n") {
			t.Errorf("trailing newline must be preserved: %q", got)
		}
	})

	t.Run("appends when absent", func(t *testing.T) {
		got := replaceGrubAssignment("GRUB_TIMEOUT=5\n", grubCmdlineKey, "quiet")
		if !strings.Contains(got, "GRUB_CMDLINE_LINUX=\"quiet\"") {
			t.Errorf("expected appended assignment, got %q", got)
		}
		if !strings.Contains(got, "GRUB_TIMEOUT=5") {
			t.Errorf("existing content must be preserved, got %q", got)
		}
	})

	t.Run("appends when file has no trailing newline", func(t *testing.T) {
		got := replaceGrubAssignment("GRUB_TIMEOUT=5", grubCmdlineKey, "quiet")
		if got != "GRUB_TIMEOUT=5\nGRUB_CMDLINE_LINUX=\"quiet\"" {
			t.Errorf("unexpected append shape: %q", got)
		}
	})

	t.Run("value with metacharacters round-trips verbatim", func(t *testing.T) {
		// Includes a literal tab: %q would escape it to `\t`, so this also guards
		// that the value is written verbatim rather than Go-quoted. (A tab is not
		// among the characters OverlayPolicy.validate rejects.)
		val := "i915.force_probe=* root=/dev/sda1 a=b spaces\there"
		got := replaceGrubAssignment(base, grubCmdlineKey, val)
		if !strings.Contains(got, "GRUB_CMDLINE_LINUX=\""+val+"\"") {
			t.Errorf("value not preserved verbatim: %q", got)
		}
	})

	t.Run("replaces every duplicate assignment", func(t *testing.T) {
		dup := "GRUB_CMDLINE_LINUX=\"one\"\nGRUB_CMDLINE_LINUX=\"two\"\n"
		got := replaceGrubAssignment(dup, grubCmdlineKey, "final")
		if strings.Count(got, "GRUB_CMDLINE_LINUX=\"final\"") != 2 {
			t.Errorf("expected both duplicates replaced, got %q", got)
		}
		if strings.Contains(got, "\"one\"") || strings.Contains(got, "\"two\"") {
			t.Errorf("stale duplicate value survived: %q", got)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		once := replaceGrubAssignment(base, grubCmdlineKey, "console=ttyS0")
		twice := replaceGrubAssignment(once, grubCmdlineKey, "console=ttyS0")
		if once != twice {
			t.Errorf("not idempotent:\n once=%q\ntwice=%q", once, twice)
		}
	})

	t.Run("tolerates leading whitespace but ignores _DEFAULT prefix", func(t *testing.T) {
		in := "  GRUB_CMDLINE_LINUX=old\nGRUB_CMDLINE_LINUX_DEFAULT=keep\n"
		got := replaceGrubAssignment(in, grubCmdlineKey, "new")
		if !strings.Contains(got, "GRUB_CMDLINE_LINUX=\"new\"") {
			t.Errorf("indented assignment must be replaced: %q", got)
		}
		if !strings.Contains(got, "GRUB_CMDLINE_LINUX_DEFAULT=keep") {
			t.Errorf("_DEFAULT must be untouched: %q", got)
		}
	})

	t.Run("GRUB_DEFAULT with submenu path round-trips and leaves cmdline alone", func(t *testing.T) {
		val := "Advanced options for Ubuntu>Ubuntu, with Linux 6.18-intel"
		got := replaceGrubAssignment(base+"GRUB_DEFAULT=0\n", grubDefaultKey, val)
		if !strings.Contains(got, "GRUB_DEFAULT=\""+val+"\"") {
			t.Errorf("GRUB_DEFAULT submenu path not preserved verbatim: %q", got)
		}
		if !strings.Contains(got, "GRUB_CMDLINE_LINUX=\"quiet splash\"") {
			t.Errorf("GRUB_CMDLINE_LINUX must be untouched when replacing GRUB_DEFAULT: %q", got)
		}
	})
}

func TestIsGrubAssignment(t *testing.T) {
	cmdlineCases := map[string]bool{
		"GRUB_CMDLINE_LINUX=\"x\"":         true,
		"  GRUB_CMDLINE_LINUX=x":           true,
		"GRUB_CMDLINE_LINUX_DEFAULT=\"y\"": false,
		"#GRUB_CMDLINE_LINUX=x":            false,
		"GRUB_TIMEOUT=5":                   false,
		"":                                 false,
	}
	for line, want := range cmdlineCases {
		if got := isGrubAssignment(line, grubCmdlineKey); got != want {
			t.Errorf("isGrubAssignment(%q, GRUB_CMDLINE_LINUX) = %v, want %v", line, got, want)
		}
	}
	defaultCases := map[string]bool{
		"GRUB_DEFAULT=0":               true,
		"  GRUB_DEFAULT=\"saved\"":     true,
		"GRUB_DEFAULT_SOMETHING=x":     false,
		"GRUB_DISABLE_SUBMENU=y":       false,
		"GRUB_CMDLINE_LINUX=\"quiet\"": false,
	}
	for line, want := range defaultCases {
		if got := isGrubAssignment(line, grubDefaultKey); got != want {
			t.Errorf("isGrubAssignment(%q, GRUB_DEFAULT) = %v, want %v", line, got, want)
		}
	}
}

func TestRegenerateGrub_SkipsWhenNotGrub2(t *testing.T) {
	origExec := grubRegenExec
	defer func() { grubRegenExec = origExec }()
	called := false
	grubRegenExec = func(string, string) (string, error) { called = true; return "", nil }

	for _, bl := range []string{"systemd-boot", "uki", "unknown"} {
		info := &BaselineInfo{Bootloader: bl}
		if err := RegenerateGrub(grubTemplate(""), info, "/mnt/root"); err != nil {
			t.Errorf("bootloader %q with no cmdline must be a clean no-op, got %v", bl, err)
		}
	}
	if called {
		t.Error("GRUB regeneration must not run on a non-grub2 baseline")
	}
}

func TestRegenerateGrub_ErrorsOnOverrideForNonGrub(t *testing.T) {
	cases := map[string]*config.ImageTemplate{
		"kernelCmdline set": grubTemplate("quiet"),
		"grubDefault set":   grubTemplateFull("", "Advanced options for Ubuntu>Ubuntu, with Linux 6.18-intel"),
	}
	for name, tmpl := range cases {
		t.Run(name, func(t *testing.T) {
			err := RegenerateGrub(tmpl, &BaselineInfo{Bootloader: "systemd-boot"}, "/mnt/root")
			if err == nil || !strings.Contains(err.Error(), "not grub2") {
				t.Fatalf("override on a non-grub2 baseline must error, got %v", err)
			}
		})
	}
}

func TestRegenerateGrub_SkipsWhenNoChange(t *testing.T) {
	origExec := grubRegenExec
	defer func() { grubRegenExec = origExec }()
	called := false
	grubRegenExec = func(string, string) (string, error) { called = true; return "", nil }
	stubDetectKernels(t, []string{"6.8.0-40-generic"}) // same as baseline

	info := &BaselineInfo{Bootloader: "grub2", Kernels: []string{"6.8.0-40-generic"}}
	if err := RegenerateGrub(grubTemplate(""), info, "/mnt/root"); err != nil {
		t.Fatalf("no cmdline + no new kernel must skip cleanly, got %v", err)
	}
	if called {
		t.Error("GRUB regeneration must be skipped when nothing changed")
	}
}

func TestRegenerateGrub_RunsOnNewKernel(t *testing.T) {
	gotCmd := stubGrubRegen(t)
	stubDetectKernels(t, []string{"6.8.0-40-generic", "6.8.0-50-generic"})

	info := &BaselineInfo{Bootloader: "grub2", Kernels: []string{"6.8.0-40-generic"}}
	if err := RegenerateGrub(grubTemplate(""), info, "/mnt/root"); err != nil {
		t.Fatalf("RegenerateGrub: %v", err)
	}
	if *gotCmd == "" {
		t.Error("a newly added kernel must trigger GRUB regeneration")
	}
}

func TestRegenerateGrub_RunsOnCmdlineSet(t *testing.T) {
	gotCmd := stubGrubRegen(t)
	stubDetectKernels(t, []string{"6.8.0-40-generic"}) // no new kernel
	content := "GRUB_CMDLINE_LINUX=\"old\"\n"
	written := stubGrubDefaults(t, &content)

	info := &BaselineInfo{Bootloader: "grub2", Kernels: []string{"6.8.0-40-generic"}}
	if err := RegenerateGrub(grubTemplate("console=ttyS0,115200n8"), info, "/mnt/root"); err != nil {
		t.Fatalf("RegenerateGrub: %v", err)
	}
	if !strings.Contains(*written, "GRUB_CMDLINE_LINUX=\"console=ttyS0,115200n8\"") {
		t.Errorf("cmdline must be written to grub defaults, got %q", *written)
	}
	if *gotCmd == "" {
		t.Error("a set cmdline must trigger GRUB regeneration even with no new kernel")
	}
}

func TestRegenerateGrub_RunsOnGrubDefaultSet(t *testing.T) {
	gotCmd := stubGrubRegen(t)
	stubDetectKernels(t, []string{"6.8.0-40-generic"}) // no new kernel
	content := "GRUB_DEFAULT=0\nGRUB_CMDLINE_LINUX=\"quiet\"\n"
	written := stubGrubDefaults(t, &content)

	const wantDefault = "Advanced options for Ubuntu>Ubuntu, with Linux 6.18-intel"
	info := &BaselineInfo{Bootloader: "grub2", Kernels: []string{"6.8.0-40-generic"}}
	if err := RegenerateGrub(grubTemplateFull("", wantDefault), info, "/mnt/root"); err != nil {
		t.Fatalf("RegenerateGrub: %v", err)
	}
	if !strings.Contains(*written, "GRUB_DEFAULT=\""+wantDefault+"\"") {
		t.Errorf("GRUB_DEFAULT must be pinned in grub defaults, got %q", *written)
	}
	// The cmdline was not requested, so its line must be left untouched.
	if !strings.Contains(*written, "GRUB_CMDLINE_LINUX=\"quiet\"") {
		t.Errorf("GRUB_CMDLINE_LINUX must be untouched when only grubDefault is set, got %q", *written)
	}
	if *gotCmd == "" {
		t.Error("a set grubDefault must trigger GRUB regeneration even with no new kernel")
	}
}

func TestRegenerateGrub_AppliesBothOverridesInOnePass(t *testing.T) {
	stubGrubRegen(t)
	stubDetectKernels(t, nil)
	content := "GRUB_DEFAULT=0\nGRUB_CMDLINE_LINUX=\"old\"\n"
	written := stubGrubDefaults(t, &content)

	info := &BaselineInfo{Bootloader: "grub2"}
	tmpl := grubTemplateFull("console=ttyS0", "Advanced options for Ubuntu>Ubuntu, with Linux 6.18-intel")
	if err := RegenerateGrub(tmpl, info, "/mnt/root"); err != nil {
		t.Fatalf("RegenerateGrub: %v", err)
	}
	if !strings.Contains(*written, "GRUB_CMDLINE_LINUX=\"console=ttyS0\"") {
		t.Errorf("cmdline override missing from single write, got %q", *written)
	}
	if !strings.Contains(*written, "GRUB_DEFAULT=\"Advanced options for Ubuntu>Ubuntu, with Linux 6.18-intel\"") {
		t.Errorf("grubDefault override missing from single write, got %q", *written)
	}
}

// TestRegenerateGrub_DefaultsWriteUsesConfinedPath asserts the defaults edit acts
// on the path handed back by the confinement resolver (resolveInRoot), not a bare
// filepath.Join(rootMount, ...). A symlink at etc/default/grub could otherwise
// redirect the sudo-backed copy onto a host file; resolving first neutralizes that.
func TestRegenerateGrub_DefaultsWriteUsesConfinedPath(t *testing.T) {
	stubGrubRegen(t)
	stubDetectKernels(t, nil)

	origResolve, origRead, origWrite := grubDefaultsResolveFn, grubDefaultReadFn, grubDefaultWriteFn
	t.Cleanup(func() {
		grubDefaultsResolveFn, grubDefaultReadFn, grubDefaultWriteFn = origResolve, origRead, origWrite
	})
	// The resolver returns a sentinel host path distinct from a naive join, standing
	// in for a symlink target resolved inside the baseline.
	const resolved = "/confined/etc/default/grub"
	var gotResolveRoot, gotResolveRel, gotReadPath, gotWritePath string
	grubDefaultsResolveFn = func(rootMount, rootPath string) (string, error) {
		gotResolveRoot, gotResolveRel = rootMount, rootPath
		return resolved, nil
	}
	grubDefaultReadFn = func(p string) ([]byte, error) {
		gotReadPath = p
		return []byte("GRUB_CMDLINE_LINUX=\"old\"\n"), nil
	}
	grubDefaultWriteFn = func(p string, _ []byte) error { gotWritePath = p; return nil }

	info := &BaselineInfo{Bootloader: "grub2"}
	if err := RegenerateGrub(grubTemplate("console=ttyS0"), info, "/mnt/root"); err != nil {
		t.Fatalf("RegenerateGrub: %v", err)
	}
	if gotResolveRoot != "/mnt/root" || gotResolveRel != "/"+grubDefaultsRelPath {
		t.Errorf("resolver called with (%q, %q), want (%q, %q)",
			gotResolveRoot, gotResolveRel, "/mnt/root", "/"+grubDefaultsRelPath)
	}
	if gotReadPath != resolved {
		t.Errorf("read used %q, want the resolved path %q", gotReadPath, resolved)
	}
	if gotWritePath != resolved {
		t.Errorf("write used %q, want the resolved path %q", gotWritePath, resolved)
	}
}

// TestRegenerateGrub_DefaultsResolveFailureErrors asserts a confinement-resolution
// failure (e.g. the defaults file symlink-escapes the baseline or is absent) fails
// the build rather than falling back to an unconfined path.
func TestRegenerateGrub_DefaultsResolveFailureErrors(t *testing.T) {
	stubGrubRegen(t)
	stubDetectKernels(t, nil)

	origResolve, origWrite := grubDefaultsResolveFn, grubDefaultWriteFn
	t.Cleanup(func() { grubDefaultsResolveFn, grubDefaultWriteFn = origResolve, origWrite })
	grubDefaultsResolveFn = func(string, string) (string, error) { return "", os.ErrNotExist }
	wrote := false
	grubDefaultWriteFn = func(string, []byte) error { wrote = true; return nil }

	info := &BaselineInfo{Bootloader: "grub2"}
	err := RegenerateGrub(grubTemplate("quiet"), info, "/mnt/root")
	if err == nil || !strings.Contains(err.Error(), grubDefaultsRelPath) {
		t.Fatalf("a failed confinement resolve must error clearly, got %v", err)
	}
	if wrote {
		t.Error("no write must occur when the defaults path cannot be resolved within the baseline")
	}
}

func TestRegenerateGrub_CmdlineFileAbsentErrors(t *testing.T) {
	stubGrubRegen(t)
	stubDetectKernels(t, nil)
	stubGrubDefaults(t, nil) // read fails with os.ErrNotExist

	info := &BaselineInfo{Bootloader: "grub2"}
	err := RegenerateGrub(grubTemplate("quiet"), info, "/mnt/root")
	if err == nil || !strings.Contains(err.Error(), grubDefaultsRelPath) {
		t.Fatalf("a missing grub defaults file must error clearly, got %v", err)
	}
}

func TestRegenerateGrub_GeneratorFailureSurfaces(t *testing.T) {
	origExec, origCmdExist := grubRegenExec, commandExistsFn
	defer func() { grubRegenExec, commandExistsFn = origExec, origCmdExist }()
	commandExistsFn = func(string, string) (bool, error) { return true, nil }
	mounts, umounts := stubSysfsMounts(t)
	stubDetectKernels(t, []string{"6.8.0-50-generic"})
	grubRegenExec = func(string, string) (string, error) { return "grub error detail", errors.New("exit status 1") }

	info := &BaselineInfo{Bootloader: "grub2", Kernels: []string{"6.8.0-40-generic"}}
	err := RegenerateGrub(grubTemplate(""), info, "/mnt/root")
	if err == nil || !strings.Contains(err.Error(), "grub") {
		t.Fatalf("a present-but-failing generator must surface, got %v", err)
	}
	if !strings.Contains(err.Error(), "grub error detail") {
		t.Errorf("captured generator output must be appended to the error, got %v", err)
	}
	if *mounts != 1 || *umounts != 1 {
		t.Errorf("sysfs mount/umount = %d/%d, want 1/1", *mounts, *umounts)
	}
}

func TestRegenerateGrub_NoGeneratorWithWorkToDoErrors(t *testing.T) {
	origExec, origCmdExist := grubRegenExec, commandExistsFn
	defer func() { grubRegenExec, commandExistsFn = origExec, origCmdExist }()
	commandExistsFn = func(string, string) (bool, error) { return false, nil } // no generator on PATH
	called := false
	grubRegenExec = func(string, string) (string, error) { called = true; return "", nil }
	stubDetectKernels(t, []string{"6.8.0-40-generic", "6.8.0-50-generic"}) // a new kernel => work to do

	info := &BaselineInfo{Bootloader: "grub2", Kernels: []string{"6.8.0-40-generic"}}
	err := RegenerateGrub(grubTemplate(""), info, "/mnt/root")
	if err == nil || !strings.Contains(err.Error(), "no GRUB config generator") {
		t.Fatalf("a missing generator with work to do must be a hard error, got %v", err)
	}
	if called {
		t.Error("the generator must not run when none is present")
	}
}

func TestRegenerateGrub_NilGuards(t *testing.T) {
	info := &BaselineInfo{Bootloader: "grub2"}
	if err := RegenerateGrub(nil, info, "/mnt/root"); err == nil {
		t.Error("expected error for nil template")
	}
	if err := RegenerateGrub(grubTemplate(""), nil, "/mnt/root"); err == nil {
		t.Error("expected error for nil info")
	}
	if err := RegenerateGrub(grubTemplate(""), info, ""); err == nil {
		t.Error("expected error for empty root mount")
	}
}

func TestGrubRegenCommand_ToolSelection(t *testing.T) {
	orig := commandExistsFn
	defer func() { commandExistsFn = orig }()

	// Only the named tool is present; assert the selected command and tool.
	cases := []struct {
		present  string
		wantTool string
		wantCmd  string
	}{
		{"update-grub", "update-grub", "update-grub"},
		{"grub2-mkconfig", "grub2-mkconfig", "grub2-mkconfig -o /boot/grub2/grub.cfg"},
		{"grub-mkconfig", "grub-mkconfig", "grub-mkconfig -o /boot/grub/grub.cfg"},
	}
	for _, tc := range cases {
		t.Run(tc.present, func(t *testing.T) {
			commandExistsFn = func(tool, _ string) (bool, error) { return tool == tc.present, nil }
			cmd, tool, present, err := grubRegenCommand("/mnt/root")
			if err != nil || !present {
				t.Fatalf("present=%v err=%v, want present with no error", present, err)
			}
			if tool != tc.wantTool || cmd != tc.wantCmd {
				t.Errorf("selected tool=%q cmd=%q, want tool=%q cmd=%q", tool, cmd, tc.wantTool, tc.wantCmd)
			}
		})
	}

	t.Run("none present", func(t *testing.T) {
		commandExistsFn = func(string, string) (bool, error) { return false, nil }
		_, _, present, err := grubRegenCommand("/mnt/root")
		if err != nil || present {
			t.Errorf("no generator: present=%v err=%v, want present=false err=nil", present, err)
		}
	})

	t.Run("probe error surfaces", func(t *testing.T) {
		commandExistsFn = func(string, string) (bool, error) { return false, errors.New("probe boom") }
		if _, _, _, err := grubRegenCommand("/mnt/root"); err == nil {
			t.Error("a probe error must surface")
		}
	})
}

// writeShim creates a fake shim binary on a temp ESP under root so
// secureBootBaseline detects a Secure Boot layout.
func writeShim(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "boot", "efi", "EFI", "ubuntu")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir esp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shimx64.efi"), []byte("shim"), 0o644); err != nil {
		t.Fatalf("write shim: %v", err)
	}
}

func TestSecureBootBaseline(t *testing.T) {
	noShim := t.TempDir()
	if secureBootBaseline(noShim) {
		t.Error("a root without a shim must not read as a Secure Boot baseline")
	}
	withShim := t.TempDir()
	writeShim(t, withShim)
	if !secureBootBaseline(withShim) {
		t.Error("a root with an ESP shim must read as a Secure Boot baseline")
	}
}

func TestNeedsSecureBootWarning(t *testing.T) {
	shimRoot := t.TempDir()
	writeShim(t, shimRoot)
	plainRoot := t.TempDir()

	signed := &config.ImageTemplate{
		SystemConfig: config.SystemConfig{
			Immutability: config.ImmutabilityConfig{
				Enabled:         true,
				SecureBootDBKey: "/k.key",
				SecureBootDBCrt: "/c.crt",
				SecureBootDBCer: "/c.cer",
			},
		},
	}
	unsigned := &config.ImageTemplate{}

	cases := []struct {
		name        string
		template    *config.ImageTemplate
		root        string
		kernelAdded bool
		want        bool
	}{
		{"sb baseline, kernel added, no signing material", unsigned, shimRoot, true, true},
		{"sb baseline, kernel added, has signing material", signed, shimRoot, true, false},
		{"sb baseline, no kernel added", unsigned, shimRoot, false, false},
		{"not sb baseline, kernel added", unsigned, plainRoot, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsSecureBootWarning(tc.template, tc.root, tc.kernelAdded); got != tc.want {
				t.Errorf("needsSecureBootWarning = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAddedKernels(t *testing.T) {
	cases := []struct {
		name          string
		before, after []string
		want          []string
	}{
		{"none added", []string{"6.8"}, []string{"6.8"}, nil},
		{"one added", []string{"6.8"}, []string{"6.8", "6.9"}, []string{"6.9"}},
		{"all new", nil, []string{"6.9"}, []string{"6.9"}},
		{"removed only", []string{"6.8", "6.9"}, []string{"6.8"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := addedKernels(tc.before, tc.after)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("addedKernels(%v, %v) = %v, want %v", tc.before, tc.after, got, tc.want)
			}
		})
	}
}
