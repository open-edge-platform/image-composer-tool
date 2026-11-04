package debutils

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
)

// TestMain keeps every test in the package off the real retry schedule, before
// any test runs, so no test has to swap it out. Behind an HTTP proxy, a test URL
// whose host does not resolve is answered 504, and waiting out the full budget
// would time the package out. Two attempts still exercise one retry.
func TestMain(m *testing.M) {
	probeRetrySchedule = probeSchedule{attempts: 2, backoff: time.Millisecond, maxBackoff: 2 * time.Millisecond}
	os.Exit(m.Run())
}

// fastSchedule is the production attempt count with millisecond waits, for
// tests that pass the schedule in.
var fastSchedule = probeSchedule{attempts: 6, backoff: time.Millisecond, maxBackoff: 2 * time.Millisecond}

// scriptedServer answers each path with its scripted statuses in order,
// repeating the last one, and counts the requests per path.
type scriptedServer struct {
	mu      sync.Mutex
	scripts map[string][]int
	hits    map[string]int
}

// noAnswer in a script resets the connection instead of answering, as a host
// shedding load may.
const noAnswer = 0

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
		status := statuses[min(n, len(statuses)-1)]
		if status == noAnswer {
			resetConnection(t, w)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return s, srv.URL
}

// resetConnection drops the request's connection with a TCP reset.
func resetConnection(t *testing.T, w http.ResponseWriter) {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		t.Errorf("hijacking the connection: %v", err)
		return
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
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

			found, got, err := probeURLExistenceWith(base+"/f", fastSchedule)
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

			found, _, err := probeURLExistenceWith(base+"/f", fastSchedule)
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

			found, _, err := probeURLExistenceWith(base+"/f", fastSchedule)
			if found || !errors.Is(err, errRepoUnavailable) {
				t.Fatalf("probeURLExistence = (%v, %v), want (false, errRepoUnavailable)", found, err)
			}
			if n := srv.count("/f"); n != fastSchedule.attempts {
				t.Errorf("requests = %d, want %d", n, fastSchedule.attempts)
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

// A host shedding load may stall, refuse, reset or close a connection instead
// of answering 503; that is no verdict on the file either.
func TestProbeURLExistence_RetriesWhenServerDoesNotAnswer(t *testing.T) {
	srv, base := newScriptedServer(t, map[string][]int{"/f": {noAnswer, noAnswer, http.StatusOK}})

	found, got, err := probeURLExistenceWith(base+"/f", fastSchedule)
	if err != nil || !found || got != http.StatusOK {
		t.Fatalf("probeURLExistence = (%v, %d, %v), want (true, 200, nil)", found, got, err)
	}
	if n := srv.count("/f"); n != 3 {
		t.Errorf("requests = %d, want 3 (two reset connections retried, then 200)", n)
	}
}

func TestProbeURLExistence_GivesUpWhenServerNeverAnswers(t *testing.T) {
	srv, base := newScriptedServer(t, map[string][]int{"/f": {noAnswer}})

	found, _, err := probeURLExistenceWith(base+"/f", fastSchedule)
	if found || !errors.Is(err, errRepoUnavailable) || !errors.Is(err, errNoResponse) {
		t.Fatalf("probeURLExistence = (%v, %v), want (false, errRepoUnavailable wrapping errNoResponse)", found, err)
	}
	if n := srv.count("/f"); n != fastSchedule.attempts {
		t.Errorf("requests = %d, want %d", n, fastSchedule.attempts)
	}
	if !strings.Contains(err.Error(), base+"/f") {
		t.Errorf("error should name the URL: %v", err)
	}
}

// A name that does not resolve still reads as an absent file at once, so a
// typo'd repository URL is not waited on. Other failures without a response
// are retried; the rest are errors straight away.
func TestProbeNetworkError(t *testing.T) {
	const target = "https://repo.example/dists/noble/InRelease"
	wrap := func(err error) error { return &url.Error{Op: "Head", URL: target, Err: err} }
	dial := func(errno syscall.Errno) error {
		return wrap(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", errno)})
	}
	tests := []struct {
		name            string
		err             error
		wantUnavailable bool
		wantErr         bool
	}{
		{"host does not resolve", wrap(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "repo.example", IsNotFound: true}}), false, false},
		{"timed out", wrap(context.DeadlineExceeded), true, true},
		{"connection refused", dial(syscall.ECONNREFUSED), true, true},
		{"connection reset", wrap(&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}), true, true},
		{"closed without a response", wrap(io.EOF), true, true},
		{"network unreachable", dial(syscall.ENETUNREACH), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := probeNetworkError(target, tt.err)
			if got.found || got.status != 0 || got.unavailable() != tt.wantUnavailable || (got.err != nil) != tt.wantErr {
				t.Errorf("probeNetworkError(%v) = %+v, want unavailable=%v err=%v", tt.err, got, tt.wantUnavailable, tt.wantErr)
			}
		})
	}
}

// retryAfterServer answers its first request 503 with the given Retry-After
// and every later one 200, counting the requests.
func retryAfterServer(t *testing.T, retryAfter string) (string, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", retryAfter)
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &requests
}

func TestProbeURLExistence_HonorsRetryAfter(t *testing.T) {
	base, _ := retryAfterServer(t, "1")

	start := time.Now()
	found, _, err := probeURLExistenceWith(base+"/f", probeSchedule{attempts: 6, backoff: time.Millisecond, maxBackoff: 5 * time.Second})
	if err != nil || !found {
		t.Fatalf("probeURLExistence = (%v, %v), want (true, nil)", found, err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("retried after %s, want at least the 1s Retry-After", elapsed)
	}
}

// A Retry-After beyond the longest wait would only be answered with another
// 503, so the probe gives up at once.
func TestProbeURLExistence_GivesUpWhenRetryAfterExceedsCap(t *testing.T) {
	base, requests := retryAfterServer(t, "600")

	_, _, err := probeURLExistenceWith(base+"/f", fastSchedule)
	if !errors.Is(err, errRepoUnavailable) || !strings.Contains(err.Error(), "10m0s") {
		t.Fatalf("err = %v, want errRepoUnavailable naming the 10m0s Retry-After", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
}

func TestProbeURLExistence_CancelledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restore := runctx.SetContext(ctx)
	defer restore()
	_, base := newScriptedServer(t, map[string][]int{"/f": {http.StatusServiceUnavailable}})

	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, _, err := probeURLExistenceWith(base+"/f", probeSchedule{attempts: 6, backoff: time.Minute, maxBackoff: time.Minute})
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
			name:        "InRelease still unavailable reports the outage, with the classic files",
			scripts:     map[string][]int{inRelease: {unavailable}},
			wantErr:     true,
			wantRelease: "/dists/noble/Release",
			wantSign:    sign,
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
			if tt.wantErr && !errors.Is(err, errRepoUnavailable) {
				t.Fatalf("err = %v, want errRepoUnavailable", err)
			}
			if !tt.wantErr && err != nil {
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

// An outage that hides the release format must not fail a repository whose
// classic release files an earlier build cached: ParseRepositoryMetadata
// builds from that cache when it cannot refresh it.
func TestBuildRepoConfigs_UnavailableRepositoryFallsBackToCachedMetadata(t *testing.T) {
	failures := []struct {
		name   string
		script int
	}{{"503", http.StatusServiceUnavailable}, {"no answer", noAnswer}}
	cases := []struct {
		name    string
		cached  []string
		wantErr bool
	}{
		{name: "classic release files cached", cached: []string{"Release", "Release.gpg"}},
		{name: "signature missing from the cache", cached: []string{"Release"}, wantErr: true},
		{name: "nothing cached", wantErr: true},
	}
	for _, failure := range failures {
		for _, tt := range cases {
			t.Run(failure.name+"/"+tt.name, func(t *testing.T) {
				resetURLExistenceCacheForTest(t)
				_, base := newScriptedServer(t, map[string][]int{
					"/dists/noble/InRelease":   {failure.script},
					"/dists/noble/Release.gpg": {failure.script},
				})
				// An earlier build found the package index, so finding it again
				// needs no probe.
				pkgList := base + "/dists/noble/main/binary-amd64/Packages.gz"
				savePackageListURLToCache(base, "noble", "amd64", "main", pkgList)
				buildPath := repoMetadataBuildPath("snapshot", base, "noble", "amd64", "main", pkgList)
				t.Cleanup(func() { _ = os.RemoveAll(buildPath) })
				if err := os.MkdirAll(buildPath, 0755); err != nil {
					t.Fatal(err)
				}
				for _, name := range tt.cached {
					if err := os.WriteFile(filepath.Join(buildPath, name), []byte("cached"), 0644); err != nil {
						t.Fatal(err)
					}
				}

				configs, err := BuildRepoConfigs([]Repository{{ID: "snapshot", Codename: "noble", URL: base, PKey: "k"}}, "amd64")
				if tt.wantErr {
					if !errors.Is(err, errRepoUnavailable) {
						t.Fatalf("err = %v, want errRepoUnavailable", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("BuildRepoConfigs should carry on with the cached metadata, got: %v", err)
				}
				if len(configs) != 1 || configs[0].ReleaseFile != base+"/dists/noble/Release" ||
					configs[0].ReleaseSign != base+"/dists/noble/Release.gpg" || configs[0].BuildPath != buildPath {
					t.Errorf("configs = %+v, want one entry for the cached classic release files", configs)
				}
			})
		}
	}
}
