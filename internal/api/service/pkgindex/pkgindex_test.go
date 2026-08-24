// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingDebServer serves one Packages.gz for any suite/component/arch and
// counts the requests that reached it, which is how the caching and
// single-flight behaviour is observed.
type countingDebServer struct {
	*httptest.Server
	hits atomic.Int64
}

// newCountingDebServer starts the server. When release is non-nil each handler
// blocks until it is closed; it is passed in rather than assigned afterwards so
// the handler never reads a field the test is still writing.
func newCountingDebServer(t *testing.T, release chan struct{}) *countingDebServer {
	t.Helper()
	s := &countingDebServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if release != nil {
			<-release
		}
		if !hasSuffix(r.URL.Path, "Packages.gz") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if _, err := w.Write(gzipped(t, twoStanzas)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func debRepo(url, codename string) Repo {
	return Repo{Type: TypeDeb, URL: url, Codename: codename, Component: "main", Arch: "amd64"}
}

func TestLookupRejectsUnknownType(t *testing.T) {
	t.Parallel()
	c := New(Config{})
	_, err := c.Lookup(context.Background(), Repo{Type: "snap", URL: "http://example.invalid"})
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("got %v, want ErrUnsupportedType", err)
	}
}

func TestLookupServesSecondCallFromCache(t *testing.T) {
	srv := newCountingDebServer(t, nil)
	c := New(Config{Client: srv.Client()})
	r := debRepo(srv.URL, "noble")

	first, err := c.Lookup(context.Background(), r)
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	second, err := c.Lookup(context.Background(), r)
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("got %d then %d entries, want 2 each", len(first), len(second))
	}
	if got := srv.hits.Load(); got != 1 {
		t.Errorf("upstream was hit %d times, want 1", got)
	}
}

func TestLookupRefetchesAfterTTL(t *testing.T) {
	srv := newCountingDebServer(t, nil)
	c := New(Config{Client: srv.Client(), TTL: time.Hour})

	// The clock is injected rather than slept on, so the TTL is exercised
	// without making the test take an hour.
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	c.now = func() time.Time { return time.Unix(0, now.Load()) }

	r := debRepo(srv.URL, "noble")
	if _, err := c.Lookup(context.Background(), r); err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	// Just inside the TTL: still cached.
	now.Add(int64(59 * time.Minute))
	if _, err := c.Lookup(context.Background(), r); err != nil {
		t.Fatalf("lookup inside the TTL: %v", err)
	}
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("upstream hit %d times inside the TTL, want 1", got)
	}
	// Past it: refetched.
	now.Add(int64(2 * time.Minute))
	if _, err := c.Lookup(context.Background(), r); err != nil {
		t.Fatalf("lookup past the TTL: %v", err)
	}
	if got := srv.hits.Load(); got != 2 {
		t.Errorf("upstream hit %d times past the TTL, want 2", got)
	}
}

func TestLookupSingleFlights(t *testing.T) {
	release := make(chan struct{})
	srv := newCountingDebServer(t, release)
	c := New(Config{Client: srv.Client()})
	r := debRepo(srv.URL, "noble")

	// Every goroutine asks for the same index while the first fetch is still
	// blocked in the handler. Without single-flight this is 20 upstream reads
	// of a multi-megabyte file.
	const callers = 20
	var wg sync.WaitGroup
	errs := make([]error, callers)
	counts := make([]int, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries, err := c.Lookup(context.Background(), r)
			errs[i], counts[i] = err, len(entries)
		}()
	}

	// Let the callers pile up behind the in-flight fetch before releasing it.
	waitFor(t, func() bool { return srv.hits.Load() >= 1 })
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if counts[i] != 2 {
			t.Errorf("caller %d got %d entries, want 2", i, counts[i])
		}
	}
	if got := srv.hits.Load(); got != 1 {
		t.Errorf("upstream was hit %d times, want exactly 1", got)
	}
}

func TestLookupWaiterHonoursItsOwnContext(t *testing.T) {
	release := make(chan struct{})
	srv := newCountingDebServer(t, release)
	// Closed before the server's own cleanup runs, so Close is not left waiting
	// on a handler that is still blocked.
	defer close(release)
	c := New(Config{Client: srv.Client()})
	r := debRepo(srv.URL, "noble")

	// Leader: starts the fetch and stays blocked in the handler.
	go func() {
		_, _ = c.Lookup(context.Background(), r)
	}()
	waitFor(t, func() bool { return srv.hits.Load() >= 1 })

	// A waiter that gives up must return promptly rather than being held by
	// someone else's slow fetch.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.Lookup(ctx, r); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waiter took %s to give up", elapsed)
	}
}

func TestLookupEnforcesFetchTimeout(t *testing.T) {
	// A mirror that accepts the connection and then stalls must not pin the
	// request: the project HTTP client bounds only the response header wait.
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-stall
	}))
	// Defers run last-in-first-out, so the stall is released before Close is
	// called. The other order deadlocks: the client's deadline does not unblock
	// the handler, and Close waits for it.
	defer srv.Close()
	defer close(stall)

	c := New(Config{Client: srv.Client(), FetchTimeout: 150 * time.Millisecond})
	start := time.Now()
	_, err := c.Lookup(context.Background(), debRepo(srv.URL, "noble"))
	if err == nil {
		t.Fatal("want an error from the stalled fetch")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("fetch took %s despite a 150ms timeout", elapsed)
	}
}

// failingDebServer serves a valid index once fail is cleared, counting every
// request, so a test can tell a cached failure from a re-dialled one.
func failingDebServer(t *testing.T, fail *atomic.Bool, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() || !hasSuffix(r.URL.Path, "Packages.gz") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(gzipped(t, twoStanzas)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLookupCachesFailuresForFailureTTL(t *testing.T) {
	// An unreachable repository must not be redialled on every request: a
	// search fans out across the whole catalog, so one dead mirror would
	// otherwise cost its full dial timeout on every keystroke.
	var fail atomic.Bool
	fail.Store(true)
	var hits atomic.Int64
	srv := failingDebServer(t, &fail, &hits)

	c := New(Config{Client: srv.Client(), FailureTTL: time.Minute})
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	c.now = func() time.Time { return time.Unix(0, now.Load()) }

	r := debRepo(srv.URL, "noble")
	if _, err := c.Lookup(context.Background(), r); err == nil {
		t.Fatal("want an error while the server is failing")
	}
	first := hits.Load()

	// Inside the failure TTL: served from the negative cache, still an error,
	// but without touching the upstream again.
	if _, err := c.Lookup(context.Background(), r); err == nil {
		t.Fatal("want the remembered error inside the failure TTL")
	}
	if got := hits.Load(); got != first {
		t.Errorf("upstream hit %d times inside the failure TTL, want %d", got, first)
	}

	// Past it: retried, and the recovered repository is served normally.
	fail.Store(false)
	now.Add(int64(2 * time.Minute))
	got, err := c.Lookup(context.Background(), r)
	if err != nil {
		t.Fatalf("lookup past the failure TTL: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d entries, want 2", len(got))
	}
	if hits.Load() <= first {
		t.Errorf("upstream hit %d times; the failure was never retried", hits.Load())
	}
}

func TestLookupFetchSurvivesItsInitiator(t *testing.T) {
	// The fetch is detached from whichever caller started it, so abandoning
	// that request must not hand context.Canceled to a concurrent waiter on the
	// same index — and the fetch should still warm the cache.
	started := make(chan struct{})
	release := make(chan struct{})
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			close(started)
			<-release
		}
		if _, err := w.Write(gzipped(t, twoStanzas)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	c := New(Config{Client: srv.Client()})
	r := debRepo(srv.URL, "noble")

	// Caller one starts the fetch, then gives up while it is still in flight.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.Lookup(ctx, r)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("initiator: want context.Canceled, got %v", err)
	}

	// Caller two, on a live context, gets the real result rather than the
	// departed initiator's cancellation.
	close(release)
	got, err := c.Lookup(context.Background(), r)
	if err != nil {
		t.Fatalf("waiter after the initiator left: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d entries, want 2", len(got))
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("upstream hit %d times; the abandoned fetch did not warm the cache", n)
	}
	// The caller that gave up must not have been recorded as the repository's
	// failure — that would blank it out for the whole FailureTTL.
	c.mu.Lock()
	n := len(c.failures)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("an abandoned caller left %d negative-cache entries, want 0", n)
	}
}

func TestLookupEvictsLeastRecentlyUsed(t *testing.T) {
	srv := newCountingDebServer(t, nil)
	c := New(Config{Client: srv.Client(), MaxRepos: 2})
	a, b, d := debRepo(srv.URL, "noble"), debRepo(srv.URL, "jammy"), debRepo(srv.URL, "trixie")

	for _, r := range []Repo{a, b} {
		if _, err := c.Lookup(context.Background(), r); err != nil {
			t.Fatalf("warm %s: %v", r.Codename, err)
		}
	}
	// Touch a so b becomes the least recently used, then overflow the cap.
	if _, err := c.Lookup(context.Background(), a); err != nil {
		t.Fatalf("touch a: %v", err)
	}
	if _, err := c.Lookup(context.Background(), d); err != nil {
		t.Fatalf("lookup d: %v", err)
	}
	if got := srv.hits.Load(); got != 3 {
		t.Fatalf("upstream hit %d times warming three repos, want 3", got)
	}

	// a survives because it was touched; b was evicted and must refetch.
	if _, err := c.Lookup(context.Background(), a); err != nil {
		t.Fatalf("lookup a after eviction: %v", err)
	}
	if got := srv.hits.Load(); got != 3 {
		t.Errorf("a was refetched (%d hits); the touched entry should have survived", got)
	}
	if _, err := c.Lookup(context.Background(), b); err != nil {
		t.Fatalf("lookup b after eviction: %v", err)
	}
	if got := srv.hits.Load(); got != 4 {
		t.Errorf("upstream hit %d times, want 4 — b should have been evicted", got)
	}
}

func TestLookupEvictsOnTheEntryBudget(t *testing.T) {
	srv := newCountingDebServer(t, nil)
	// Well under MaxRepos, so only the package budget can force an eviction.
	// Each fixture index holds 2 entries, so a third overflows a budget of 5.
	c := New(Config{Client: srv.Client(), MaxRepos: 10, MaxEntries: 5})
	a, b, d := debRepo(srv.URL, "noble"), debRepo(srv.URL, "jammy"), debRepo(srv.URL, "trixie")

	for _, r := range []Repo{a, b, d} {
		if _, err := c.Lookup(context.Background(), r); err != nil {
			t.Fatalf("warm %s: %v", r.Codename, err)
		}
	}
	if got := srv.hits.Load(); got != 3 {
		t.Fatalf("upstream hit %d times warming three repos, want 3", got)
	}
	// a was least recently used when the budget was exceeded, so it must refetch
	// while b and d stay cached.
	if _, err := c.Lookup(context.Background(), a); err != nil {
		t.Fatalf("lookup a: %v", err)
	}
	if got := srv.hits.Load(); got != 4 {
		t.Errorf("upstream hit %d times, want 4 — a should have been evicted", got)
	}
	if _, err := c.Lookup(context.Background(), d); err != nil {
		t.Fatalf("lookup d: %v", err)
	}
	if got := srv.hits.Load(); got != 4 {
		t.Errorf("d was refetched (%d hits); it should still be cached", got)
	}
}

func TestLookupKeepsAnIndexLargerThanTheBudget(t *testing.T) {
	srv := newCountingDebServer(t, nil)
	// The budget is smaller than the single index. Evicting it would mean
	// refetching a multi-megabyte file on every request, so it is retained.
	c := New(Config{Client: srv.Client(), MaxEntries: 1})
	r := debRepo(srv.URL, "noble")

	for i := range 2 {
		if _, err := c.Lookup(context.Background(), r); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	if got := srv.hits.Load(); got != 1 {
		t.Errorf("upstream hit %d times, want 1 — the sole index should be kept", got)
	}
}

func TestRepoKeyDistinguishesEveryField(t *testing.T) {
	t.Parallel()
	base := Repo{Type: TypeDeb, URL: "http://r", Codename: "noble", Component: "main", Arch: "amd64"}
	// A key that collapsed any of these would serve one architecture's or
	// component's packages under another's.
	variants := map[string]Repo{
		"type":      {Type: TypeRPM, URL: "http://r", Codename: "noble", Component: "main", Arch: "amd64"},
		"url":       {Type: TypeDeb, URL: "http://s", Codename: "noble", Component: "main", Arch: "amd64"},
		"codename":  {Type: TypeDeb, URL: "http://r", Codename: "jammy", Component: "main", Arch: "amd64"},
		"component": {Type: TypeDeb, URL: "http://r", Codename: "noble", Component: "universe", Arch: "amd64"},
		"arch":      {Type: TypeDeb, URL: "http://r", Codename: "noble", Component: "main", Arch: "arm64"},
	}
	for field, v := range variants {
		if v.key() == base.key() {
			t.Errorf("%s does not affect the cache key: %q", field, v.key())
		}
	}
}

// waitFor blocks until cond holds, failing the test if it never does.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}
