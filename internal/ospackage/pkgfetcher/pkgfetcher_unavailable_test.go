package pkgfetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
)

// The unavailable schedule waits minutes in production; tests that exercise it
// would otherwise take that long.
func TestMain(m *testing.M) {
	unavailableRetryBackoff = 10 * time.Millisecond
	maxUnavailableRetryBackoff = 50 * time.Millisecond
	os.Exit(m.Run())
}

func TestUnavailableWait(t *testing.T) {
	const base, limit = 15 * time.Second, 2 * time.Minute
	tests := []struct {
		name       string
		n, worker  int
		retryAfter time.Duration
		want       time.Duration
	}{
		{"first wait", 1, 0, 0, 15 * time.Second},
		{"doubles", 2, 0, 0, 30 * time.Second},
		{"doubles again", 3, 0, 0, 60 * time.Second},
		{"capped", 4, 0, 0, 2 * time.Minute},
		{"stays capped", 5, 0, 0, 2 * time.Minute},
		{"worker offset", 1, 3, 0, 18 * time.Second},
		{"retry-after above schedule", 1, 0, 90 * time.Second, 90 * time.Second},
		{"retry-after below schedule", 3, 0, 5 * time.Second, 60 * time.Second},
		{"retry-after capped", 1, 0, 10 * time.Minute, 2 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unavailableWait(tt.n, tt.worker, tt.retryAfter, base, limit); got != tt.want {
				t.Errorf("unavailableWait(%d, %d, %s) = %s, want %s", tt.n, tt.worker, tt.retryAfter, got, tt.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"120", 120 * time.Second},
		{" 7 ", 7 * time.Second},
		{"0", 0},
		{"-5", 0},
		{"soon", 0},
		{time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat), 0},
	} {
		if got := parseRetryAfter(tt.in); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got < 85*time.Second || got > 90*time.Second {
		t.Errorf("parseRetryAfter(HTTP-date +90s) = %s, want about 90s", got)
	}
}

// serveUnavailable answers status to the first failures requests, then 200.
func serveUnavailable(t *testing.T, status int, failures int32, retryAfter string) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&count, 1) <= failures {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	return server, &count
}

// Five 503s in a row exceed the default three attempts but stay within the
// budget a 503 earns, as when Launchpad's snapshot host sheds load.
func TestDownloadWithRetry_ServiceUnavailableGetsLongerBudget(t *testing.T) {
	server, count := serveUnavailable(t, http.StatusServiceUnavailable, maxUnavailableAttempts-1, "")
	dest := filepath.Join(t.TempDir(), "pkg.deb")

	if err := downloadWithRetry(context.Background(), network.GetSecureHTTPClient(), server.URL+"/pkg.deb", dest, 0); err != nil {
		t.Fatalf("downloadWithRetry: %v", err)
	}
	if got := atomic.LoadInt32(count); got != maxUnavailableAttempts {
		t.Errorf("requests = %d, want %d", got, maxUnavailableAttempts)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("package not written: %v", err)
	}
}

func TestDownloadWithRetry_ServiceUnavailableGivesUp(t *testing.T) {
	server, count := serveUnavailable(t, http.StatusServiceUnavailable, 1000, "")
	dest := filepath.Join(t.TempDir(), "pkg.deb")

	err := downloadWithRetry(context.Background(), network.GetSecureHTTPClient(), server.URL+"/pkg.deb", dest, 0)
	if err == nil || !strings.Contains(err.Error(), "after 6 attempts") {
		t.Fatalf("err = %v, want failure after 6 attempts", err)
	}
	if got := atomic.LoadInt32(count); got != maxUnavailableAttempts {
		t.Errorf("requests = %d, want %d", got, maxUnavailableAttempts)
	}
}

// A Retry-After beyond the longest wait would only be answered with another 503,
// so the download gives up at once rather than spending its remaining attempts.
func TestDownloadWithRetry_GivesUpWhenRetryAfterExceedsCap(t *testing.T) {
	server, count := serveUnavailable(t, http.StatusServiceUnavailable, 1000, "600")
	dest := filepath.Join(t.TempDir(), "pkg.deb")

	err := downloadWithRetry(context.Background(), network.GetSecureHTTPClient(), server.URL+"/pkg.deb", dest, 0)
	if err == nil || !strings.Contains(err.Error(), "retry after 10m0s") {
		t.Fatalf("err = %v, want it to report the 10m0s Retry-After", err)
	}
	if got := atomic.LoadInt32(count); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

func TestDownloadWithRetry_HonorsRetryAfter(t *testing.T) {
	oldBase, oldLimit := unavailableRetryBackoff, maxUnavailableRetryBackoff
	unavailableRetryBackoff, maxUnavailableRetryBackoff = time.Millisecond, 5*time.Second
	t.Cleanup(func() { unavailableRetryBackoff, maxUnavailableRetryBackoff = oldBase, oldLimit })

	server, _ := serveUnavailable(t, http.StatusServiceUnavailable, 1, "1")
	dest := filepath.Join(t.TempDir(), "pkg.deb")

	start := time.Now()
	if err := downloadWithRetry(context.Background(), network.GetSecureHTTPClient(), server.URL+"/pkg.deb", dest, 0); err != nil {
		t.Fatalf("downloadWithRetry: %v", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("retried after %s, want at least the 1s Retry-After", elapsed)
	}
}

// Gateway and rate-limit statuses earn the budget a 503 does; a plain server
// error keeps the default.
func TestDownloadWithRetry_AttemptBudgetByStatus(t *testing.T) {
	for _, tt := range []struct {
		status int
		want   int32
	}{
		{http.StatusTooManyRequests, maxUnavailableAttempts},
		{http.StatusBadGateway, maxUnavailableAttempts},
		{http.StatusGatewayTimeout, maxUnavailableAttempts},
		{http.StatusInternalServerError, maxDownloadAttempts},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			server, count := serveUnavailable(t, tt.status, 1000, "")
			dest := filepath.Join(t.TempDir(), "pkg.deb")

			if err := downloadWithRetry(context.Background(), network.GetSecureHTTPClient(), server.URL+"/pkg.deb", dest, 0); err == nil {
				t.Fatal("downloadWithRetry succeeded against a server that always fails")
			}
			if got := atomic.LoadInt32(count); got != tt.want {
				t.Errorf("requests = %d, want %d", got, tt.want)
			}
		})
	}
}

// A header timeout already waited the client's full ResponseHeaderTimeout
// (minutes for package downloads), so it keeps the default budget instead of
// stretching a genuine hang over the long 503 schedule.
func TestDownloadWithRetry_HeaderTimeoutKeepsDefaultBudget(t *testing.T) {
	var count int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(transport.CloseIdleConnections)
	dest := filepath.Join(t.TempDir(), "pkg.deb")

	err := downloadWithRetry(context.Background(), &http.Client{Transport: transport}, server.URL+"/pkg.deb", dest, 0)
	if err == nil || !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Fatalf("err = %v, want a header timeout", err)
	}
	if got := atomic.LoadInt32(&count); got != maxDownloadAttempts {
		t.Errorf("requests = %d, want %d", got, maxDownloadAttempts)
	}
}

// The budget follows the latest failure: a 503 followed by header timeouts gets
// the default three attempts, not the six the 503 alone would have earned.
func TestDownloadWithRetry_BudgetFollowsLatestFailure(t *testing.T) {
	var count int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&count, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(transport.CloseIdleConnections)
	dest := filepath.Join(t.TempDir(), "pkg.deb")

	err := downloadWithRetry(context.Background(), &http.Client{Transport: transport}, server.URL+"/pkg.deb", dest, 0)
	if err == nil || !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Fatalf("err = %v, want a header timeout", err)
	}
	if got := atomic.LoadInt32(&count); got != maxDownloadAttempts {
		t.Errorf("requests = %d, want %d", got, maxDownloadAttempts)
	}
}
