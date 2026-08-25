package system

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// chownRecorder captures (path, uid, gid) tuples in place of a real Lchown so
// the traversal logic can be exercised without CAP_CHOWN. It installs itself as
// the package-level lchown and restores the original on cleanup.
type chownRecorder struct {
	calls []string
}

func installChownRecorder(t *testing.T) *chownRecorder {
	t.Helper()
	rec := &chownRecorder{}
	orig := lchown
	lchown = func(path string, uid, gid int) error {
		rec.calls = append(rec.calls, path)
		return nil
	}
	t.Cleanup(func() { lchown = orig })
	return rec
}

// setSudoEnv sets SUDO_UID/SUDO_GID for the duration of the test.
func setSudoEnv(t *testing.T, uid, gid string) {
	t.Helper()
	t.Setenv("SUDO_UID", uid)
	t.Setenv("SUDO_GID", gid)
}

func TestChownArtifacts_ChownsPathNodesAndArtifactTreeOnly(t *testing.T) {
	setSudoEnv(t, "1000", "1000")
	rec := installChownRecorder(t)

	workDir := t.TempDir()
	provider := "ubuntu-ubuntu24-x86_64"
	imageBuildDir := filepath.Join(workDir, provider, "imagebuild", "cfg")

	// Build a realistic layout: the artifact leaf with an image + SBOM, plus a
	// sibling chroot tree that must NOT be chowned.
	if err := os.MkdirAll(imageBuildDir, 0o700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(imageBuildDir, "image.raw")
	sbom := filepath.Join(imageBuildDir, "image.spdx.json")
	for _, f := range []string{image, sbom} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	chrootTree := filepath.Join(workDir, provider, "chrootenv", "etc")
	if err := os.MkdirAll(chrootTree, 0o700); err != nil {
		t.Fatal(err)
	}
	chrootFile := filepath.Join(chrootTree, "shadow")
	if err := os.WriteFile(chrootFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ChownArtifactsToSudoUser(workDir, imageBuildDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := make(map[string]bool)
	for _, c := range rec.calls {
		got[c] = true
	}

	// Path nodes chowned (non-recursively) + artifact tree chowned (recursively).
	wantChowned := []string{
		workDir,
		filepath.Join(workDir, provider),
		filepath.Join(workDir, provider, "imagebuild"),
		imageBuildDir,
		image,
		sbom,
	}
	for _, w := range wantChowned {
		if !got[w] {
			t.Errorf("expected %q to be chowned, but it was not (calls: %v)", w, sortedKeys(got))
		}
	}

	// The chroot tree and its files must be left untouched.
	for _, forbidden := range []string{
		filepath.Join(workDir, provider, "chrootenv"),
		chrootTree,
		chrootFile,
	} {
		if got[forbidden] {
			t.Errorf("chroot path %q must not be chowned, but it was", forbidden)
		}
	}
}

func TestChownArtifacts_RefusesDangerousWorkDir(t *testing.T) {
	setSudoEnv(t, "1000", "1000")

	for name, workDir := range map[string]string{
		"filesystem root":     string(os.PathSeparator),
		"top-level directory": "/var",
	} {
		t.Run(name, func(t *testing.T) {
			rec := installChownRecorder(t)
			imageBuildDir := filepath.Join(workDir, "p", "imagebuild", "cfg")
			if err := ChownArtifactsToSudoUser(workDir, imageBuildDir); err == nil {
				t.Errorf("expected workDir %q (%s) to be refused, got nil error", workDir, name)
			}
			if len(rec.calls) != 0 {
				t.Errorf("expected no chown calls when refusing %q, got %v", workDir, rec.calls)
			}
		})
	}
}

// TestChownArtifacts_SymlinkComponentCannotRedirect verifies that a symlinked
// intermediate path component cannot make the recursive walk escape workDir: the
// image build dir resolves through a symlink to a sibling outside the workspace,
// which must be refused after symlink resolution rather than chowned.
func TestChownArtifacts_SymlinkComponentCannotRedirect(t *testing.T) {
	setSudoEnv(t, "1000", "1000")
	rec := installChownRecorder(t)

	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A real directory OUTSIDE workDir, holding a file that must never be chowned.
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	// Materialise the artifact path under the outside dir so EvalSymlinks resolves
	// the redirected leaf to a real location outside workDir (an unresolvable path
	// would keep its lexical form and pass containment for a different reason).
	if err := os.MkdirAll(filepath.Join(outside, "imagebuild", "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "imagebuild", "cfg", "secret")
	if err := os.WriteFile(secret, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A component under workDir that is actually a symlink to the outside dir.
	link := filepath.Join(workDir, "provider")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	imageBuildDir := filepath.Join(link, "imagebuild", "cfg")
	if err := ChownArtifactsToSudoUser(workDir, imageBuildDir); err == nil {
		t.Fatal("expected symlink-redirected imageBuildDir to be refused, got nil")
	}
	for _, c := range rec.calls {
		if strings.HasPrefix(c, outside) {
			t.Errorf("symlink redirect chowned outside workDir: %q", c)
		}
	}
}

func TestChownArtifacts_NoopWithoutSudoEnv(t *testing.T) {
	// Explicitly clear both vars so a sudo-launched test runner does not leak them.
	t.Setenv("SUDO_UID", "")
	t.Setenv("SUDO_GID", "")
	rec := installChownRecorder(t)

	workDir := t.TempDir()
	imageBuildDir := filepath.Join(workDir, "p", "imagebuild", "cfg")
	if err := os.MkdirAll(imageBuildDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := ChownArtifactsToSudoUser(workDir, imageBuildDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected no chown calls without SUDO_UID/SUDO_GID, got %v", rec.calls)
	}
}

func TestChownArtifacts_NoopWhenSudoUserIsRoot(t *testing.T) {
	setSudoEnv(t, "0", "0")
	rec := installChownRecorder(t)

	workDir := t.TempDir()
	imageBuildDir := filepath.Join(workDir, "p", "imagebuild", "cfg")
	if err := os.MkdirAll(imageBuildDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := ChownArtifactsToSudoUser(workDir, imageBuildDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected no chown calls when SUDO_UID is 0, got %v", rec.calls)
	}
}

func TestChownArtifacts_RejectsImageDirEscapingWorkDir(t *testing.T) {
	setSudoEnv(t, "1000", "1000")
	rec := installChownRecorder(t)

	workDir := t.TempDir()
	// A sibling directory outside workDir.
	outside := filepath.Join(filepath.Dir(workDir), "elsewhere", "imagebuild", "cfg")

	err := ChownArtifactsToSudoUser(workDir, outside)
	if err == nil {
		t.Fatal("expected an error when imageBuildDir escapes workDir, got nil")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("expected an escape error, got: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected no chown calls on escape rejection, got %v", rec.calls)
	}
}

func TestChownArtifacts_RejectsRelativePaths(t *testing.T) {
	setSudoEnv(t, "1000", "1000")
	installChownRecorder(t)

	if err := ChownArtifactsToSudoUser("relative/work", "relative/work/p/imagebuild/cfg"); err == nil {
		t.Fatal("expected an error for relative paths, got nil")
	}
}

func TestChownArtifacts_LeafEqualsWorkDir(t *testing.T) {
	setSudoEnv(t, "1000", "1000")
	rec := installChownRecorder(t)

	workDir := t.TempDir()
	f := filepath.Join(workDir, "image.raw")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Degenerate but valid: imageBuildDir == workDir. The recursive walk must
	// still cover the directory and its contents, with no duplicate/erroneous
	// node chown above it.
	if err := ChownArtifactsToSudoUser(workDir, workDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := make(map[string]bool)
	for _, c := range rec.calls {
		got[c] = true
	}
	if !got[workDir] || !got[f] {
		t.Errorf("expected workDir and its file to be chowned, got %v", sortedKeys(got))
	}
}

// TestChownArtifacts_RealChownAsRoot exercises the genuine os.Lchown path (no
// recorder) and asserts ownership actually lands on the invoking user. It needs
// real root to chown to another uid, so it is skipped otherwise — mirroring the
// other Geteuid()==0-gated integration tests in this repo.
func TestChownArtifacts_RealChownAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to chown files to another uid")
	}
	// Chown back to whoever launched sudo; fall back to a common unprivileged uid.
	uid, gid := 1000, 1000
	setSudoEnv(t, "1000", "1000")

	workDir := t.TempDir()
	imageBuildDir := filepath.Join(workDir, "p", "imagebuild", "cfg")
	if err := os.MkdirAll(imageBuildDir, 0o700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(imageBuildDir, "image.raw")
	if err := os.WriteFile(image, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ChownArtifactsToSudoUser(workDir, imageBuildDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range []string{workDir, filepath.Join(workDir, "p"), imageBuildDir, image} {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatalf("unexpected stat type for %s", p)
		}
		if int(st.Uid) != uid || int(st.Gid) != gid {
			t.Errorf("%s owned by %d:%d, want %d:%d", p, st.Uid, st.Gid, uid, gid)
		}
	}
}

func TestChownDirTree_ChownsEverythingRecursively(t *testing.T) {
	setSudoEnv(t, "1000", "1000")
	rec := installChownRecorder(t)

	// Nest the cache under a parent so it is not itself a top-level directory
	// (which the safety guard would reject).
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	pkgs := filepath.Join(cache, "pkgCache", "ubuntu-ubuntu24-x86_64")
	if err := os.MkdirAll(pkgs, 0o700); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(pkgs, "foo.deb")
	if err := os.WriteFile(pkg, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ChownDirTreeToSudoUser(cache); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := make(map[string]bool)
	for _, c := range rec.calls {
		got[c] = true
	}
	for _, want := range []string{cache, filepath.Join(cache, "pkgCache"), pkgs, pkg} {
		if !got[want] {
			t.Errorf("expected %q to be chowned, got %v", want, sortedKeys(got))
		}
	}
}

func TestChownDirTree_NoopWithoutSudoEnv(t *testing.T) {
	t.Setenv("SUDO_UID", "")
	t.Setenv("SUDO_GID", "")
	rec := installChownRecorder(t)

	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ChownDirTreeToSudoUser(cache); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected no chown calls without sudo env, got %v", rec.calls)
	}
}

func TestChownDirTree_RefusesDangerousRoots(t *testing.T) {
	setSudoEnv(t, "1000", "1000")

	cases := map[string]string{
		"filesystem root":      string(os.PathSeparator),
		"system temp dir":      filepath.Clean(os.TempDir()),
		"top-level directory":  "/var",
		"shared var tmp":       "/var/tmp",
		"shared var tmp child": "/var/tmp/ict-temp",
		"shared var cache":     "/var/cache",
		"shared var lib":       "/var/lib",
		"relative path":        "cache",
	}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			rec := installChownRecorder(t)
			if err := ChownDirTreeToSudoUser(dir); err == nil {
				t.Errorf("expected %q (%s) to be refused, got nil error", dir, name)
			}
			if len(rec.calls) != 0 {
				t.Errorf("expected no chown calls when refusing %q, got %v", dir, rec.calls)
			}
		})
	}
}

func TestChownDirTree_RefusesResolvedSystemTempAlias(t *testing.T) {
	setSudoEnv(t, "1000", "1000")
	rec := installChownRecorder(t)

	root := t.TempDir()
	realTemp := filepath.Join(root, "real-tmp")
	if err := os.MkdirAll(realTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	linkTemp := filepath.Join(root, "link-tmp")
	if err := os.Symlink(realTemp, linkTemp); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TMPDIR", linkTemp)

	if err := ChownDirTreeToSudoUser(realTemp); err == nil {
		t.Fatalf("expected resolved system temp alias %q to be refused, got nil error", realTemp)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected no chown calls when refusing %q, got %v", realTemp, rec.calls)
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
