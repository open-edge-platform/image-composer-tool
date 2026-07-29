// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"strings"
	"testing"
)

func TestSudoersSpecRender(t *testing.T) {
	spec := SudoersSpec{
		User:      "ictsvc",
		ICTPath:   "/opt/ict/image-composer-tool",
		KillCmd:   "/usr/bin/kill",
		CatCmd:    "/usr/bin/cat",
		BuildsDir: "/srv/ict-workspace/builds",
	}
	out := spec.Render()

	// The three privileged operations, each scoped to a single command. The cat
	// rule is scoped to the builds subtree, where all artifacts live.
	want := []string{
		"ictsvc ALL=(root) NOPASSWD: /opt/ict/image-composer-tool build *",
		"ictsvc ALL=(root) NOPASSWD: /usr/bin/kill -TERM -[0-9]*",
		"ictsvc ALL=(root) NOPASSWD: /usr/bin/cat /srv/ict-workspace/builds/*",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("rendered sudoers missing rule:\n  want line: %s\n  got:\n%s", w, out)
		}
	}

	// The cat rule must be scoped to the builds subtree, never a bare /* (any
	// root-owned file) nor the whole work dir.
	if strings.Contains(out, "/usr/bin/cat /*") {
		t.Errorf("cat rule is unscoped (grants reading any path); want builds-scoped:\n%s", out)
	}

	// Every rule is NOPASSWD-scoped to root and to this user only.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "ictsvc ALL=(root) NOPASSWD:") {
			t.Errorf("non-comment rule is not scoped to (root) NOPASSWD for the user: %q", line)
		}
	}
}

func TestResolveSudoersSpecResolvesRelativePaths(t *testing.T) {
	// A relative binary/workdir must come back absolute — sudo matches commands
	// literally, so a relative path would never match an absolute NOPASSWD rule.
	spec, err := ResolveSudoersSpec("./build/image-composer-tool", "webui-workspace")
	if err != nil {
		// Running as root legitimately refuses (no rules needed); skip in that case.
		if strings.Contains(err.Error(), "running as root") {
			t.Skip("running as root: generator correctly refuses")
		}
		t.Fatalf("ResolveSudoersSpec: %v", err)
	}
	if !strings.HasPrefix(spec.ICTPath, "/") {
		t.Errorf("ICTPath not absolute: %q", spec.ICTPath)
	}
	if !strings.HasPrefix(spec.BuildsDir, "/") || !strings.HasSuffix(spec.BuildsDir, "/builds") {
		t.Errorf("BuildsDir not an absolute .../builds path: %q", spec.BuildsDir)
	}
	if !strings.HasPrefix(spec.KillCmd, "/") || !strings.HasSuffix(spec.KillCmd, "kill") {
		t.Errorf("KillCmd not an absolute kill path: %q", spec.KillCmd)
	}
	if !strings.HasPrefix(spec.CatCmd, "/") || !strings.HasSuffix(spec.CatCmd, "cat") {
		t.Errorf("CatCmd not an absolute cat path: %q", spec.CatCmd)
	}
}
