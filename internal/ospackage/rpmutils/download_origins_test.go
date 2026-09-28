package rpmutils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchRepoOriginPicksLongestPrefix(t *testing.T) {
	repos := []repoOrigin{
		{baseURL: "http://example.com/"},
		{baseURL: "http://example.com/sub/"},
	}

	if got := matchRepoOrigin("http://example.com/sub/pkg.rpm", repos); got != 1 {
		t.Fatalf("expected longest-prefix match index 1, got %d", got)
	}
	if got := matchRepoOrigin("http://example.com/pkg.rpm", repos); got != 0 {
		t.Fatalf("expected match index 0, got %d", got)
	}
	if got := matchRepoOrigin("http://other.example/pkg.rpm", repos); got != -1 {
		t.Fatalf("expected no match (-1), got %d", got)
	}
}

func TestRepoRawKeysMergesPKeyAndPKeys(t *testing.T) {
	got := repoRawKeys("https://a/key.asc, https://b/key.asc", []string{"https://c/key.asc"})
	want := []string{"https://a/key.asc", "https://b/key.asc", "https://c/key.asc"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// TestValidateOriginsScopesTrustedYesPerRepo is a regression test for the
// review comment on the [trusted=yes] opt-out: a repo explicitly marked
// [trusted=yes] must have its own RPMs skipped, while an unsigned RPM that
// actually came from a *different*, signed repo must still fail
// verification -- even though [trusted=yes] appears somewhere in the overall
// configuration.
func TestValidateOriginsScopesTrustedYesPerRepo(t *testing.T) {
	keyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("dummy-gpg-key-content"))
	}))
	defer keyServer.Close()

	destDir := t.TempDir()

	trustedRPM := "trusted-1.0-1.x86_64.rpm"
	signedRepoRPM := "unsigned-from-signed-repo-1.0-1.x86_64.rpm"

	for _, name := range []string{trustedRPM, signedRepoRPM} {
		if err := os.WriteFile(filepath.Join(destDir, name), []byte("rpm bytes"), 0644); err != nil {
			t.Fatalf("failed to create RPM %s: %v", name, err)
		}
	}

	fileOrigins := map[string]string{
		trustedRPM:    "http://trusted.local/repo/" + trustedRPM,
		signedRepoRPM: "http://signed.local/repo/" + signedRepoRPM,
	}
	repos := []repoOrigin{
		{baseURL: "http://trusted.local/repo/", keys: []string{"[trusted=yes]"}},
		{baseURL: "http://signed.local/repo/", keys: []string{keyServer.URL}},
	}

	err := ValidateOrigins(destDir, fileOrigins, repos)
	if err == nil {
		t.Fatal("expected verification failure for the unsigned RPM from the signed repo")
	}
	if !strings.Contains(err.Error(), signedRepoRPM) {
		t.Errorf("expected failure to reference %s, got: %v", signedRepoRPM, err)
	}
	if strings.Contains(err.Error(), trustedRPM) {
		t.Errorf("the [trusted=yes] repo's own RPM should not have been verified, got: %v", err)
	}
}

// TestValidateOriginsUnknownOriginFallsBackToUnionCheck ensures an RPM whose
// source repo can't be determined is never silently exempted -- it must
// still be checked against the union of all configured keys.
func TestValidateOriginsUnknownOriginFallsBackToUnionCheck(t *testing.T) {
	destDir := t.TempDir()
	unknownRPM := "unknown-origin-1.0-1.x86_64.rpm"
	if err := os.WriteFile(filepath.Join(destDir, unknownRPM), []byte("rpm bytes"), 0644); err != nil {
		t.Fatalf("failed to create RPM: %v", err)
	}

	// fileOrigins has no entry for unknownRPM, and repos are all trusted --
	// but since the origin is unknown, the RPM must still be verified (and
	// fail, since it isn't signed) rather than being exempted.
	fileOrigins := map[string]string{
		"other.rpm": "http://trusted.local/repo/other.rpm",
	}
	repos := []repoOrigin{
		{baseURL: "http://trusted.local/repo/", keys: []string{"[trusted=yes]"}},
	}

	err := ValidateOrigins(destDir, fileOrigins, repos)
	if err == nil {
		t.Fatal("expected verification failure for an RPM with an unknown origin")
	}
}

func TestValidateOriginsAllTrustedSkipsVerification(t *testing.T) {
	destDir := t.TempDir()
	rpmName := "trusted-1.0-1.x86_64.rpm"
	if err := os.WriteFile(filepath.Join(destDir, rpmName), []byte("rpm bytes"), 0644); err != nil {
		t.Fatalf("failed to create RPM: %v", err)
	}

	fileOrigins := map[string]string{
		rpmName: "http://trusted.local/repo/" + rpmName,
	}
	repos := []repoOrigin{
		{baseURL: "http://trusted.local/repo/", keys: []string{"[trusted=yes]"}},
	}

	if err := ValidateOrigins(destDir, fileOrigins, repos); err != nil {
		t.Fatalf("expected [trusted=yes] repo's own RPM to skip verification, got: %v", err)
	}
}

// TestPurgeTrustedCachedRPMsForcesTrustedRedownload is a regression test for the
// cached-artifact bypass: FetchPackages skips existing files, so a stale cached
// RPM sharing a basename with a package now served by a [trusted=yes] repo would
// inherit the opt-out and skip verification (CWE-347). purgeTrustedCachedRPMs
// must delete the cached file for the trusted package (forcing a fresh download)
// while leaving cached files for signed or unrecognized origins untouched.
func TestPurgeTrustedCachedRPMsForcesTrustedRedownload(t *testing.T) {
	destDir := t.TempDir()

	trusted := "trusted-1.0-1.x86_64.rpm"
	signed := "signed-1.0-1.x86_64.rpm"
	unknown := "unknown-1.0-1.x86_64.rpm"
	for _, name := range []string{trusted, signed, unknown} {
		if err := os.WriteFile(filepath.Join(destDir, name), []byte("stale bytes"), 0644); err != nil {
			t.Fatalf("seeding cached %s: %v", name, err)
		}
	}

	urls := []string{
		"http://trusted.local/repo/" + trusted,
		"http://signed.local/repo/" + signed,
		"http://elsewhere.local/repo/" + unknown,
	}
	filenames := []string{trusted, signed, unknown}
	repos := []repoOrigin{
		{baseURL: "http://trusted.local/repo/", keys: []string{"[trusted=yes]"}},
		{baseURL: "http://signed.local/repo/", keys: []string{"https://signed.local/key.asc"}},
	}

	if err := purgeTrustedCachedRPMs(destDir, urls, filenames, repos); err != nil {
		t.Fatalf("purgeTrustedCachedRPMs returned an unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, trusted)); !os.IsNotExist(err) {
		t.Errorf("expected cached %s to be removed so it is re-downloaded from its [trusted=yes] source, stat err=%v", trusted, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, signed)); err != nil {
		t.Errorf("cached RPM from a signed repo must be kept (it is verified), got stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, unknown)); err != nil {
		t.Errorf("cached RPM with an unrecognized origin must be kept (it falls back to verification), got stat err=%v", err)
	}
}

// TestPurgeTrustedCachedRPMsFailsWhenCacheCannotBeRemoved is a regression test
// for the review comment that a removal failure must be fatal: if the stale
// cached file cannot be deleted, FetchPackages would skip the download and the
// old bytes would inherit the [trusted=yes] opt-out, so the helper must return
// an error rather than continue.
func TestPurgeTrustedCachedRPMsFailsWhenCacheCannotBeRemoved(t *testing.T) {
	destDir := t.TempDir()

	trusted := "trusted-1.0-1.x86_64.rpm"
	// A non-empty directory at the cached path makes os.Remove fail with a
	// non-IsNotExist error, standing in for any undeletable cache entry.
	if err := os.MkdirAll(filepath.Join(destDir, trusted, "child"), 0755); err != nil {
		t.Fatalf("seeding undeletable cache entry: %v", err)
	}

	urls := []string{"http://trusted.local/repo/" + trusted}
	filenames := []string{trusted}
	repos := []repoOrigin{
		{baseURL: "http://trusted.local/repo/", keys: []string{"[trusted=yes]"}},
	}

	if err := purgeTrustedCachedRPMs(destDir, urls, filenames, repos); err == nil {
		t.Fatal("expected an error when a trusted repo's cached file cannot be removed")
	}
}
