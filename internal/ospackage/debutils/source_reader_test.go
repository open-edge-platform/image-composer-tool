package debutils

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

func TestSourceRepositoryCache(t *testing.T) {
	t.Parallel()

	signer, err := openpgp.NewEntity("Signer", "test", "signer@example.invalid", nil)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	other, err := openpgp.NewEntity("Other", "test", "other@example.invalid", nil)
	if err != nil {
		t.Fatalf("create other signer: %v", err)
	}
	var key, wrongKey bytes.Buffer
	if err := signer.Serialize(&key); err != nil {
		t.Fatalf("serialize key: %v", err)
	}
	if err := other.Serialize(&wrongKey); err != nil {
		t.Fatalf("serialize other key: %v", err)
	}
	type snapshot struct{ release, index []byte }
	makeSnapshot := func(directory string) snapshot {
		t.Helper()
		checksum := strings.Repeat("a", 64)
		index := []byte("Package: mesa\nVersion: 1\nDirectory: " + directory + "\nChecksums-Sha256:\n " +
			checksum + " 10 mesa.dsc\n")
		release := fmt.Sprintf("Valid-Until: Sat, 01 Jan 2000 00:00:00 GMT\nSHA256:\n %x %d main/source/Sources\n",
			sha256.Sum256(index), len(index))
		var signed bytes.Buffer
		writer, err := clearsign.Encode(&signed, signer.PrivateKey, nil)
		if err != nil {
			t.Fatalf("create InRelease: %v", err)
		}
		if _, err := writer.Write([]byte(release)); err != nil {
			t.Fatalf("write InRelease: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close InRelease: %v", err)
		}
		return snapshot{release: signed.Bytes(), index: index}
	}
	first := makeSnapshot("pool/one")
	second := makeSnapshot("pool/two")
	var current atomic.Value
	current.Store(first)
	var invalidSignature atomic.Bool
	var serverError atomic.Bool
	var indexRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if serverError.Load() {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		snapshot := current.Load().(snapshot)
		switch request.URL.Path {
		case "/dists/test/InRelease":
			if invalidSignature.Load() {
				_, _ = response.Write([]byte("invalid signature"))
			} else {
				_, _ = response.Write(snapshot.release)
			}
		case "/dists/test/main/source/Sources":
			indexRequests.Add(1)
			_, _ = response.Write(snapshot.index)
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	options := sourceRepositoryOptions{
		BaseURL: server.URL, Suite: "test", Component: "main", Key: key.Bytes(), CacheDir: t.TempDir(),
	}

	result, err := readSourceRepository(context.Background(), options)
	if err != nil || !result.Expired || result.Offline || result.Packages[0].Directory != "pool/one" {
		t.Fatalf("first snapshot = %+v, %v", result, err)
	}
	result, err = readSourceRepository(context.Background(), options)
	if err != nil || result.Offline || indexRequests.Load() != 1 {
		t.Fatalf("cache hit = %+v, %v, index requests %d", result, err, indexRequests.Load())
	}
	current.Store(second)
	result, err = readSourceRepository(context.Background(), options)
	if err != nil || result.Packages[0].Directory != "pool/two" {
		t.Fatalf("second snapshot = %+v, %v", result, err)
	}
	entries, err := os.ReadDir(filepath.Join(options.CacheDir, sourceCacheIdentity(options)))
	if err != nil || len(entries) != 3 {
		t.Fatalf("cached snapshots = %v, %v; want two snapshots and latest", entries, err)
	}
	invalidSignature.Store(true)
	if result, err := readSourceRepository(context.Background(), options); !errors.Is(err, errSourceVerification) || result != nil {
		t.Fatalf("invalid live signature = %+v, %v; want verification failure", result, err)
	}
	invalidSignature.Store(false)
	serverError.Store(true)
	result, err = readSourceRepository(context.Background(), options)
	if err != nil || !result.Offline || !result.Expired || result.Packages[0].Directory != "pool/two" {
		t.Fatalf("server error fallback = %+v, %v", result, err)
	}
	server.Close()
	result, err = readSourceRepository(context.Background(), options)
	if err != nil || !result.Offline || !result.Expired || result.Packages[0].Directory != "pool/two" {
		t.Fatalf("offline snapshot = %+v, %v", result, err)
	}
	options.Key = wrongKey.Bytes()
	if result, err := readSourceRepository(context.Background(), options); !errors.Is(err, errSourceVerification) || result != nil {
		t.Fatalf("offline with wrong key = %+v, %v; want verification failure", result, err)
	}
	options.Key = key.Bytes()
	cacheDir := filepath.Join(options.CacheDir, sourceCacheIdentity(options))
	pointer, err := os.ReadFile(filepath.Join(cacheDir, "latest"))
	if err != nil {
		t.Fatalf("read cache pointer: %v", err)
	}
	cachePath := filepath.Join(cacheDir, string(pointer)+".json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var cache sourceCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	cache.Result.Packages[0].Name = "tampered"
	data, err = json.Marshal(cache)
	if err != nil {
		t.Fatalf("encode altered cache: %v", err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatalf("write altered cache: %v", err)
	}
	result, err = readSourceRepository(context.Background(), options)
	if err != nil || result.Packages[0].Name != "mesa" {
		t.Fatalf("offline parsed cache = %+v, %v; want signed index contents", result, err)
	}
	cache.Result.Index[0] ^= 1
	data, err = json.Marshal(cache)
	if err != nil {
		t.Fatalf("encode cache: %v", err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatalf("corrupt cache: %v", err)
	}
	if result, err := readSourceRepository(context.Background(), options); !errors.Is(err, errSourceVerification) || result != nil {
		t.Fatalf("tampered offline index = %+v, %v; want verification failure", result, err)
	}
}

func TestSourceCacheSnapshotIdentity(t *testing.T) {
	t.Parallel()

	options := sourceRepositoryOptions{BaseURL: "https://example.invalid/repo", Suite: "test", Component: "main"}
	result := &sourceRepositoryResult{
		Release: []byte("release"), IndexMetadata: sourceIndexMetadata{Path: "main/source/Sources", SHA256: "digest"},
	}
	original := sourceCacheSnapshotKey(options, result)
	otherOptions := options
	otherOptions.BaseURL = "https://other.invalid/repo"
	if sourceCacheSnapshotKey(otherOptions, result) == original {
		t.Error("different repository shares a cache snapshot")
	}
	otherOptions = options
	otherOptions.Suite = "other"
	if sourceCacheSnapshotKey(otherOptions, result) == original {
		t.Error("different suite shares a cache snapshot")
	}
	otherOptions = options
	otherOptions.Component = "other"
	if sourceCacheSnapshotKey(otherOptions, result) == original {
		t.Error("different component shares a cache snapshot")
	}
	otherResult := *result
	otherResult.Release = []byte("different release")
	if sourceCacheSnapshotKey(options, &otherResult) == original {
		t.Error("different Release shares a cache snapshot")
	}
	otherResult = *result
	otherResult.IndexMetadata.Path += ".gz"
	if sourceCacheSnapshotKey(options, &otherResult) == original {
		t.Error("different compression shares a cache snapshot")
	}
	otherResult = *result
	otherResult.IndexMetadata.SHA256 = "other digest"
	if sourceCacheSnapshotKey(options, &otherResult) == original {
		t.Error("different index digest shares a cache snapshot")
	}
}

func TestReadSourceRepositoryCompressionFallback(t *testing.T) {
	t.Parallel()

	signer, err := openpgp.NewEntity("Signer", "test", "signer@example.invalid", nil)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	var key bytes.Buffer
	if err := signer.Serialize(&key); err != nil {
		t.Fatalf("serialize key: %v", err)
	}
	checksum := strings.Repeat("a", 64)
	index := []byte("Package: pkg\nVersion: 1\nDirectory: pool/pkg\nChecksums-Sha256:\n " + checksum + " 1 pkg.dsc\n")
	release := fmt.Sprintf("SHA256:\n %x 1 main/source/Sources.xz\n %x %d main/source/Sources\n",
		sha256.Sum256([]byte("x")), sha256.Sum256(index), len(index))
	var signed bytes.Buffer
	writer, err := clearsign.Encode(&signed, signer.PrivateKey, nil)
	if err != nil {
		t.Fatalf("create InRelease: %v", err)
	}
	if _, err := writer.Write([]byte(release)); err != nil {
		t.Fatalf("write InRelease: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close InRelease: %v", err)
	}
	var preferredRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/dists/test/InRelease":
			_, _ = response.Write(signed.Bytes())
		case "/dists/test/main/source/Sources.xz":
			preferredRequests.Add(1)
			http.NotFound(response, request)
		case "/dists/test/main/source/Sources":
			_, _ = response.Write(index)
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)

	result, err := ReadSourceRepository(context.Background(), SourceRepositoryOptions{
		BaseURL: server.URL, Suite: "test", Component: "main", Key: key.Bytes(),
	})
	if err != nil || result.IndexMetadata.Path != "main/source/Sources" || preferredRequests.Load() != 1 {
		t.Fatalf("fallback result = %+v, %v; preferred requests %d", result, err, preferredRequests.Load())
	}
	if pkg, err := result.FindPackage("pkg", "1"); err != nil || pkg.Name != "pkg" {
		t.Fatalf("exact source match = %+v, %v", pkg, err)
	}
}

func TestReadSourceRepository(t *testing.T) {
	t.Parallel()

	signer, err := openpgp.NewEntity("Signer", "test", "signer@example.invalid", nil)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	var key bytes.Buffer
	if err := signer.Serialize(&key); err != nil {
		t.Fatalf("serialize key: %v", err)
	}
	checksum := strings.Repeat("a", 64)
	index := []byte("Package: mesa\nVersion: 2:26.0-1\nDirectory: pool/main/m/mesa\nChecksums-Sha256:\n " +
		checksum + " 10 mesa.dsc\n " + checksum + " 20 mesa.orig.tar.xz\n " + checksum + " 30 mesa.extra.tar.gz\n")
	release := []byte(fmt.Sprintf("Suite: test\nSHA256:\n %x %d main/source/Sources\n", sha256.Sum256(index), len(index)))
	var inRelease, signature bytes.Buffer
	writer, err := clearsign.Encode(&inRelease, signer.PrivateKey, nil)
	if err != nil {
		t.Fatalf("create InRelease: %v", err)
	}
	if _, err := writer.Write(release); err != nil {
		t.Fatalf("write InRelease: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close InRelease: %v", err)
	}
	if err := openpgp.DetachSign(&signature, signer, bytes.NewReader(release), nil); err != nil {
		t.Fatalf("sign Release: %v", err)
	}
	tests := []struct {
		name             string
		detached         bool
		invalidSignature bool
		indexBody        []byte
		indexStatus      int
		wantError        error
	}{
		{name: "inrelease", indexBody: index},
		{name: "detached", detached: true, indexBody: index},
		{name: "invalid signature", invalidSignature: true, indexBody: index, wantError: errSourceVerification},
		{name: "digest mismatch", indexBody: bytes.Repeat([]byte("x"), len(index)), wantError: errSourceVerification},
		{name: "undersized index", indexBody: []byte("short"), wantError: errSourceVerification},
		{name: "oversized index", indexBody: append(bytes.Clone(index), 'x'), wantError: errSourceVerification},
		{name: "missing index", indexStatus: http.StatusNotFound, wantError: errSourceNotFound},
		{name: "index redirect", indexStatus: http.StatusFound, wantError: errSourceHTTPStatus},
		{name: "network server error", indexStatus: http.StatusInternalServerError, wantError: errSourceHTTPStatus},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var artifactRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/repo/dists/test/InRelease":
					if tt.detached {
						http.NotFound(response, request)
					} else if tt.invalidSignature {
						_, _ = response.Write([]byte("invalid"))
					} else {
						_, _ = response.Write(inRelease.Bytes())
					}
				case "/repo/dists/test/Release":
					_, _ = response.Write(release)
				case "/repo/dists/test/Release.gpg":
					_, _ = response.Write(signature.Bytes())
				case "/repo/dists/test/main/source/Sources":
					if tt.indexStatus != 0 {
						response.Header().Set("Location", "https://unapproved.example/Sources")
						response.WriteHeader(tt.indexStatus)
					} else {
						_, _ = response.Write(tt.indexBody)
					}
				default:
					artifactRequests.Add(1)
					http.NotFound(response, request)
				}
			}))
			t.Cleanup(server.Close)

			result, err := readSourceRepository(context.Background(), sourceRepositoryOptions{
				BaseURL: server.URL + "/repo", Suite: "test", Component: "main", Key: key.Bytes(),
			})
			if tt.wantError != nil {
				if !errors.Is(err, tt.wantError) || result != nil {
					t.Fatalf("readSourceRepository = %+v, %v; want %v", result, err, tt.wantError)
				}
			} else if err != nil || len(result.Packages) != 1 || len(result.Packages[0].Artifacts) != 3 ||
				result.Packages[0].Artifacts[2].URL != server.URL+"/repo/pool/main/m/mesa/mesa.extra.tar.gz" {
				t.Fatalf("readSourceRepository = %+v, %v", result, err)
			}
			if artifactRequests.Load() != 0 {
				t.Errorf("unexpected artifact requests: %d", artifactRequests.Load())
			}
		})
	}
}
