package debutils

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
)

// TestMain keeps every test in the package off the real retry schedule. Behind
// an HTTP proxy, a test URL whose host does not resolve is answered 504, and
// waiting out the full budget would time the package out.
func TestMain(m *testing.M) {
	probeUnavailableBackoff, maxProbeUnavailableBackoff = time.Millisecond, 2*time.Millisecond
	os.Exit(m.Run())
}

// scriptedServer answers each path with its scripted statuses in order,
// repeating the last one, and counts the requests per path.
type scriptedServer struct {
	mu      sync.Mutex
	scripts map[string][]int
	hits    map[string]int
}

func newScriptedServer(t *testing.T, scripts map[string][]int) (*scriptedServer, string) {
	t.Helper()
	s := &scriptedServer{scripts: scripts, hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		statuses, ok := s.scripts[r.URL.Path]
		n := s.hits[r.URL.Path]
		s.hits[r.URL.Path] = n + 1
		s.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(statuses[min(n, len(statuses)-1)])
	}))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func (s *scriptedServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

func TestProbeURLExistence_RetriesUnavailableStatuses(t *testing.T) {
	for _, status := range []int{
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, base := newScriptedServer(t, map[string][]int{"/f": {status, status, http.StatusOK}})

			found, got, err := probeURLExistence(base + "/f")
			if err != nil || !found || got != http.StatusOK {
				t.Fatalf("probeURLExistence = (%v, %d, %v), want (true, 200, nil)", found, got, err)
			}
			if n := srv.count("/f"); n != 3 {
				t.Errorf("requests = %d, want 3 (two %d answers retried, then 200)", n, status)
			}
		})
	}
}

func TestProbeURLExistence_DoesNotRetryDefiniteAnswers(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantFound bool
		wantErr   bool
	}{
		{name: "present", status: http.StatusOK, wantFound: true},
		{name: "missing", status: http.StatusNotFound},
		{name: "internal server error", status: http.StatusInternalServerError, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, base := newScriptedServer(t, map[string][]int{"/f": {tt.status}})

			found, _, err := probeURLExistence(base + "/f")
			if found != tt.wantFound || (err != nil) != tt.wantErr {
				t.Fatalf("probeURLExistence = (%v, %v), want found=%v err=%v", found, err, tt.wantFound, tt.wantErr)
			}
			if errors.Is(err, errRepoUnavailable) {
				t.Errorf("a %d answer must not be reported as an unavailable server: %v", tt.status, err)
			}
			if n := srv.count("/f"); n != 1 {
				t.Errorf("requests = %d, want 1 (a %d answer is not retried)", n, tt.status)
			}
		})
	}
}

func TestProbeURLExistence_GivesUpWhenStillUnavailable(t *testing.T) {
	for _, status := range []int{
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, base := newScriptedServer(t, map[string][]int{"/f": {status}})

			found, _, err := probeURLExistence(base + "/f")
			if found || !errors.Is(err, errRepoUnavailable) {
				t.Fatalf("probeURLExistence = (%v, %v), want (false, errRepoUnavailable)", found, err)
			}
			if n := srv.count("/f"); n != probeUnavailableAttempts {
				t.Errorf("requests = %d, want %d", n, probeUnavailableAttempts)
			}
			// A 429 reaches here with no error from probeURL; the message must
			// still say which URL refused and with what status.
			msg := err.Error()
			if !strings.Contains(msg, base+"/f") || !strings.Contains(msg, strconv.Itoa(status)) || strings.Contains(msg, "%!") {
				t.Errorf("error should name the URL and status %d, got: %s", status, msg)
			}
		})
	}
}

func TestProbeURLExistence_CancelledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restore := runctx.SetContext(ctx)
	defer restore()
	backoff := probeUnavailableBackoff
	probeUnavailableBackoff = time.Minute
	t.Cleanup(func() { probeUnavailableBackoff = backoff })
	_, base := newScriptedServer(t, map[string][]int{"/f": {http.StatusServiceUnavailable}})

	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, _, err := probeURLExistence(base + "/f")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want it to wrap context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("returned after %s; the backoff wait should end as soon as the run is cancelled", elapsed)
	}
}

func TestResolveReleaseURLs_ServerUnavailability(t *testing.T) {
	const (
		inRelease = "/dists/noble/InRelease"
		sign      = "/dists/noble/Release.gpg"
	)
	unavailable := http.StatusServiceUnavailable
	tests := []struct {
		name        string
		scripts     map[string][]int
		wantErr     bool
		wantRelease string
		wantSign    string
	}{
		{
			name:    "InRelease still unavailable fails instead of falling back",
			scripts: map[string][]int{inRelease: {unavailable}},
			wantErr: true,
		},
		{
			name:        "InRelease recovering after a 503 is used",
			scripts:     map[string][]int{inRelease: {unavailable, http.StatusOK}},
			wantRelease: inRelease,
			wantSign:    inReleaseSentinel,
		},
		{
			name:        "InRelease confirmed missing still falls back to classic files",
			scripts:     map[string][]int{},
			wantRelease: "/dists/noble/Release",
			wantSign:    sign,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetURLExistenceCacheForTest(t)
			_, base := newScriptedServer(t, tt.scripts)

			release, signURL, err := resolveReleaseURLs(base, "noble", "Release")
			if tt.wantErr {
				if !errors.Is(err, errRepoUnavailable) {
					t.Fatalf("err = %v, want errRepoUnavailable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveReleaseURLs error: %v", err)
			}
			if release != base+tt.wantRelease {
				t.Errorf("release URL = %q, want %q", release, base+tt.wantRelease)
			}
			wantSign := tt.wantSign
			if wantSign != inReleaseSentinel {
				wantSign = base + wantSign
			}
			if signURL != wantSign {
				t.Errorf("release sign URL = %q, want %q", signURL, wantSign)
			}
		})
	}
}

// regression test: a repository whose InRelease, Release.gpg and package
// index all answer 503 must fail as unavailable, not fall back to classic
// Release/Release.gpg files it does not publish.
func TestBuildRepoConfigs_UnavailableRepositoryReportsOutage(t *testing.T) {
	resetURLExistenceCacheForTest(t)
	unavailable := []int{http.StatusServiceUnavailable}
	_, base := newScriptedServer(t, map[string][]int{
		"/dists/noble/InRelease":                     unavailable,
		"/dists/noble/Release.gpg":                   unavailable,
		"/dists/noble/main/binary-amd64/Packages.gz": unavailable,
	})

	_, err := BuildRepoConfigs([]Repository{{ID: "snapshot", Codename: "noble", URL: base, PKey: "k"}}, "amd64")
	if !errors.Is(err, errRepoUnavailable) {
		t.Fatalf("err = %v, want errRepoUnavailable", err)
	}
	if !strings.Contains(err.Error(), "InRelease") {
		t.Errorf("error should name the InRelease probe the server kept refusing: %v", err)
	}
}
