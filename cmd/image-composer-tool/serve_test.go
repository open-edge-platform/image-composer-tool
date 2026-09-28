// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestExecuteServePrintSudoers exercises the `serve --print-sudoers` helper path:
// it must emit the three scoped sudoers rules to stdout and return without
// starting the server. Running as root, ResolveSudoersSpec refuses (no rules
// needed), which is also a valid outcome we accept.
func TestExecuteServePrintSudoers(t *testing.T) {
	// Set the package-level serve flags the handler reads, and restore them so the
	// test doesn't leak state into other tests in this package.
	origPrint, origBin, origWork := servePrintSudoers, serveBinary, serveWorkDir
	t.Cleanup(func() {
		servePrintSudoers, serveBinary, serveWorkDir = origPrint, origBin, origWork
	})
	servePrintSudoers = true
	serveBinary = "/opt/ict/image-composer-tool"
	serveWorkDir = "/srv/ict-workspace"

	// executeServe writes the rules with fmt.Print (os.Stdout); capture it.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	runErr := executeServe(nil, nil)
	_ = w.Close()
	os.Stdout = origStdout

	out, _ := io.ReadAll(r)
	got := string(out)

	if runErr != nil {
		// The only acceptable error is the root refusal.
		if strings.Contains(runErr.Error(), "running as root") {
			t.Skip("running as root: generator correctly refuses")
		}
		t.Fatalf("executeServe(--print-sudoers) returned error: %v", runErr)
	}

	for _, want := range []string{
		"/opt/ict/image-composer-tool build *",
		"kill -TERM -[0-9]*",
		"/srv/ict-workspace/builds/*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("printed sudoers missing %q; got:\n%s", want, got)
		}
	}
}
