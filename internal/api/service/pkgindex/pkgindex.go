// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package pkgindex reads package indexes from apt and dnf repositories so the
// web UI can list and search what a repository offers.
//
// It deliberately does not reuse debutils.ParseRepositoryMetadata or
// rpmutils.ParseRepositoryMetadata, the parsers the build path uses. Those are
// driven by package-level mutable state (debutils.RepoCfg, GzHref,
// Architecture), take their deadline from the process-wide runctx.Context()
// rather than the caller's, and write their metadata caches into the build
// tree, which is root-owned whenever a build ran under sudo. None of that is
// safe to reach from an HTTP handler serving concurrent requests. They also
// produce ospackage.PackageInfo, carrying dependency, checksum and file-list
// data a picker never shows: an Ubuntu main index is ~69,000 stanzas, which is
// tens of megabytes as PackageInfo and a few as Entry.
//
// So this package parses the same wire formats independently, keeping only the
// four fields the picker renders, and holds results in memory rather than on
// disk. Nothing here writes to the filesystem.
package pkgindex

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
)

var log = logger.Logger()

// Repo identifies a single repository index to read. Codename and Component
// apply to deb only; an rpm repository publishes one index per base URL, and
// its URL may carry an {arch} placeholder that Lookup substitutes.
type Repo struct {
	Type      string // "deb" or "rpm"
	URL       string
	Codename  string
	Component string
	Arch      string
}

// Repo types accepted by Lookup.
const (
	TypeDeb = "deb"
	TypeRPM = "rpm"
)

// Entry is one package, reduced to what the picker displays.
type Entry struct {
	Name        string
	Version     string
	Description string // short synopsis; the first Description/summary line only
	Arch        string
}

// ErrUnsupportedType is returned for a Repo whose Type is neither deb nor rpm.
var ErrUnsupportedType = errors.New("unsupported repository type")

// Defaults for a zero-valued Config.
const (
	DefaultTTL          = 6 * time.Hour
	DefaultFetchTimeout = 90 * time.Second
	DefaultMaxRepos     = 24
	// DefaultMaxEntries bounds retained packages rather than retained indexes,
	// because index sizes differ by two orders of magnitude: Ubuntu noble
	// amd64 main holds 6,099 packages while universe holds 64,755. Measured at
	// 218 bytes per Entry, this budget is roughly 130 MB.
	DefaultMaxEntries = 600_000
)

// Config tunes a Cache. A zero Config is usable; each field falls back to its
// Default above.
type Config struct {
	// TTL is how long a parsed index is served before it is refetched.
	TTL time.Duration
	// FetchTimeout bounds a single index fetch. It exists because the project
	// HTTP client sets only a response-header timeout and leaves body transfer
	// unbounded (see network/securehttp.go), so without it a stalled mirror
	// would pin a request forever.
	FetchTimeout time.Duration
	// MaxRepos caps how many parsed indexes are retained. The least recently
	// used is dropped past the cap, so a session touching many repositories
	// cannot grow the heap without bound.
	MaxRepos int
	// MaxEntries caps the total packages retained across all indexes. MaxRepos
	// alone is not a memory bound: 24 universe-sized indexes would be ~324 MB.
	// The most recently used index is always kept, even if it alone exceeds
	// this, so an oversized repository is still usable rather than refetched on
	// every request.
	MaxEntries int
	// Client overrides the HTTP client, for tests.
	Client *http.Client
}

func (c Config) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return DefaultTTL
}

func (c Config) fetchTimeout() time.Duration {
	if c.FetchTimeout > 0 {
		return c.FetchTimeout
	}
	return DefaultFetchTimeout
}

func (c Config) maxRepos() int {
	if c.MaxRepos > 0 {
		return c.MaxRepos
	}
	return DefaultMaxRepos
}

func (c Config) maxEntries() int {
	if c.MaxEntries > 0 {
		return c.MaxEntries
	}
	return DefaultMaxEntries
}

// Cache serves parsed indexes, refetching one only when its TTL has passed.
// It is safe for concurrent use.
type Cache struct {
	cfg    Config
	client *http.Client

	// now is time.Now indirected so tests can advance the clock past a TTL
	// without sleeping.
	now func() time.Time

	mu       sync.Mutex
	entries  map[string]*cached
	order    []string // keys, least recently used first
	inflight map[string]*fetch
}

type cached struct {
	entries []Entry
	fetched time.Time
}

// fetch is one in-progress index read. Callers that arrive while it is running
// wait on done rather than issuing a second request for the same index.
type fetch struct {
	done    chan struct{}
	entries []Entry
	err     error
}

// New returns a Cache. It performs no I/O.
func New(cfg Config) *Cache {
	client := cfg.Client
	if client == nil {
		client = network.NewSecureHTTPClient()
	}
	return &Cache{
		cfg:      cfg,
		client:   client,
		now:      time.Now,
		entries:  make(map[string]*cached),
		inflight: make(map[string]*fetch),
	}
}

// key identifies a repository index. Arch is included because a deb repository
// serves a separate index per architecture, and an rpm URL is arch-templated.
func (r Repo) key() string {
	return strings.Join([]string{r.Type, r.URL, r.Codename, r.Component, r.Arch}, "|")
}

// Lookup returns the packages r offers, reading the index over the network on a
// miss or once the cached copy has aged past the TTL.
//
// Concurrent lookups of the same repository share one fetch: the first caller
// performs it and the rest wait for that result. A waiter still honours its own
// ctx, so a client that gives up does not block on someone else's slow fetch.
func (c *Cache) Lookup(ctx context.Context, r Repo) ([]Entry, error) {
	if r.Type != TypeDeb && r.Type != TypeRPM {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedType, r.Type)
	}
	key := r.key()

	c.mu.Lock()
	if hit, ok := c.entries[key]; ok && c.now().Sub(hit.fetched) < c.cfg.ttl() {
		c.touchLocked(key)
		c.mu.Unlock()
		return hit.entries, nil
	}
	if f, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		return f.wait(ctx)
	}
	f := &fetch{done: make(chan struct{})}
	c.inflight[key] = f
	c.mu.Unlock()

	f.entries, f.err = c.load(ctx, r)
	close(f.done)

	c.mu.Lock()
	delete(c.inflight, key)
	if f.err == nil {
		c.entries[key] = &cached{entries: f.entries, fetched: c.now()}
		c.touchLocked(key)
		c.evictLocked()
	}
	c.mu.Unlock()

	return f.entries, f.err
}

// wait blocks until the shared fetch finishes or ctx is done, whichever first.
func (f *fetch) wait(ctx context.Context) ([]Entry, error) {
	select {
	case <-f.done:
		return f.entries, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// load fetches and parses one index under its own deadline.
func (c *Cache) load(ctx context.Context, r Repo) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.fetchTimeout())
	defer cancel()

	start := c.now()
	var entries []Entry
	var err error
	if r.Type == TypeDeb {
		entries, err = fetchDeb(ctx, c.client, r)
	} else {
		entries, err = fetchRPM(ctx, c.client, r)
	}
	if err != nil {
		return nil, err
	}
	log.Debugf("pkgindex: %s index %s has %d packages (%s)",
		r.Type, r.URL, len(entries), c.now().Sub(start).Round(time.Millisecond))
	return entries, nil
}

// touchLocked marks key as most recently used. Callers hold c.mu.
func (c *Cache) touchLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}

// evictLocked drops least-recently-used indexes until both the index count and
// the total package count are within their caps. The last remaining index is
// never evicted, so a single repository larger than the entry budget is still
// served from cache instead of being refetched every time. Callers hold c.mu.
func (c *Cache) evictLocked() {
	total := 0
	for _, e := range c.entries {
		total += len(e.entries)
	}
	for len(c.order) > 1 && (len(c.order) > c.cfg.maxRepos() || total > c.cfg.maxEntries()) {
		oldest := c.order[0]
		c.order = c.order[1:]
		if e, ok := c.entries[oldest]; ok {
			total -= len(e.entries)
			delete(c.entries, oldest)
		}
		log.Debugf("pkgindex: evicted %s; now %d indexes / %d packages cached",
			oldest, len(c.order), total)
	}
}
