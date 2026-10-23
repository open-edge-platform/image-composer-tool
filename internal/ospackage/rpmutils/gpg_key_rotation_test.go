package rpmutils

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
)

// newTestGPGPublicKey generates an RSA key: the key data is also checked by the
// system gpg, whose support for newer algorithms varies between environments.
func newTestGPGPublicKey(t *testing.T, name string) string {
	t.Helper()

	entity, err := openpgp.NewEntity(name, "", name+"@example.com", &packet.Config{Algorithm: packet.PubKeyAlgoRSA, RSABits: 2048})
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("failed to start armor encoding: %v", err)
	}
	if err := entity.Serialize(w); err != nil {
		t.Fatalf("failed to serialize test key: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to finish armor encoding: %v", err)
	}
	return buf.String()
}

// requireGPGKeyCacheUnder fails the test unless the persistent cache entry for
// keyURL lives under the test's own cache directory. These tests count the
// requests a key server receives, which only means anything if no other test can
// have cached the key first.
func requireGPGKeyCacheUnder(t *testing.T, keyURL, cacheRoot string) {
	t.Helper()
	cachePath, err := config.GPGKeyCacheFilePath(keyURL)
	if err != nil {
		t.Fatalf("GPGKeyCacheFilePath() error = %v", err)
	}
	if !strings.HasPrefix(cachePath, cacheRoot+string(filepath.Separator)) {
		t.Fatalf("GPG key cache %s is outside the test's cache directory %s", cachePath, cacheRoot)
	}
}

// stubVerifyAcceptingKey makes the RPM check pass only when one of the key
// files it is handed holds exactly accepted.
func stubVerifyAcceptingKey(t *testing.T, accepted string) {
	t.Helper()

	orig := verifyAllRPMs
	t.Cleanup(func() { verifyAllRPMs = orig })
	verifyAllRPMs = func(paths, keyPaths []string, workers int) []Result {
		keyOK := false
		for _, keyPath := range keyPaths {
			if data, err := os.ReadFile(keyPath); err == nil && string(data) == accepted {
				keyOK = true
			}
		}
		results := make([]Result, len(paths))
		for i, p := range paths {
			results[i] = Result{Path: p, OK: keyOK}
			if !keyOK {
				results[i].Error = os.ErrInvalid
			}
		}
		return results
	}
}

// setupRotatedKeyRepo caches oldKey for a key URL whose server now serves
// newKey (or an HTTP error when newKey is nil), and returns a directory holding
// one RPM together with a counter of the requests the key server receives.
func setupRotatedKeyRepo(t *testing.T, oldKey string, newKey *string) (destDir string, hits *int32) {
	t.Helper()
	cacheRoot := setRPMMetadataTestCache(t)

	var served atomic.Pointer[string]
	served.Store(&oldKey)
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		key := served.Load()
		if key == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(*key))
	}))
	t.Cleanup(server.Close)
	keyURL := server.URL + "/RPM-GPG-KEY.asc"
	requireGPGKeyCacheUnder(t, keyURL, cacheRoot)

	if _, err := config.GetCachedOrDownloadGPGKey(keyURL); err != nil {
		t.Fatalf("failed to prime the GPG key cache: %v", err)
	}
	served.Store(newKey)
	atomic.StoreInt32(&requests, 0)

	origRepoCfg, origUserRepo := RepoCfg, UserRepo
	t.Cleanup(func() { RepoCfg, UserRepo = origRepoCfg, origUserRepo })
	RepoCfg = RepoConfig{URL: server.URL, GPGKey: keyURL}
	UserRepo = nil

	destDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(destDir, "pkg-1.0-1.x86_64.rpm"), []byte("rpm"), 0644); err != nil {
		t.Fatalf("failed to create test rpm: %v", err)
	}
	return destDir, &requests
}

func TestValidateRefreshesRotatedGPGKey(t *testing.T) {
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifyAcceptingKey(t, newKey)
	destDir, hits := setupRotatedKeyRepo(t, oldKey, &newKey)

	if err := Validate(destDir); err != nil {
		t.Fatalf("Validate() should refresh the rotated key and succeed: %v", err)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("key requests = %d, want exactly 1 refresh", n)
	}

	// The refreshed key is now cached, so the next run needs no network.
	if err := Validate(destDir); err != nil {
		t.Fatalf("second Validate() error = %v", err)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("key requests after second run = %d, want still 1", n)
	}
}

func TestValidateCachedGPGKeyHitMakesNoRequest(t *testing.T) {
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifyAcceptingKey(t, oldKey)
	destDir, hits := setupRotatedKeyRepo(t, oldKey, &newKey)

	if err := Validate(destDir); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if n := atomic.LoadInt32(hits); n != 0 {
		t.Fatalf("key requests = %d, want 0 when the cached key verifies", n)
	}
}

func TestValidateReportsVerificationFailureWhenKeyCannotBeRefreshed(t *testing.T) {
	oldKey := newTestGPGPublicKey(t, "old")
	stubVerifyAcceptingKey(t, "a key that is never served")
	destDir, hits := setupRotatedKeyRepo(t, oldKey, nil)

	err := Validate(destDir)
	if err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("Validate() error = %v, want the original verification failure", err)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("key requests = %d, want a single refresh attempt", n)
	}
}

func TestVerifyGroupsRefreshesRotatedGPGKey(t *testing.T) {
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifyAcceptingKey(t, newKey)
	destDir, hits := setupRotatedKeyRepo(t, oldKey, &newKey)

	groups := map[int]*verifyGroup{
		0: {keys: []string{RepoCfg.GPGKey}, files: []string{filepath.Join(destDir, "pkg-1.0-1.x86_64.rpm")}},
	}
	if err := verifyGroups(groups); err != nil {
		t.Fatalf("verifyGroups() should refresh the rotated key and succeed: %v", err)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("key requests = %d, want exactly 1 refresh", n)
	}
}

// cachedGPGKey returns the persistent cache entry for keyURL.
func cachedGPGKey(t *testing.T, keyURL string) string {
	t.Helper()
	cachePath, err := config.GPGKeyCacheFilePath(keyURL)
	if err != nil {
		t.Fatalf("GPGKeyCacheFilePath() error = %v", err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("failed to read the cached GPG key: %v", err)
	}
	return string(data)
}

// A refreshed key is published to the cache only once it has verified the RPMs.
// If verification still fails, for example because an RPM is corrupt, the cached
// key stays, since it may still verify RPMs downloaded earlier.
func TestValidateKeepsCachedGPGKeyWhenRetryAlsoFails(t *testing.T) {
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifyAcceptingKey(t, "a key that signs nothing here")
	destDir, hits := setupRotatedKeyRepo(t, oldKey, &newKey)

	err := Validate(destDir)
	if err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("Validate() error = %v, want a verification failure", err)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("key requests = %d, want a single refresh", n)
	}
	if got := cachedGPGKey(t, RepoCfg.GPGKey); got != oldKey {
		t.Fatalf("the cached key was replaced although the refreshed key verified nothing")
	}
}

// stubVerifySignedBy makes the RPM check pass for a file only when one of the
// key files it is handed holds exactly the key that signed that file, given by
// signers (file name to key); files not listed in signers never verify.
func stubVerifySignedBy(t *testing.T, signers map[string]string) {
	t.Helper()

	orig := verifyAllRPMs
	t.Cleanup(func() { verifyAllRPMs = orig })
	verifyAllRPMs = func(paths, keyPaths []string, workers int) []Result {
		present := map[string]bool{}
		for _, keyPath := range keyPaths {
			if data, err := os.ReadFile(keyPath); err == nil {
				present[string(data)] = true
			}
		}
		results := make([]Result, len(paths))
		for i, p := range paths {
			ok := present[signers[filepath.Base(p)]]
			results[i] = Result{Path: p, OK: ok}
			if !ok {
				results[i].Error = os.ErrInvalid
			}
		}
		return results
	}
}

// twoKeyRepo is a repository with two GPG key URLs, whose cached keys are oldA
// and oldB while the key server now serves newA and newB (nil: unreachable).
type twoKeyRepo struct {
	urlA, urlB string
	destDir    string
	requests   [2]*int32
}

func setupTwoKeyRepo(t *testing.T, oldA string, newA *string, oldB string, newB *string, rpmNames ...string) twoKeyRepo {
	t.Helper()
	cacheRoot := setRPMMetadataTestCache(t)

	var servedA, servedB atomic.Pointer[string]
	servedA.Store(&oldA)
	servedB.Store(&oldB)
	repo := twoKeyRepo{requests: [2]*int32{new(int32), new(int32)}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served, requests := &servedA, repo.requests[0]
		if r.URL.Path == "/b.asc" {
			served, requests = &servedB, repo.requests[1]
		}
		atomic.AddInt32(requests, 1)
		key := served.Load()
		if key == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(*key))
	}))
	t.Cleanup(server.Close)
	repo.urlA, repo.urlB = server.URL+"/a.asc", server.URL+"/b.asc"
	requireGPGKeyCacheUnder(t, repo.urlA, cacheRoot)
	requireGPGKeyCacheUnder(t, repo.urlB, cacheRoot)
	for _, keyURL := range []string{repo.urlA, repo.urlB} {
		if _, err := config.GetCachedOrDownloadGPGKey(keyURL); err != nil {
			t.Fatalf("failed to prime the GPG key cache: %v", err)
		}
	}
	servedA.Store(newA)
	servedB.Store(newB)
	atomic.StoreInt32(repo.requests[0], 0)
	atomic.StoreInt32(repo.requests[1], 0)

	origRepoCfg, origUserRepo := RepoCfg, UserRepo
	t.Cleanup(func() { RepoCfg, UserRepo = origRepoCfg, origUserRepo })
	RepoCfg = RepoConfig{URL: server.URL, GPGKey: repo.urlA + "," + repo.urlB}
	UserRepo = nil

	repo.destDir = t.TempDir()
	for _, name := range rpmNames {
		if err := os.WriteFile(filepath.Join(repo.destDir, name), []byte("rpm"), 0644); err != nil {
			t.Fatalf("failed to create test rpm: %v", err)
		}
	}
	return repo
}

// With several key URLs, a rotated key must still be refreshed when another
// key's URL cannot be reached: the retry combines the new key with the other
// key's cached one, and the key that could not be refreshed keeps its entry.
func TestValidateRefreshesRotatedGPGKeyWhenAnotherKeyCannotBeRefreshed(t *testing.T) {
	oldA, newA, keyB := newTestGPGPublicKey(t, "old-a"), newTestGPGPublicKey(t, "new-a"), newTestGPGPublicKey(t, "b")
	stubVerifySignedBy(t, map[string]string{"pkg-a-1.0-1.x86_64.rpm": newA, "pkg-b-1.0-1.x86_64.rpm": keyB})
	repo := setupTwoKeyRepo(t, oldA, &newA, keyB, nil, "pkg-a-1.0-1.x86_64.rpm", "pkg-b-1.0-1.x86_64.rpm")

	if err := Validate(repo.destDir); err != nil {
		t.Fatalf("Validate() should verify with the new key and the cached one: %v", err)
	}
	if a, b := atomic.LoadInt32(repo.requests[0]), atomic.LoadInt32(repo.requests[1]); a != 1 || b != 1 {
		t.Fatalf("key requests = (a:%d, b:%d), want one refresh attempt each", a, b)
	}
	if got := cachedGPGKey(t, repo.urlA); got != newA {
		t.Fatalf("the refreshed key was not cached")
	}
	if got := cachedGPGKey(t, repo.urlB); got != keyB {
		t.Fatalf("the key that could not be refreshed must keep its cache entry")
	}
}

// When both keys rotate but an RPM that already verified is signed by a cached
// key, only the RPMs that failed are retried, and that cached key is kept so the
// RPM still verifies offline later.
func TestValidateKeepsCachedGPGKeyStillVerifyingCachedRPMs(t *testing.T) {
	oldA, newA := newTestGPGPublicKey(t, "old-a"), newTestGPGPublicKey(t, "new-a")
	oldB, newB := newTestGPGPublicKey(t, "old-b"), newTestGPGPublicKey(t, "new-b")
	stubVerifySignedBy(t, map[string]string{"pkg-a-1.0-1.x86_64.rpm": newA, "pkg-b-1.0-1.x86_64.rpm": oldB})
	repo := setupTwoKeyRepo(t, oldA, &newA, oldB, &newB, "pkg-a-1.0-1.x86_64.rpm", "pkg-b-1.0-1.x86_64.rpm")

	if err := Validate(repo.destDir); err != nil {
		t.Fatalf("Validate() should not fail an RPM that verified with its cached key: %v", err)
	}
	if got := cachedGPGKey(t, repo.urlA); got != newA {
		t.Fatalf("the key that verified the failed RPM was not cached")
	}
	if got := cachedGPGKey(t, repo.urlB); got != oldB {
		t.Fatalf("the cached key was replaced although a cached RPM still needs it")
	}
	if err := Validate(repo.destDir); err != nil {
		t.Fatalf("second Validate() error = %v", err)
	}
}

// A rotated key that verified none of the RPMs that needed a refresh has not
// been shown to work, so the cached key stays.
func TestValidateDoesNotCacheRefreshedGPGKeyThatVerifiesNothing(t *testing.T) {
	oldA, newA := newTestGPGPublicKey(t, "old-a"), newTestGPGPublicKey(t, "new-a")
	oldB, newB := newTestGPGPublicKey(t, "old-b"), newTestGPGPublicKey(t, "new-b")
	stubVerifySignedBy(t, map[string]string{"pkg-a-1.0-1.x86_64.rpm": newA})
	repo := setupTwoKeyRepo(t, oldA, &newA, oldB, &newB, "pkg-a-1.0-1.x86_64.rpm")

	if err := Validate(repo.destDir); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := cachedGPGKey(t, repo.urlA); got != newA {
		t.Fatalf("the key that verified the failed RPM was not cached")
	}
	if got := cachedGPGKey(t, repo.urlB); got != oldB {
		t.Fatalf("the cached key was replaced by a refreshed key that verified no RPM")
	}
}

// Two key URLs that cache the same key and both rotate to the same new key must
// not both be replaced when an RPM that already verified is signed by the old
// key: each replacement looks safe alone, since the old key is still cached at
// the other URL, but publishing both would remove it.
func TestValidateKeepsOldKeyAtOneURLWhenBothURLsRotate(t *testing.T) {
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifySignedBy(t, map[string]string{"pkg-old-1.0-1.x86_64.rpm": oldKey, "pkg-new-1.0-1.x86_64.rpm": newKey})
	repo := setupTwoKeyRepo(t, oldKey, &newKey, oldKey, &newKey, "pkg-old-1.0-1.x86_64.rpm", "pkg-new-1.0-1.x86_64.rpm")

	if err := Validate(repo.destDir); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := cachedGPGKey(t, repo.urlA); got != newKey {
		t.Fatalf("the first URL's key was not replaced by the key that verified the failed RPM")
	}
	if got := cachedGPGKey(t, repo.urlB); got != oldKey {
		t.Fatalf("the second URL's cached key was replaced, so the old key is no longer cached anywhere")
	}
	if err := Validate(repo.destDir); err != nil {
		t.Fatalf("second Validate() should verify both RPMs from the cache: %v", err)
	}
}

// Repositories that share a key URL are verified separately, but the cached key
// they share must not be replaced while one of them still needs it, whichever of
// them is verified first.
func TestVerifyGroupsKeepsSharedCachedKeyStillNeededByAnotherGroup(t *testing.T) {
	const oldRPM, newRPM = "pkg-old-1.0-1.x86_64.rpm", "pkg-new-1.0-1.x86_64.rpm"
	for name, order := range map[string][2]string{
		"group needing the new key first": {newRPM, oldRPM},
		"group needing the old key first": {oldRPM, newRPM},
	} {
		t.Run(name, func(t *testing.T) {
			oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
			stubVerifySignedBy(t, map[string]string{oldRPM: oldKey, newRPM: newKey})
			destDir, _ := setupRotatedKeyRepo(t, oldKey, &newKey)
			keyURL := RepoCfg.GPGKey
			for _, rpm := range order {
				if err := os.WriteFile(filepath.Join(destDir, rpm), []byte("rpm"), 0644); err != nil {
					t.Fatalf("failed to create test rpm: %v", err)
				}
			}

			groups := map[int]*verifyGroup{
				0: {keys: []string{keyURL}, files: []string{filepath.Join(destDir, order[0])}},
				1: {keys: []string{keyURL}, files: []string{filepath.Join(destDir, order[1])}},
			}
			if err := verifyGroups(groups); err != nil {
				t.Fatalf("verifyGroups() error = %v", err)
			}
			if got := cachedGPGKey(t, keyURL); got != oldKey {
				t.Fatalf("the shared cached key was replaced although a group still needs it")
			}
		})
	}
}

// Two builds that share the cache each see the old key cached at both URLs, and
// each refreshes a different URL to the same new key, so each replacement looks
// safe alone against the build's own view of the cache. The second publication
// must be checked against the cache the first one left, or the old key, which an
// RPM that already verified still needs, would be removed from both URLs.
func TestPublishKeyRefreshesChecksTheCacheLeftByAnotherBuild(t *testing.T) {
	const oldRPM, newRPM = "pkg-old-1.0-1.x86_64.rpm", "pkg-new-1.0-1.x86_64.rpm"
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifySignedBy(t, map[string]string{oldRPM: oldKey, newRPM: newKey})
	repo := setupTwoKeyRepo(t, oldKey, &newKey, oldKey, &newKey, oldRPM, newRPM)
	keyURLs := []string{repo.urlA, repo.urlB}
	snapshot := map[string][]byte{repo.urlA: []byte(oldKey), repo.urlB: []byte(oldKey)}
	refresh := func(keyURL string) *keyRefresh {
		return &keyRefresh{
			keyURLs:   keyURLs,
			cached:    snapshot,
			refreshed: map[string][]byte{keyURL: []byte(newKey)},
			failed:    []string{filepath.Join(repo.destDir, newRPM)},
			passed:    []string{filepath.Join(repo.destDir, oldRPM)},
		}
	}

	for _, keyURL := range []string{repo.urlA, repo.urlB} {
		if err := publishKeyRefreshes([]*keyRefresh{refresh(keyURL)}); err != nil {
			t.Fatalf("publishKeyRefreshes() error = %v", err)
		}
	}

	if got := cachedGPGKey(t, repo.urlA); got != newKey {
		t.Fatalf("the first build's refreshed key was not cached")
	}
	if got := cachedGPGKey(t, repo.urlB); got != oldKey {
		t.Fatalf("the second build removed the old key from the last URL caching it")
	}
}

// A build that downloaded the old key at one URL but could not write it to the
// cache holds it only in memory. Another build's cache has the old key at the
// other URL only, so a refresh of that URL must not count the unpersisted copy:
// it would let the old key be replaced although an RPM that already verified
// with it would then have no cached key left.
func TestPublishKeyRefreshesIgnoresAKeyThatWasNeverCached(t *testing.T) {
	const oldRPM, newRPM = "pkg-old-1.0-1.x86_64.rpm", "pkg-new-1.0-1.x86_64.rpm"
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifySignedBy(t, map[string]string{oldRPM: oldKey, newRPM: newKey})
	repo := setupTwoKeyRepo(t, oldKey, &newKey, oldKey, nil, oldRPM, newRPM)
	cachePathB, err := config.GPGKeyCacheFilePath(repo.urlB)
	if err != nil {
		t.Fatalf("GPGKeyCacheFilePath() error = %v", err)
	}
	if err := os.Remove(cachePathB); err != nil {
		t.Fatalf("failed to drop the cached key of the second URL: %v", err)
	}

	if err := publishKeyRefreshes([]*keyRefresh{{
		keyURLs:   []string{repo.urlA, repo.urlB},
		cached:    map[string][]byte{repo.urlA: []byte(oldKey), repo.urlB: []byte(oldKey)},
		refreshed: map[string][]byte{repo.urlA: []byte(newKey)},
		failed:    []string{filepath.Join(repo.destDir, newRPM)},
		passed:    []string{filepath.Join(repo.destDir, oldRPM)},
	}}); err != nil {
		t.Fatalf("publishKeyRefreshes() error = %v", err)
	}

	if got := cachedGPGKey(t, repo.urlA); got != oldKey {
		t.Fatalf("the old key was replaced although no other cache entry holds it")
	}
}

// A build cancelled while another holds the cache lock must stop waiting for it
// and report the cancellation: publishing or reading keys without the lock is
// what the lock exists to prevent, and silently skipping the cache would hide
// that the build was interrupted.
func TestKeyCacheLockWaitStopsWhenBuildIsCancelled(t *testing.T) {
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifyAcceptingKey(t, newKey)
	destDir, _ := setupRotatedKeyRepo(t, oldKey, &newKey)
	keyURL := RepoCfg.GPGKey
	refresh := &keyRefresh{
		keyURLs:   []string{keyURL},
		cached:    map[string][]byte{keyURL: []byte(oldKey)},
		refreshed: map[string][]byte{keyURL: []byte(newKey)},
		failed:    []string{filepath.Join(destDir, "pkg-1.0-1.x86_64.rpm")},
	}

	unlock, err := config.LockGPGKeyCache(context.Background())
	if err != nil {
		t.Fatalf("LockGPGKeyCache() error = %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	t.Cleanup(runctx.SetContext(ctx))

	done := make(chan error, 2)
	go func() { done <- publishKeyRefreshes([]*keyRefresh{refresh}) }()
	go func() {
		_, err := snapshotCachedKeys([]string{keyURL})
		done <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("error = %v, want one wrapping the cancellation", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("did not return after the build was cancelled")
		}
	}
	if got := cachedGPGKey(t, keyURL); got != oldKey {
		t.Fatalf("the cache was written without the lock")
	}
}

// Publishing holds the cache lock from reading the cached keys until the last
// write, so a build that has the lock is not interleaved with.
func TestPublishKeyRefreshesWaitsForTheCacheLock(t *testing.T) {
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	stubVerifyAcceptingKey(t, newKey)
	destDir, _ := setupRotatedKeyRepo(t, oldKey, &newKey)
	keyURL := RepoCfg.GPGKey
	refresh := &keyRefresh{
		keyURLs:   []string{keyURL},
		cached:    map[string][]byte{keyURL: []byte(oldKey)},
		refreshed: map[string][]byte{keyURL: []byte(newKey)},
		failed:    []string{filepath.Join(destDir, "pkg-1.0-1.x86_64.rpm")},
	}

	unlock, err := config.LockGPGKeyCache(context.Background())
	if err != nil {
		t.Fatalf("LockGPGKeyCache() error = %v", err)
	}
	done := make(chan struct{})
	go func() {
		if err := publishKeyRefreshes([]*keyRefresh{refresh}); err != nil {
			t.Errorf("publishKeyRefreshes() error = %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("publishKeyRefreshes() returned while another build held the cache lock")
	case <-time.After(200 * time.Millisecond):
	}
	if got := cachedGPGKey(t, keyURL); got != oldKey {
		t.Fatalf("the cache was written while another build held the lock")
	}
	unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("publishKeyRefreshes() did not finish after the lock was released")
	}
	if got := cachedGPGKey(t, keyURL); got != newKey {
		t.Fatalf("the refreshed key was not cached once the lock was released")
	}
}

// A key URL listed more than once is downloaded once. If a second request were
// made and failed, the cached key would replace the rotated key already
// downloaded for the retry, and the RPMs signed by the new key would be rejected.
func TestVerifyRPMsWithKeyRefreshFetchesARepeatedKeyURLOnce(t *testing.T) {
	oldKey, newKey := newTestGPGPublicKey(t, "old"), newTestGPGPublicKey(t, "new")
	cacheRoot := setRPMMetadataTestCache(t)

	var requests int32
	var rotated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		switch {
		case !rotated.Load():
			_, _ = w.Write([]byte(oldKey))
		case n == 1:
			_, _ = w.Write([]byte(newKey))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	keyURL := server.URL + "/RPM-GPG-KEY.asc"
	requireGPGKeyCacheUnder(t, keyURL, cacheRoot)
	if _, err := config.GetCachedOrDownloadGPGKey(keyURL); err != nil {
		t.Fatalf("failed to prime the GPG key cache: %v", err)
	}
	rotated.Store(true)
	atomic.StoreInt32(&requests, 0)
	stubVerifyAcceptingKey(t, newKey)

	rpm := filepath.Join(t.TempDir(), "pkg-1.0-1.x86_64.rpm")
	if err := os.WriteFile(rpm, []byte("rpm"), 0644); err != nil {
		t.Fatalf("failed to create test rpm: %v", err)
	}

	refresh, err := verifyRPMsWithKeyRefresh([]string{rpm}, []string{keyURL, keyURL})
	if err != nil {
		t.Fatalf("verifyRPMsWithKeyRefresh() error = %v, want the downloaded key to verify the RPM", err)
	}
	if n := atomic.LoadInt32(&requests); n != 1 {
		t.Fatalf("key requests = %d, want the repeated URL fetched once", n)
	}
	if got := string(refresh.refreshed[keyURL]); got != newKey {
		t.Fatalf("the rotated key was not kept for publishing")
	}
}

// A reader must see the cache as it was at one moment. A publisher that replaces
// (A:X, B:K) with (A:K, B:N) keeps signer K available throughout; a reader that
// read A before the publication and B after would hold only X and N and reject
// an RPM signed by K.
func TestSnapshotCachedKeysIsNotInterleavedWithAPublication(t *testing.T) {
	keyX, keyK, keyN := newTestGPGPublicKey(t, "x"), newTestGPGPublicKey(t, "k"), newTestGPGPublicKey(t, "n")
	setRPMMetadataTestCache(t)
	urlA, urlB := "https://a.example/RPM-GPG-KEY", "https://b.example/RPM-GPG-KEY"

	store := func(keys map[string]string) {
		for keyURL, keyData := range keys {
			if err := config.StoreGPGKey(keyURL, []byte(keyData)); err != nil {
				t.Errorf("StoreGPGKey(%s) error = %v", keyURL, err)
			}
		}
	}
	store(map[string]string{urlA: keyX, urlB: keyK})

	published := make(chan struct{})
	afterCachedKeyRead = func(keyURL string) {
		if keyURL != urlA {
			return
		}
		go func() {
			defer close(published)
			unlock, err := config.LockGPGKeyCache(context.Background())
			if err != nil {
				t.Errorf("LockGPGKeyCache() error = %v", err)
				return
			}
			defer unlock()
			store(map[string]string{urlA: keyK, urlB: keyN})
		}()
		// A publisher that is not held back by the reader's lock finishes here.
		select {
		case <-published:
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Cleanup(func() { afterCachedKeyRead = func(string) {} })

	snapshot, err := snapshotCachedKeys([]string{urlA, urlB})
	if err != nil {
		t.Fatalf("snapshotCachedKeys() error = %v", err)
	}
	<-published

	if string(snapshot[urlA]) != keyX || string(snapshot[urlB]) != keyK {
		t.Fatalf("snapshot mixes cache states: A is the old key %v, B is the old key %v, want both from before the publication",
			string(snapshot[urlA]) == keyX, string(snapshot[urlB]) == keyK)
	}
	if got := cachedGPGKey(t, urlB); got != keyN {
		t.Fatalf("the publication did not complete once the snapshot released the lock")
	}
}
