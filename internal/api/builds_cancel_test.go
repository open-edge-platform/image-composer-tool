// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// --- exit-code classification ---

// signaledErr runs a child that terminates on the given signal and returns the
// resulting *exec.ExitError, so classifyExit sees a real WaitStatus. `sh -c
// 'kill -N $$'` makes the shell signal itself.
func signaledErr(t *testing.T, sig string) error {
	t.Helper()
	err := exec.Command("sh", "-c", "kill -"+sig+" $$").Run()
	if err == nil {
		t.Fatalf("expected the child to be terminated by %s", sig)
	}
	return err
}

func TestClassifyExit(t *testing.T) {
	// A non-zero exit that is NOT a cancel is always a failure, regardless of code.
	failErr := exec.Command("sh", "-c", "exit 2").Run()
	if got := classifyExit(failErr, false); got != statusFailed {
		t.Fatalf("non-cancel exit 2: got %q, want failed", got)
	}
	if got := classifyExit(failErr, true); got != statusFailed {
		t.Fatalf("cancel-requested exit 2 (unrelated failure): got %q, want failed", got)
	}

	// Signal-code exits under a requested cancel map to cancelled.
	for _, code := range []int{130, 143, 137} {
		err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
		if got := classifyExit(err, true); got != statusCancelled {
			t.Fatalf("cancel-requested exit %d: got %q, want cancelled", code, got)
		}
		// Same code without a cancel request is a failure.
		if got := classifyExit(err, false); got != statusFailed {
			t.Fatalf("no-cancel exit %d: got %q, want failed", code, got)
		}
	}

	// A child killed by a signal (Signaled(), no exit code) under a cancel maps to
	// cancelled — this is the path where sudo/ICT is TERM/KILLed rather than
	// exiting with a translated code.
	sigErr := signaledErr(t, "TERM")
	if got := classifyExit(sigErr, true); got != statusCancelled {
		t.Fatalf("cancel-requested SIGTERM-killed child: got %q, want cancelled", got)
	}
	if got := classifyExit(sigErr, false); got != statusFailed {
		t.Fatalf("no-cancel SIGTERM-killed child: got %q, want failed", got)
	}
}

// --- residual-cleanup log scanning ---

// The lines below mirror what ICT actually logs from the backstop-cleanup defer
// in cmd/image-composer-tool/build.go: an ERROR header with the count, one
// indented "  - <label>: <err>" item per leftover resource (labels come from the
// runctx registrations — "chroot:<root>" and "loop:<dev>"), then a WARN hint.
// The parser is anchored on that shape, so the fixture must keep it.
func TestScanResidualCleanup(t *testing.T) {
	clean := []string{
		"2026-07-24\tINFO\tbuild.go:1\tstarting build",
		"2026-07-24\tINFO\tbuild.go:2\tcleanup complete",
	}
	if got := scanResidualCleanup(clean); got != "" {
		t.Fatalf("clean teardown flagged residual: %q", got)
	}

	dirty := []string{
		"2026-07-24\tINFO\tbuild.go:1\tstarting build",
		"2026-07-24\tWARN\tbuild.go:2\tbuild cancelled by signal (context canceled), running backstop cleanup",
		"2026-07-24\tERROR\tbuild.go:3\tresidual cleanup issues (2):",
		"2026-07-24\tERROR\tbuild.go:4\t  - chroot:/var/tmp/ict/builds/abc/rootfs: unmounting /proc: device or resource busy",
		"2026-07-24\tERROR\tbuild.go:5\t  - loop:/dev/loop3: detaching loop device: device or resource busy",
		"2026-07-24\tWARN\tbuild.go:6\tsome resources may still be held; consider running 'mount | grep /var/tmp/ict/builds/abc' and 'losetup -l' to identify leftovers",
		"2026-07-24\tINFO\tbuild.go:7\texiting",
	}
	got := scanResidualCleanup(dirty)
	if got == "" {
		t.Fatal("residual cleanup report not detected")
	}
	// Every part of the report must survive: the header (carries the count), both
	// items (a leftover *mount* item has no distinctive keyword, which is why the
	// parser is block-based), and the remediation hint.
	for _, want := range []string{
		"residual cleanup issues (2):",
		"- chroot:/var/tmp/ict/builds/abc/rootfs: unmounting /proc",
		"- loop:/dev/loop3: detaching loop device",
		"mount | grep /var/tmp/ict/builds/abc",
		"losetup -l",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("residual detail missing %q:\n%s", want, got)
		}
	}
	// Lines outside the report block must not leak in, and logger prefixes are
	// stripped.
	for _, unwanted := range []string{"starting build", "exiting", "INFO", "ERROR\t"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("residual detail should not contain %q:\n%s", unwanted, got)
		}
	}
}

// A report whose items never made it into the log buffer (truncation, or the
// process dying mid-report) must still yield a non-empty detail: the header
// alone tells the user something was left behind.
func TestScanResidualCleanupHeaderOnly(t *testing.T) {
	got := scanResidualCleanup([]string{
		"2026-07-24\tERROR\tbuild.go:3\tresidual cleanup issues (1):",
	})
	if !strings.Contains(got, "residual cleanup issues (1):") {
		t.Fatalf("header-only report lost: %q", got)
	}
}

// TestStripLogPrefix verifies the logger's fixed 3-field prefix
// (<ts>\t<LEVEL>\t<source>\t) is removed while tabs inside the message are
// preserved — a message with an embedded tab must not be truncated.
func TestStripLogPrefix(t *testing.T) {
	if got := stripLogPrefix("12:00:00\tERROR\tcleanup.go:9\tleftover loop device"); got != "leftover loop device" {
		t.Fatalf("prefix not stripped: %q", got)
	}
	// A message that itself contains a tab keeps everything after the 3rd tab.
	line := "12:00:00\tERROR\tcleanup.go:9\tmount | grep foo\tand more"
	if got := stripLogPrefix(line); got != "mount | grep foo\tand more" {
		t.Fatalf("embedded tab truncated: %q", got)
	}
	// A non-logger line (fewer than 3 tabs) is returned unchanged.
	if got := stripLogPrefix("bare message"); got != "bare message" {
		t.Fatalf("non-prefixed line altered: %q", got)
	}
}

// --- build.beginCancel / setResidual ---

func TestBeginCancelTransitions(t *testing.T) {
	b := &build{ID: "b", status: statusRunning, pgid: 4242, done: make(chan struct{})}

	ok, pgid := b.beginCancel()
	if !ok || pgid != 4242 {
		t.Fatalf("first cancel: ok=%v pgid=%d, want true/4242", ok, pgid)
	}
	if s := b.snapshot(); s.status != statusCancelling {
		t.Fatalf("status after cancel = %q, want cancelling", s.status)
	}
	if !b.wasCancelRequested() {
		t.Fatal("cancelRequested not set")
	}

	// A second cancel on an already-cancelling build is rejected (idempotent).
	if ok, _ := b.beginCancel(); ok {
		t.Fatal("second cancel transitioned again; want false")
	}

	// A cancel on a terminal build is rejected.
	done := &build{ID: "d", status: statusFailed, done: make(chan struct{})}
	if ok, _ := done.beginCancel(); ok {
		t.Fatal("cancel on failed build transitioned; want false")
	}
}

// TestSetPgidCheckCancel covers the start-race window: a cancel can arrive after
// the build is tracked as running but before runBuild records the process-group
// id. beginCancel transitions to cancelling with pgid still 0; setPgidCheckCancel
// (called once the child is started) must record the real pgid and report that a
// cancel is pending so the caller signals the group it finally has.
func TestSetPgidCheckCancel(t *testing.T) {
	// No cancel pending: records the pgid, reports pending=false.
	b := &build{ID: "b", status: statusRunning, done: make(chan struct{})}
	if pgid, pending := b.setPgidCheckCancel(1234); pgid != 1234 || pending {
		t.Fatalf("no-cancel: pgid=%d pending=%v, want 1234/false", pgid, pending)
	}
	if s := b.snapshot(); s.status != statusRunning {
		t.Fatalf("no-cancel status = %q, want running (unchanged)", s.status)
	}

	// Cancel arrived during the window (status=cancelling, pgid still 0):
	// setPgidCheckCancel records the pgid and reports pending=true so runBuild
	// delivers the deferred signal.
	raced := &build{ID: "r", status: statusRunning, done: make(chan struct{})}
	if ok, pgid := raced.beginCancel(); !ok || pgid != 0 {
		t.Fatalf("beginCancel before pgid set: ok=%v pgid=%d, want true/0", ok, pgid)
	}
	pgid, pending := raced.setPgidCheckCancel(5678)
	if pgid != 5678 || !pending {
		t.Fatalf("raced cancel: pgid=%d pending=%v, want 5678/true", pgid, pending)
	}

	// A build that already reached a terminal state before the pgid was recorded
	// must not report a pending cancel (nothing to signal).
	term := &build{ID: "t", status: statusCancelled, cancelRequested: true, done: make(chan struct{})}
	if _, pending := term.setPgidCheckCancel(9999); pending {
		t.Fatal("terminal build reported a pending cancel; want false")
	}
}

// A cancel arriving before the child is spawned must be accepted: the build
// already holds the single-build slot, so refusing it would leave the user with
// no way to release it. beginCancel therefore also accepts not-started.
func TestBeginCancelBeforeStart(t *testing.T) {
	b := &build{ID: "pre", status: statusNotStarted, done: make(chan struct{})}
	ok, pgid := b.beginCancel()
	if !ok || pgid != 0 {
		t.Fatalf("cancel on not-started build: ok=%v pgid=%d, want true/0", ok, pgid)
	}
	if s := b.snapshot(); s.status != statusCancelling {
		t.Fatalf("status = %q, want cancelling", s.status)
	}
}

// setPgidCheckCancel doubles as the not-started → running promotion, so a build
// that was never cancelled reports running once its child is up.
func TestSetPgidPromotesNotStarted(t *testing.T) {
	b := &build{ID: "b", status: statusNotStarted, done: make(chan struct{})}
	if pgid, pending := b.setPgidCheckCancel(4321); pgid != 4321 || pending {
		t.Fatalf("pgid=%d pending=%v, want 4321/false", pgid, pending)
	}
	if s := b.snapshot(); s.status != statusRunning {
		t.Fatalf("status = %q, want running", s.status)
	}
}

// finish must be single-shot: the wait path and the cancel watchdog can both
// reach a build, and the first terminal classification has to win — otherwise a
// build that completed successfully could be relabelled cancelled (and the slot
// released twice).
func TestFinishSingleShot(t *testing.T) {
	b := &build{ID: "b", status: statusRunning, done: make(chan struct{})}
	if !b.finish(statusSuccess, []artifact{{Name: "img.raw"}}, "") {
		t.Fatal("first finish reported false; want true (it recorded the outcome)")
	}
	if b.finish(statusCancelled, nil, "watchdog gave up") {
		t.Fatal("second finish reported true; terminal state must be single-shot")
	}
	s := b.snapshot()
	if s.status != statusSuccess {
		t.Fatalf("status = %q, want success (first writer wins)", s.status)
	}
	if s.errMsg != "" || len(s.artifacts) != 1 {
		t.Fatalf("second finish mutated the record: errMsg=%q artifacts=%v", s.errMsg, s.artifacts)
	}
}

// closeDone must tolerate being called from both the wait path and the watchdog:
// a double close(b.done) would panic and take the server down.
func TestCloseDoneIdempotent(t *testing.T) {
	b := &build{ID: "b", done: make(chan struct{})}
	b.closeDone()
	b.closeDone() // must not panic
	select {
	case <-b.done:
	default:
		t.Fatal("done not closed")
	}
}

func TestSetResidualFirstWins(t *testing.T) {
	b := &build{ID: "b", done: make(chan struct{})}
	b.setResidual(residualCancellation, "kill failed")
	b.setResidual(residualCleanup, "leftover mount") // must not overwrite
	s := b.snapshot()
	if s.residual == nil || s.residual.Kind != residualCancellation {
		t.Fatalf("residual = %+v, want first (cancellation-failure) to win", s.residual)
	}
}

// --- cancel handler ---

func TestHandleCancelBuildNotFound(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/nope/cancel", nil)
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	s.handleCancelBuild(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleCancelBuildNotRunning(t *testing.T) {
	s := newTestServer(t)
	b := &build{ID: "done", status: statusSuccess, done: make(chan struct{})}
	s.tracker.add(b)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/done/cancel", nil)
	req.SetPathValue("id", "done")
	rec := httptest.NewRecorder()
	s.handleCancelBuild(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestHandleCancelBuildRunningTransitions(t *testing.T) {
	s := newTestServer(t) // Sudo=false → signalCancel uses syscall.Kill directly
	// A build whose "process group" is our own harmless pgid: we don't want the
	// cancel to actually kill the test process, so use a pgid that Kill will fail
	// on cleanly (a very large, non-existent pid). The handler still transitions
	// to cancelling and records a cancellation-failure when the signal fails.
	b := &build{ID: "run", status: statusRunning, pgid: 2 << 30, done: make(chan struct{})}
	s.tracker.add(b)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/run/cancel", nil)
	req.SetPathValue("id", "run")
	rec := httptest.NewRecorder()
	s.handleCancelBuild(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	snap := b.snapshot()
	if snap.status != statusCancelling {
		t.Fatalf("status after cancel = %q, want cancelling", snap.status)
	}
	// The bogus pgid can't be signalled, so a cancellation-failure is recorded.
	if snap.residual == nil || snap.residual.Kind != residualCancellation {
		t.Fatalf("residual = %+v, want cancellation-failure", snap.residual)
	}
}

// A build record that exists only on disk (persisted as running by a server that
// has since restarted) has no process group and no wait goroutine behind it.
// Cancelling it would mutate a throwaway struct and report a bogus 202, so the
// handler must 409 and point the user at the host.
func TestHandleCancelBuildOrphanedRecord(t *testing.T) {
	s := newTestServer(t)
	b := &build{
		ID:      "orphan",
		RootDir: filepath.Join(s.buildsRoot(), "orphan"),
		status:  statusRunning,
		done:    make(chan struct{}),
	}
	if err := os.MkdirAll(b.RootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := b.writeMeta(); err != nil {
		t.Fatal(err)
	}
	// Deliberately NOT added to the tracker: on-disk only, as after a restart.

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/orphan/cancel", nil)
	req.SetPathValue("id", "orphan")
	rec := httptest.NewRecorder()
	s.handleCancelBuild(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for an on-disk-only build", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no live process") {
		t.Fatalf("body should explain the orphan: %s", rec.Body.String())
	}
}

// A cancel whose signal fails must report the residual in the 202 body: the
// build stays in cancelling and no terminal SSE event may ever arrive, so this is
// the only prompt notification the UI gets.
func TestHandleCancelBuildReportsResidualInResponse(t *testing.T) {
	s := newTestServer(t)
	b := &build{ID: "run", status: statusRunning, pgid: 2 << 30, done: make(chan struct{})}
	s.tracker.add(b)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/run/cancel", nil)
	req.SetPathValue("id", "run")
	rec := httptest.NewRecorder()
	s.handleCancelBuild(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	var got cancelAccepted
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v (%s)", err, rec.Body.String())
	}
	if got.Residual == nil || got.Residual.Kind != residualCancellation {
		t.Fatalf("response residual = %+v, want cancellation-failure", got.Residual)
	}
	if !strings.Contains(got.Residual.Detail, "failed to signal") {
		t.Fatalf("residual detail lacks the underlying error: %q", got.Residual.Detail)
	}
}

// --- cancel watchdog ---

// A build that ignores the cancel must not hold the single-build slot forever.
// concludeStalledCancel is the watchdog's hard-deadline action: it marks the
// build cancelled with a cancellation-failure and frees the slot.
func TestConcludeStalledCancelReleasesSlot(t *testing.T) {
	s := newTestServer(t)
	b := &build{ID: "wedged", status: statusCancelling, cancelRequested: true, pgid: 2 << 30, done: make(chan struct{})}
	s.tracker.add(b)
	if ok, _ := s.tryAcquireBuildSlot(b.ID); !ok {
		t.Fatal("could not occupy the build slot")
	}

	s.concludeStalledCancel(b)

	snap := b.snapshot()
	if snap.status != statusCancelled {
		t.Fatalf("status = %q, want cancelled", snap.status)
	}
	if snap.residual == nil || snap.residual.Kind != residualCancellation {
		t.Fatalf("residual = %+v, want cancellation-failure", snap.residual)
	}
	if !strings.Contains(snap.errMsg, "did not exit") {
		t.Fatalf("errMsg should say the build never exited: %q", snap.errMsg)
	}
	select {
	case <-b.done:
	default:
		t.Fatal("done not closed; SSE subscribers would hang")
	}
	if ok, active := s.tryAcquireBuildSlot("next"); !ok {
		t.Fatalf("slot still held by %q; a wedged cancel must not block new composes", active)
	}
}

// If the child exits just after the hard deadline fires, the wait path's outcome
// must stand: finish is single-shot, so the watchdog neither relabels the build
// nor releases a slot it no longer owns.
func TestConcludeStalledCancelLosesRaceToWaitPath(t *testing.T) {
	s := newTestServer(t)
	b := &build{ID: "raced", status: statusCancelling, cancelRequested: true, done: make(chan struct{})}
	s.tracker.add(b)
	// The wait path got there first.
	if !b.finish(statusCancelled, nil, "") {
		t.Fatal("setup: first finish should have recorded")
	}
	// A new build has since claimed the slot.
	if ok, _ := s.tryAcquireBuildSlot("next"); !ok {
		t.Fatal("setup: could not claim the slot for the next build")
	}

	s.concludeStalledCancel(b)

	if snap := b.snapshot(); snap.errMsg != "" {
		t.Fatalf("errMsg = %q, want empty (the wait path's clean outcome must stand)", snap.errMsg)
	}
	if ok, active := s.tryAcquireBuildSlot("third"); ok {
		t.Fatal("watchdog released a slot owned by another build")
	} else if active != "next" {
		t.Fatalf("slot holder = %q, want next", active)
	}
}

// --- single-build lock ---

func TestSingleBuildSlot(t *testing.T) {
	s := newTestServer(t)
	if ok, _ := s.tryAcquireBuildSlot("a"); !ok {
		t.Fatal("first acquire failed")
	}
	ok, active := s.tryAcquireBuildSlot("b")
	if ok {
		t.Fatal("second acquire succeeded while a build was active")
	}
	if active != "a" {
		t.Fatalf("active build id = %q, want a", active)
	}
	// Releasing a non-owner is a no-op; the slot stays held by "a".
	s.releaseBuildSlot("b")
	if ok, _ := s.tryAcquireBuildSlot("c"); ok {
		t.Fatal("acquire succeeded after a no-op release")
	}
	// The owner releases; the next build can start.
	s.releaseBuildSlot("a")
	if ok, _ := s.tryAcquireBuildSlot("c"); !ok {
		t.Fatal("acquire failed after the owner released")
	}
}

func TestHandleStartBuildRejectsConcurrent(t *testing.T) {
	s := newTestServer(t)
	// Occupy the slot as if a build were already running.
	if ok, _ := s.tryAcquireBuildSlot("in-flight"); !ok {
		t.Fatal("could not occupy the build slot")
	}

	body := `{"compose":{"vertical":"robotics","sku":"amr","platform":"wcl","os":"ubuntu24","imageType":"iso"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleStartBuild(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (build in progress)", rec.Code)
	}
	// No second build should have been tracked.
	if n := len(s.tracker.all()); n != 0 {
		t.Fatalf("tracker has %d builds; a rejected start must not create one", n)
	}
}
