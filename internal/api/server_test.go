// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// blockingFakeICT announces that it is running, then holds the build open until
// the test releases it. SIGTERM is ignored on purpose: the shutdown path
// signals the build's process group, and a fake that died on that signal would
// conclude on its own, leaving nothing to prove that Start actually waits. With
// the signal ignored, the test alone decides when the build concludes — so a
// Start that returned early would be caught rather than racing to a pass.
//
// Verbs are %[1]s (ready file) and %[2]s (release file).
const blockingFakeICT = `#!/bin/sh
trap '' TERM
echo running > "%[1]s"
while [ ! -f "%[2]s" ]; do sleep 0.05; done
echo "	INFO	display/display.go:79	    • image.raw.gz (1.13 GB)"
echo "	INFO	display/display.go:80	      /output/image.raw.gz"
exit 0
`

// waitForFile blocks until path exists, so a test can synchronise on the fake
// child's own progress instead of guessing at it with a sleep.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, path)
}

// TestStartWaitsForInFlightBuildOnShutdown is the regression test for the
// container-shutdown half of the merged-template bug: a build still running
// when the process is asked to shut down (ctx cancelled, mirroring the
// SIGTERM-driven context main.go installs) must be signalled to tear down and
// then given a bounded chance to reach a terminal state — archiving its merged
// template on the way — before Start returns, instead of the in-flight runBuild
// goroutine being abandoned along with the rest of the process mid-merge.
func TestStartWaitsForInFlightBuildOnShutdown(t *testing.T) {
	sync := t.TempDir()
	readyPath := filepath.Join(sync, "running")
	releasePath := filepath.Join(sync, "release")
	release := func() {
		if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
			t.Errorf("releasing fake build: %v", err)
		}
	}
	// Release on every exit path. A failed assertion below must not leave the
	// fake spinning — and Start blocked behind it — for the rest of the suite.
	t.Cleanup(func() { _ = os.WriteFile(releasePath, nil, 0o600) })

	srv, _ := newTestServerWithICT(t, fmt.Sprintf(blockingFakeICT, readyPath, releasePath))
	srv.addr = "127.0.0.1:0"

	acc, err := srv.svc.StartBuild(service.BuildRequest{Compose: &service.Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
		ImageName: "shutdown-test-name",
	}})
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	// The child is genuinely mid-build only once it has written its ready file.
	waitForFile(t, readyPath, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start(ctx) }()
	cancel()

	// Start must still be draining: the build is held open, so returning here
	// would mean the in-flight build was abandoned rather than waited for.
	select {
	case err := <-startErr:
		t.Fatalf("Start returned while a build was still in flight: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	release()

	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(buildDrainTimeout + 10*time.Second):
		t.Fatal("Start did not return after the build concluded")
	}

	h, ok := srv.svc.Build(acc.BuildID)
	if !ok {
		t.Fatalf("build %s not tracked", acc.BuildID)
	}
	res := h.Result()
	// The build concluded rather than being left mid-flight. Which terminal
	// label it carries is the exit classifier's business (this fake ignores the
	// signal and exits 0), so assert only that it is no longer in flight.
	if res.Status == service.StatusRunning || res.Status == service.StatusCancelling ||
		res.Status == service.StatusNotStarted {
		t.Fatalf("build status = %s, want a terminal state — Start must wait for the in-flight build", res.Status)
	}
	// Shutdown must request cancellation, not just wait: without it the drain
	// window is spent watching a compose that cannot finish, and the child is
	// killed with the process while still holding mounts and loop devices.
	if logs := strings.Join(h.SnapshotLogs(), "\n"); !strings.Contains(logs, "server is shutting down") {
		t.Errorf("build was never signalled to stop on shutdown; log was:\n%s", logs)
	}
	if res.TemplatePath == "" {
		t.Fatal("build finished with no TemplatePath")
	}
	archived, err := os.ReadFile(res.TemplatePath)
	if err != nil {
		t.Fatalf("archived template not written: %v", err)
	}
	if !strings.Contains(string(archived), "shutdown-test-name") {
		t.Errorf("archived template does not reflect the override (got the raw delta, not the merged template):\n%s", archived)
	}
}

// TestStartRefusesNewBuildsWhileDraining covers the admission half of the
// shutdown path. Draining does not close the door on its own: http.Server's
// Shutdown stops accepting new connections but lets active handlers run, and
// abandons them when it times out rather than stopping them. A compose that
// was still decoding its body or resolving its template at that moment would
// otherwise spawn a child after shutdown had already snapshotted what to
// cancel and wait for — a build nothing ever signals, killed with the process
// mid-chroot while holding mounts and loop devices.
//
// The in-flight build is held open so Start is provably still inside its
// shutdown sequence when the second compose is attempted.
func TestStartRefusesNewBuildsWhileDraining(t *testing.T) {
	sync := t.TempDir()
	readyPath := filepath.Join(sync, "running")
	releasePath := filepath.Join(sync, "release")
	t.Cleanup(func() { _ = os.WriteFile(releasePath, nil, 0o600) })

	srv, _ := newTestServerWithICT(t, fmt.Sprintf(blockingFakeICT, readyPath, releasePath))
	srv.addr = "127.0.0.1:0"

	compose := service.BuildRequest{Compose: &service.Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
	}}
	if _, err := srv.svc.StartBuild(compose); err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	waitForFile(t, readyPath, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start(ctx) }()
	cancel()

	// Start is now draining and waiting on the held build. Poll until the
	// rejection turns into the shutdown one: admission closes at the top of the
	// shutdown sequence, so this must happen without releasing the build. Until
	// then the slot is simply busy, which is a different (retryable) answer.
	deadline := time.Now().Add(10 * time.Second)
	var lastCode string
	for time.Now().Before(deadline) {
		_, err := srv.svc.StartBuild(compose)
		if err == nil {
			t.Fatal("a compose was admitted while the server was shutting down")
		}
		var svcErr *service.Error
		if !errors.As(err, &svcErr) {
			t.Fatalf("StartBuild error = %v, want a *service.Error", err)
		}
		lastCode = svcErr.Code
		if svcErr.Code == "SERVER_SHUTTING_DOWN" {
			if svcErr.Status != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", svcErr.Status)
			}
			break
		}
		if svcErr.Code != "BUILD_IN_PROGRESS" {
			t.Fatalf("unexpected rejection code %q (%v)", svcErr.Code, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lastCode != "SERVER_SHUTTING_DOWN" {
		t.Fatalf("builds were still being admitted during shutdown (last code %q)", lastCode)
	}

	// Let the held build finish so Start can return.
	if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
		t.Fatalf("releasing fake build: %v", err)
	}
	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(buildDrainTimeout + 10*time.Second):
		t.Fatal("Start did not return after the build concluded")
	}
}
