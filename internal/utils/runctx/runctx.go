// Package runctx wires the build's cancellation-cleanup coordinator into
// callers that acquire host-visible resources (mounts, loop devices) during
// an ICT build. Resources register a teardown when they're acquired; on
// SIGINT/SIGTERM the driving goroutine in build.go invokes Run, which
// executes registrations in LIFO order under a fresh per-entry timeout so
// cleanup itself isn't cancelled by the signal that fired.
//
// The coordinator lives on a package-scoped pointer set by executeBuild and
// cleared on return. Callers outside a build (unit tests, validate/inspect/
// compare commands) see Get() == nil and simply skip registration — their
// normal defer-based teardown paths are unchanged.
package runctx

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CleanupFunc is a per-resource teardown callback. It receives a fresh
// per-entry context (see Run) so it can wire its own shell/exec calls to a
// bounded budget. Return an error to surface it in the residual list.
type CleanupFunc func(ctx context.Context) error

// PerEntryTimeout caps how long a single teardown may run before the
// coordinator moves on. 30s comfortably covers a full chroot unmount
// escalation (4 strategies × ~5s per mount × ~7 mounts) or a losetup -d
// on ~5 partitions. It's a var (not a const) so tests can shorten the
// window without waiting the full production budget.
var PerEntryTimeout = 30 * time.Second

// entry holds a registered cleanup callback and the label used in residual
// reporting. Each entry carries a unique id assigned at Register time; the
// unregister closure removes an entry by scanning c.entries under c.mu.
// Once Run fires it flips c.fired and clears c.entries, so a late unregister
// becomes a no-op naturally — the id scan finds nothing.
type entry struct {
	id    uint64
	label string
	fn    CleanupFunc
}

// Coordinator collects cleanup registrations and runs them on demand. Safe
// for concurrent registration; Run executes serially in LIFO order.
type Coordinator struct {
	mu      sync.Mutex
	entries []entry
	nextID  uint64
	// fired is set once by Run to prevent late Register calls from silently
	// disappearing after the coordinator has already fired. Protected by mu:
	// Register's fired-check and entries-append must be atomic together, or
	// a Register call that read fired==false before Run's snapshot could
	// append its entry to c.entries after Run cleared them — the entry
	// would sit forever in the slice and never run.
	fired bool
}

// New constructs an empty coordinator.
func New() *Coordinator {
	return &Coordinator{}
}

// Register attaches fn to the coordinator under label and returns an
// unregister closure the caller can invoke from a happy-path defer to
// avoid double-teardown. If the coordinator has already run (Fired) the
// registration is dropped and unregister is a no-op — cleanup for resources
// acquired after a cancel is the acquirer's responsibility.
func (c *Coordinator) Register(label string, fn CleanupFunc) (unregister func()) {
	if c == nil || fn == nil {
		return func() {}
	}

	c.mu.Lock()
	if c.fired {
		c.mu.Unlock()
		return func() {}
	}
	c.nextID++
	id := c.nextID
	c.entries = append(c.entries, entry{id: id, label: label, fn: fn})
	c.mu.Unlock()

	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, e := range c.entries {
			if e.id == id {
				c.entries = append(c.entries[:i], c.entries[i+1:]...)
				return
			}
		}
	}
}

// Run executes every registered cleanup in LIFO order under a fresh
// context.WithTimeout(background, PerEntryTimeout) per entry. The
// caller-supplied ctx is currently unused; it is kept in the signature
// for symmetry with idiomatic Go APIs and future use. Cancellation of
// the caller ctx does NOT cancel the cleanup — each per-entry ctx is
// derived from context.Background so cleanup is not defeated by the
// same signal that fired it. Returns a human-readable list of
// "label: err" strings for entries that failed — empty on complete
// success.
//
// Run is idempotent — subsequent calls return an empty residual list.
func (c *Coordinator) Run(ctx context.Context) []string {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	if c.fired {
		c.mu.Unlock()
		return nil
	}
	c.fired = true
	// Copy so we can release the lock before invoking user callbacks.
	pending := make([]entry, len(c.entries))
	copy(pending, c.entries)
	c.entries = nil
	c.mu.Unlock()

	var residual []string
	// LIFO: newer resources depend on older ones, so tear down in reverse order.
	for i := len(pending) - 1; i >= 0; i-- {
		e := pending[i]
		entryCtx, cancel := context.WithTimeout(context.Background(), PerEntryTimeout)
		err := runOne(entryCtx, e)
		cancel()
		if err != nil {
			residual = append(residual, fmt.Sprintf("%s: %v", e.label, err))
		}
	}
	return residual
}

// runOne invokes fn under a small recover so a panic in one teardown doesn't
// abort the rest of the LIFO chain. A panicking cleanup is reported as an
// error and drops through to the next entry.
func runOne(ctx context.Context, e entry) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cleanup panicked: %v", r)
		}
	}()
	return e.fn(ctx)
}

// Len reports how many entries are currently registered. Primarily for tests
// and for logging "N cleanups pending" at run boundaries.
func (c *Coordinator) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Package-scoped coordinator handle. Wrapped in a struct so atomic.Value sees
// a single concrete type across Set(nil) and Set(coord) calls, mirroring the
// pattern used by shell.SetContext.
type coordHolder struct{ c *Coordinator }

var current atomic.Value // holds coordHolder

// Set installs c as the coordinator that subsequent Register calls target.
// Passing nil clears the binding — equivalent to Clear.
func Set(c *Coordinator) {
	current.Store(coordHolder{c: c})
}

// Clear removes the coordinator binding.
func Clear() {
	current.Store(coordHolder{})
}

// Get returns the currently bound coordinator, or nil if no build is active.
// Register-side callers should always guard on Get() != nil so this package
// is safe to import from code paths that run outside a build.
func Get() *Coordinator {
	if v := current.Load(); v != nil {
		return v.(coordHolder).c
	}
	return nil
}

// ctxHolder wraps a context.Context so atomic.Value sees a single concrete
// type across the different concrete ctx implementations (*cancelCtx,
// *timerCtx, *emptyCtx, *valueCtx). Mirrors the shell package's approach.
type ctxHolder struct{ ctx context.Context }

var currentCtx atomic.Value // holds ctxHolder

// SetContext binds ctx as the ambient run-scoped context that pure-Go code
// paths (which cannot observe subprocess cancellation from the shell layer)
// can consult via Context(). Returns a restore closure so callers can revert
// on defer — matches shell.SetContext ergonomics.
//
// This is separate from the shell.SetContext binding by design: pkgfetcher
// and similar pure-Go paths need ctx for cooperative HTTP cancellation, and
// build.go installs both bindings from the same source ctx in Phase 4.
//
// Concurrency: the returned restore closure captures the previous binding at
// call time, so nested SetContext/restore pairs must be strictly LIFO on a
// single goroutine. The current build path (executeBuild → PostProcess
// wrapper → cleanup coordinator callbacks) satisfies this — only the main
// build goroutine invokes SetContext at any given time. If a future in-process
// caller invokes SetContext from multiple goroutines concurrently (e.g. an
// HTTP handler that dispatches a build in-process), the restore-chain will
// clobber and this needs to be replaced with a ctx-holder passed explicitly
// through the call graph instead of stored in an atomic global.
func SetContext(ctx context.Context) (restore func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	prev := currentCtx.Load()
	currentCtx.Store(ctxHolder{ctx: ctx})
	return func() {
		if prev == nil {
			currentCtx.Store(ctxHolder{ctx: context.Background()})
			return
		}
		currentCtx.Store(prev)
	}
}

// Context returns the currently bound run-scoped context, or context.Background
// if none is set. Callers outside a build (unit tests, non-build subcommands)
// receive Background and behave as they did before.
func Context() context.Context {
	if v := currentCtx.Load(); v != nil {
		return v.(ctxHolder).ctx
	}
	return context.Background()
}
