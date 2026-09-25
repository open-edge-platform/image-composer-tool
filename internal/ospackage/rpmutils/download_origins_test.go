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
