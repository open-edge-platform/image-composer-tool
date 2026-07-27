// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
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

func TestScanResidualCleanup(t *testing.T) {
	clean := []string{
		"2026-07-24\tINFO\tbuild.go:1\tstarting build",
		"2026-07-24\tINFO\tbuild.go:2\tunmounting cleanly",
	}
	if got := scanResidualCleanup(clean); got != "" {
		t.Fatalf("clean teardown flagged residual: %q", got)
	}

	dirty := []string{
		"2026-07-24\tINFO\tbuild.go:1\tstarting build",
		"2026-07-24\tERROR\tcleanup.go:9\tleftover loop device /dev/loop3 — run `losetup -l`",
		"2026-07-24\tERROR\tcleanup.go:9\tresidual mount remains — run `mount | grep builds/abc`",
	}
	got := scanResidualCleanup(dirty)
	if got == "" {
		t.Fatal("residual cleanup lines not detected")
	}
	// The logger prefix is stripped, leaving the message.
	if want := "leftover loop device"; !strings.Contains(got, want) {
		t.Fatalf("residual detail %q missing %q", got, want)
	}
	if strings.Contains(got, "INFO") {
		t.Fatalf("residual detail %q should not include the INFO line", got)
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
