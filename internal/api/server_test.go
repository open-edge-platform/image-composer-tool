// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

// slowFakeICT sleeps briefly before succeeding, giving the shutdown test a
// window where the build is still in-flight when the context is cancelled.
const slowFakeICT = `#!/bin/sh
sleep 0.3
echo "	INFO	display/display.go:79	    • image.raw.gz (1.13 GB)"
echo "	INFO	display/display.go:80	      /output/image.raw.gz"
exit 0
`

// TestStartWaitsForInFlightBuildOnShutdown is the regression test for the
// container-shutdown half of the merged-template bug: a build still running
// when the process is asked to shut down (ctx cancelled, mirroring the
// SIGTERM-driven context main.go installs) must get a bounded chance to reach
// a terminal state — and archive its merged template — before Start returns,
// instead of the in-flight runBuild goroutine being abandoned along with the
// rest of the process mid-merge.
func TestStartWaitsForInFlightBuildOnShutdown(t *testing.T) {
	srv, _ := newTestServerWithICT(t, slowFakeICT)
	srv.addr = "127.0.0.1:0"

	acc, err := srv.svc.StartBuild(service.BuildRequest{Compose: &service.Selection{
		Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso",
		ImageName: "shutdown-test-name",
	}})
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start(ctx) }()

	// Give Start a moment to bind and begin serving before tearing it down —
	// the build (slowFakeICT sleeps 0.3s) is still running at this point.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancellation — it must not hang past buildDrainTimeout")
	}

	h, ok := srv.svc.Build(acc.BuildID)
	if !ok {
		t.Fatalf("build %s not tracked", acc.BuildID)
	}
	res := h.Result()
	if res.Status != service.StatusSuccess {
		t.Fatalf("build status = %s, want success — Start must wait for the in-flight build to finish", res.Status)
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
