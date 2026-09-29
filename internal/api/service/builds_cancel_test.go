// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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
	if got := classifyExit(failErr, false); got != StatusFailed {
		t.Fatalf("non-cancel exit 2: got %q, want failed", got)
	}
	if got := classifyExit(failErr, true); got != StatusFailed {
		t.Fatalf("cancel-requested exit 2 (unrelated failure): got %q, want failed", got)
	}

	// Signal-code exits under a requested cancel map to cancelled.
	for _, code := range []int{130, 143, 137} {
		err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
		if got := classifyExit(err, true); got != StatusCancelled {
			t.Fatalf("cancel-requested exit %d: got %q, want cancelled", code, got)
		}
		// Same code without a cancel request is a failure.
		if got := classifyExit(err, false); got != StatusFailed {
			t.Fatalf("no-cancel exit %d: got %q, want failed", code, got)
		}
	}

	// A child killed by a signal (Signaled(), no exit code) under a cancel maps to
	// cancelled — this is the path where sudo/ICT is TERM/KILLed rather than
	// exiting with a translated code.
	sigErr := signaledErr(t, "TERM")
	if got := classifyExit(sigErr, true); got != StatusCancelled {
		t.Fatalf("cancel-requested SIGTERM-killed child: got %q, want cancelled", got)
	}
	if got := classifyExit(sigErr, false); got != StatusFailed {
		t.Fatalf("no-cancel SIGTERM-killed child: got %q, want failed", got)
	}
}

// --- signal-failure discrimination ---

// signalCancel treats a failed `sudo -n kill` as delivered ONLY when the failure
// is the benign teardown race — `kill` reporting the group already gone, either
// via the ESRCH text or (GNU coreutils on a partially-gone group) a bare non-zero
// exit with no output. Every other non-zero outcome carries a diagnostic (sudo
// can't authorize, sudo can't execute kill, kill misusage) and must be reported
// as a delivery failure, so a hidden failure can't masquerade as a clean cancel.
func TestIsKillTargetGone(t *testing.T) {
	// The benign already-gone race (the signal was delivered, then a group member
	// exited before kill walked the group). Two shapes: the ESRCH text a POSIX-y
	// kill prints, and — observed on live root cancels — a bare non-zero exit with
	// no output, which is how GNU coreutils kill reports a partially-gone process
	// group. Both must be swallowed, or a clean cancel is reported as a spurious
	// cancellation-failure.
	gone := []string{
		"kill: (-12345): No such process",
		"bash: kill: (-12345) - No such process",
		"kill: sending signal to -12345 failed: No such process",
		"",       // silent already-gone race: kill exited non-zero, said nothing
		"  \n\t", // whitespace-only is still "no diagnostic"
	}
	for _, out := range gone {
		if !isKillTargetGone(out) {
			t.Errorf("isKillTargetGone(%q) = false, want true (benign teardown race)", out)
		}
	}
	// Every genuine delivery failure carries a diagnostic saying why, so a
	// non-empty message that isn't the ESRCH family must NOT be swallowed.
	realFailure := []string{
		"sudo: a password is required",
		"sudo: a terminal is required to read the password",
		"Sorry, user svc is not allowed to execute '/usr/bin/kill -TERM -123' as root",
		"sudo: no askpass program specified",
		"sudo: kill: command not found",
		"sudo: unable to execute /usr/bin/kill: No such file or directory",
		"kill: invalid option -- 'x'",
	}
	for _, out := range realFailure {
		if isKillTargetGone(out) {
			t.Errorf("isKillTargetGone(%q) = true, want false (real delivery failure)", out)
		}
	}
}

// signalCancel must never TERM the server's own process group. If a cancel is
// serviced before the child has moved into its own group, the recorded pgid can
// still be the server's — signalling it would kill the server (and, when it shares
// a session with the operator's shell, the login session too). The guard turns that
// into a reported error instead, which the caller records as a cancellation-failure.
func TestSignalCancelRefusesOwnGroup(t *testing.T) {
	s := newTestService(t)
	self := syscall.Getpgrp()
	err := s.signalCancel(self)
	if err == nil {
		t.Fatal("signalCancel(own pgid) returned nil; it must refuse to signal the server's own group")
	}
	if !strings.Contains(err.Error(), "own group") {
		t.Fatalf("error should explain the self-group refusal: %v", err)
	}
}

// signalCancel with no recorded group (pgid 0, the pre-start window) is an error,
// not a signal: there is nothing to target yet.
func TestSignalCancelNoPgid(t *testing.T) {
	s := newTestService(t)
	if err := s.signalCancel(0); err == nil {
		t.Fatal("signalCancel(0) returned nil; want an error for an unrecorded group")
	}
}

// On the direct (non-sudo) path, signalling a process group that has already
// exited returns ESRCH, which signalCancel must swallow: the build is on its way
// down, not a delivery failure. We use a pgid that maps to no live group.
func TestSignalCancelDirectESRCHSwallowed(t *testing.T) {
	s := newTestService(t) // Sudo=false → direct syscall.Kill(-pgid)
	// A high, almost-certainly-unused pgid that isn't the server's own group.
	pgid := 1 << 30
	if pgid == syscall.Getpgrp() {
		t.Skip("chosen pgid collides with the test process group")
	}
	if err := s.signalCancel(pgid); err != nil {
		t.Fatalf("signalCancel of a vanished group should swallow ESRCH, got: %v", err)
	}
}

// End-to-end (in-process): a real child that traps SIGTERM, prints a line, and
// exits 130 — exactly how ICT reacts to a cancel — must be classified as
// cancelled with NO residual. This is the regression test for the false
// cancellation-failure that surfaced on main: `kill` racing the group's teardown
// exits non-zero, but the signal was delivered and the child cleaned up. Runs the
// real runBuild + group-signal path with no detached server and no risk to the
// caller's session (the child is its own group leader).
func TestRunBuildCancelClassifiesCancelledNoResidual(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	s := newTestService(t)         // Sudo=false → signalCancel uses syscall.Kill(-pgid)
	s.signalGroup = s.signalCancel // exercise the real signal path, not a stub

	// A fake "build": trap TERM, emit a cleanup line, exit 130 like ICT's handler.
	script := filepath.Join(t.TempDir(), "fakebuild.sh")
	body := "#!/usr/bin/env bash\n" +
		"trap 'echo cleanup complete; exit 130' TERM\n" +
		"echo build running\n" +
		"for i in $(seq 1 100); do sleep 0.1; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	b := &build{
		ID:      "e2e",
		RootDir: filepath.Join(s.buildsRoot(), "e2e"),
		WorkDir: filepath.Join(s.buildsRoot(), "e2e", "work"),
		status:  StatusNotStarted,
		done:    make(chan struct{}),
	}
	if err := os.MkdirAll(b.WorkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s.tracker.add(b)
	if ok, _ := s.tryAcquireBuildSlot(b.ID); !ok {
		t.Fatal("could not claim the build slot")
	}

	go s.runBuild(b, "bash", []string{script})

	// Wait until the child is running and its group is recorded.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if pg := b.currentPgid(); pg > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("build never recorded a process group")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel through the real handler path.
	transitioned, pgid := b.beginCancel()
	if !transitioned {
		t.Fatal("beginCancel did not transition")
	}
	if err := s.signalGroup(pgid); err != nil {
		t.Fatalf("signalGroup: %v", err)
	}

	select {
	case <-b.done:
	case <-time.After(5 * time.Second):
		t.Fatal("build did not finish after cancel")
	}

	snap := b.snapshot()
	if snap.Status != StatusCancelled {
		t.Fatalf("status = %q, want cancelled\nlogs:\n%s", snap.Status, strings.Join(b.snapshotLogs(), "\n"))
	}
	if snap.Residual != nil {
		t.Fatalf("residual = %+v, want nil (cancel delivered and child cleaned up)", snap.Residual)
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
	b := &build{ID: "b", status: StatusRunning, pgid: 4242, done: make(chan struct{})}

	ok, pgid := b.beginCancel()
	if !ok || pgid != 4242 {
		t.Fatalf("first cancel: ok=%v pgid=%d, want true/4242", ok, pgid)
	}
	if s := b.snapshot(); s.Status != StatusCancelling {
		t.Fatalf("status after cancel = %q, want cancelling", s.Status)
	}
	if !b.wasCancelRequested() {
		t.Fatal("cancelRequested not set")
	}

	// A second cancel on an already-cancelling build is rejected (idempotent).
	if ok, _ := b.beginCancel(); ok {
		t.Fatal("second cancel transitioned again; want false")
	}

	// A cancel on a terminal build is rejected.
	done := &build{ID: "d", status: StatusFailed, done: make(chan struct{})}
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
	b := &build{ID: "b", status: StatusRunning, done: make(chan struct{})}
	if pgid, pending := b.setPgidCheckCancel(1234); pgid != 1234 || pending {
		t.Fatalf("no-cancel: pgid=%d pending=%v, want 1234/false", pgid, pending)
	}
	if s := b.snapshot(); s.Status != StatusRunning {
		t.Fatalf("no-cancel status = %q, want running (unchanged)", s.Status)
	}

	// Cancel arrived during the window (status=cancelling, pgid still 0):
	// setPgidCheckCancel records the pgid and reports pending=true so runBuild
	// delivers the deferred signal.
	raced := &build{ID: "r", status: StatusRunning, done: make(chan struct{})}
	if ok, pgid := raced.beginCancel(); !ok || pgid != 0 {
		t.Fatalf("beginCancel before pgid set: ok=%v pgid=%d, want true/0", ok, pgid)
	}
	pgid, pending := raced.setPgidCheckCancel(5678)
	if pgid != 5678 || !pending {
		t.Fatalf("raced cancel: pgid=%d pending=%v, want 5678/true", pgid, pending)
	}

	// A build that already reached a terminal state before the pgid was recorded
	// must not report a pending cancel (nothing to signal).
	term := &build{ID: "t", status: StatusCancelled, cancelRequested: true, done: make(chan struct{})}
	if _, pending := term.setPgidCheckCancel(9999); pending {
		t.Fatal("terminal build reported a pending cancel; want false")
	}
}

// A cancel arriving before the child is spawned must be accepted: the build
// already holds the single-build slot, so refusing it would leave the user with
// no way to release it. beginCancel therefore also accepts not-started.
func TestBeginCancelBeforeStart(t *testing.T) {
	b := &build{ID: "pre", status: StatusNotStarted, done: make(chan struct{})}
	ok, pgid := b.beginCancel()
	if !ok || pgid != 0 {
		t.Fatalf("cancel on not-started build: ok=%v pgid=%d, want true/0", ok, pgid)
	}
	if s := b.snapshot(); s.Status != StatusCancelling {
		t.Fatalf("status = %q, want cancelling", s.Status)
	}
}

// setPgidCheckCancel doubles as the not-started → running promotion, so a build
// that was never cancelled reports running once its child is up.
func TestSetPgidPromotesNotStarted(t *testing.T) {
	b := &build{ID: "b", status: StatusNotStarted, done: make(chan struct{})}
	if pgid, pending := b.setPgidCheckCancel(4321); pgid != 4321 || pending {
		t.Fatalf("pgid=%d pending=%v, want 4321/false", pgid, pending)
	}
	if s := b.snapshot(); s.Status != StatusRunning {
		t.Fatalf("status = %q, want running", s.Status)
	}
}

// finish must be single-shot: the wait path and the cancel watchdog can both
// reach a build, and the first terminal classification has to win — otherwise a
// build that completed successfully could be relabelled cancelled (and the slot
// released twice).
func TestFinishSingleShot(t *testing.T) {
	b := &build{ID: "b", status: StatusRunning, done: make(chan struct{})}
	if !b.finish(StatusSuccess, []Artifact{{Name: "img.raw"}}, "") {
		t.Fatal("first finish reported false; want true (it recorded the outcome)")
	}
	if b.finish(StatusCancelled, nil, "watchdog gave up") {
		t.Fatal("second finish reported true; terminal state must be single-shot")
	}
	s := b.snapshot()
	if s.Status != StatusSuccess {
		t.Fatalf("status = %q, want success (first writer wins)", s.Status)
	}
	if s.ErrMsg != "" || len(s.Artifacts) != 1 {
		t.Fatalf("second finish mutated the record: errMsg=%q artifacts=%v", s.ErrMsg, s.Artifacts)
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
	if s.Residual == nil || s.Residual.Kind != residualCancellation {
		t.Fatalf("residual = %+v, want first (cancellation-failure) to win", s.Residual)
	}
}

// --- CancelBuild ---

// assertServiceError asserts err is a domain *Error with the wanted status, and
// returns its machine code and message so callers can assert on those too. It
// returns the fields rather than the *Error itself so callers that only need the
// status assertion don't have to discard an error-typed return value.
func assertServiceError(t *testing.T, err error, wantStatus int) (code, message string) {
	t.Helper()
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want a *service.Error", err)
	}
	if se.Status != wantStatus {
		t.Fatalf("status = %d, want %d (%s)", se.Status, wantStatus, se.Message)
	}
	return se.Code, se.Message
}

func TestCancelBuildNotFound(t *testing.T) {
	s := newTestService(t)
	_, err := s.CancelBuild("nope")
	assertServiceError(t, err, http.StatusNotFound)
}

func TestCancelBuildNotRunning(t *testing.T) {
	s := newTestService(t)
	s.tracker.add(&build{ID: "done", status: StatusSuccess, done: make(chan struct{})})
	_, err := s.CancelBuild("done")
	assertServiceError(t, err, http.StatusConflict)
}

func TestCancelBuildRunningTransitions(t *testing.T) {
	s := newTestService(t)
	// The signal is delivered successfully; CancelBuild should transition to
	// cancelling and record no residual. The terminal state arrives later on
	// runBuild's wait path.
	delivered := false
	s.signalGroup = func(pgid int) error { delivered = true; return nil }
	b := &build{ID: "run", status: StatusRunning, pgid: 2 << 30, done: make(chan struct{})}
	s.tracker.add(b)

	acc, err := s.CancelBuild("run")
	if err != nil {
		t.Fatalf("CancelBuild: %v", err)
	}
	if acc.Status != StatusCancelling {
		t.Fatalf("accepted status = %q, want cancelling", acc.Status)
	}
	if !delivered {
		t.Fatal("signalGroup was not called")
	}
	snap := b.snapshot()
	if snap.Status != StatusCancelling {
		t.Fatalf("status after cancel = %q, want cancelling", snap.Status)
	}
	// A delivered signal must not raise a residual: the build cancelled cleanly.
	if snap.Residual != nil {
		t.Fatalf("residual = %+v, want nil on a delivered signal", snap.Residual)
	}
}

// A signal that genuinely fails to deliver (e.g. a missing `kill` sudoers rule, so
// sudo can't authorize) must still be surfaced as a cancellation-failure: ICT may
// never have started its teardown, and no terminal SSE event may arrive.
func TestCancelBuildSignalFailureRecordsResidual(t *testing.T) {
	s := newTestService(t)
	s.signalGroup = func(pgid int) error {
		return fmt.Errorf("exit status 1: sudo: a password is required")
	}
	b := &build{ID: "run", status: StatusRunning, pgid: 2 << 30, done: make(chan struct{})}
	s.tracker.add(b)

	if _, err := s.CancelBuild("run"); err != nil {
		t.Fatalf("CancelBuild: %v", err)
	}
	snap := b.snapshot()
	if snap.Status != StatusCancelling {
		t.Fatalf("status after cancel = %q, want cancelling", snap.Status)
	}
	if snap.Residual == nil || snap.Residual.Kind != residualCancellation {
		t.Fatalf("residual = %+v, want cancellation-failure", snap.Residual)
	}
}

// A build record that exists only on disk (persisted as running by a server that
// has since restarted) has no process group and no wait goroutine behind it.
// Cancelling it would mutate a throwaway struct and report a bogus 202, so it
// must be rejected with a 409 pointing the user at the host.
func TestCancelBuildOrphanedRecord(t *testing.T) {
	s := newTestService(t)
	b := &build{
		ID:      "orphan",
		RootDir: filepath.Join(s.buildsRoot(), "orphan"),
		status:  StatusRunning,
		done:    make(chan struct{}),
	}
	if err := os.MkdirAll(b.RootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := b.writeMeta(); err != nil {
		t.Fatal(err)
	}
	// Deliberately NOT added to the tracker: on-disk only, as after a restart.

	_, err := s.CancelBuild("orphan")
	_, msg := assertServiceError(t, err, http.StatusConflict)
	if !strings.Contains(msg, "no live process") {
		t.Fatalf("message should explain the orphan: %q", msg)
	}
}

// A cancel whose signal fails must report the residual in the accepted result:
// the build stays in cancelling and no terminal SSE event may ever arrive, so
// this is the only prompt notification the UI gets.
func TestCancelBuildReportsResidualInResult(t *testing.T) {
	s := newTestService(t)
	s.signalGroup = func(pgid int) error {
		return fmt.Errorf("exit status 1: sudo: a password is required")
	}
	b := &build{ID: "run", status: StatusRunning, pgid: 2 << 30, done: make(chan struct{})}
	s.tracker.add(b)

	acc, err := s.CancelBuild("run")
	if err != nil {
		t.Fatalf("CancelBuild: %v", err)
	}
	if acc.Residual == nil || acc.Residual.Kind != residualCancellation {
		t.Fatalf("result residual = %+v, want cancellation-failure", acc.Residual)
	}
	if !strings.Contains(acc.Residual.Detail, "failed to signal") {
		t.Fatalf("residual detail lacks the underlying error: %q", acc.Residual.Detail)
	}
}

// --- cancel watchdog ---

// A build that ignores the cancel must not hold the single-build slot forever.
// concludeStalledCancel is the watchdog's hard-deadline action: it marks the
// build cancelled with a cancellation-failure and frees the slot.
func TestConcludeStalledCancelReleasesSlot(t *testing.T) {
	s := newTestService(t)
	b := &build{ID: "wedged", status: StatusCancelling, cancelRequested: true, pgid: 2 << 30, done: make(chan struct{})}
	s.tracker.add(b)
	if ok, _ := s.tryAcquireBuildSlot(b.ID); !ok {
		t.Fatal("could not occupy the build slot")
	}

	s.concludeStalledCancel(b)

	snap := b.snapshot()
	if snap.Status != StatusCancelled {
		t.Fatalf("status = %q, want cancelled", snap.Status)
	}
	if snap.Residual == nil || snap.Residual.Kind != residualCancellation {
		t.Fatalf("residual = %+v, want cancellation-failure", snap.Residual)
	}
	if !strings.Contains(snap.ErrMsg, "did not exit") {
		t.Fatalf("errMsg should say the build never exited: %q", snap.ErrMsg)
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
	s := newTestService(t)
	b := &build{ID: "raced", status: StatusCancelling, cancelRequested: true, done: make(chan struct{})}
	s.tracker.add(b)
	// The wait path got there first.
	if !b.finish(StatusCancelled, nil, "") {
		t.Fatal("setup: first finish should have recorded")
	}
	// A new build has since claimed the slot.
	if ok, _ := s.tryAcquireBuildSlot("next"); !ok {
		t.Fatal("setup: could not claim the slot for the next build")
	}

	s.concludeStalledCancel(b)

	if snap := b.snapshot(); snap.ErrMsg != "" {
		t.Fatalf("errMsg = %q, want empty (the wait path's clean outcome must stand)", snap.ErrMsg)
	}
	if ok, active := s.tryAcquireBuildSlot("third"); ok {
		t.Fatal("watchdog released a slot owned by another build")
	} else if active != "next" {
		t.Fatalf("slot holder = %q, want next", active)
	}
}

// --- single-build lock ---

func TestSingleBuildSlot(t *testing.T) {
	s := newTestService(t)
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

func TestStartBuildRejectsConcurrent(t *testing.T) {
	s := newTestService(t)
	// Occupy the slot as if a build were already running.
	if ok, _ := s.tryAcquireBuildSlot("in-flight"); !ok {
		t.Fatal("could not occupy the build slot")
	}

	_, err := s.StartBuild(BuildRequest{Compose: &Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
	}})
	code, _ := assertServiceError(t, err, http.StatusConflict)
	if code != "BUILD_IN_PROGRESS" {
		t.Fatalf("code = %q, want BUILD_IN_PROGRESS", code)
	}
	// No second build should have been tracked.
	if n := len(s.tracker.all()); n != 0 {
		t.Fatalf("tracker has %d builds; a rejected start must not create one", n)
	}
}
