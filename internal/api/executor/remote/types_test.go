// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/api/service"
)

func TestReadEventsParsesFramesAndIgnoresHeartbeats(t *testing.T) {
	t.Parallel()
	in := ": hello\n\nevent: log\ndata: {\"n\":0,\"line\":\"a\"}\n\n" +
		": ping\n\nevent: done\ndata: {\"status\":\"succeeded\"}\n\n"
	var names []string
	err := readEvents(strings.NewReader(in), func(ev event) bool {
		names = append(names, ev.name)
		return ev.name != "done"
	})
	if err != nil || strings.Join(names, ",") != "log,done" {
		t.Fatalf("names = %v, err = %v", names, err)
	}
}

func TestHumanSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{0, ""},
		{512, "512 B"},
		{2_500_000, "2.50 MB"},
		{1_130_000_000, "1.13 GB"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := humanSize(tc.n); got != tc.want {
				t.Errorf("humanSize(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

func TestTerminalStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want service.BuildStatus
		ok   bool
	}{
		{"succeeded", service.StatusSuccess, true},
		{"failed", service.StatusFailed, true},
		{"cancelled", service.StatusCancelled, true},
		{"running", "", false},
		{"queued", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, ok := terminalStatus(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("terminalStatus(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// toArtifacts keeps the protocol's URL as the artifact's location, renders the
// size the way ICT's own output does, and never leaves the type empty.
func TestToArtifacts(t *testing.T) {
	t.Parallel()
	got := toArtifacts([]artifact{
		{Name: "a.iso", Type: "image", URL: "https://s/a.iso", SizeBytes: 1_130_000_000, SHA256: "ab"},
		{Name: "notes.txt", URL: "https://s/notes.txt"},
	})
	if len(got) != 2 {
		t.Fatalf("got %d artifacts", len(got))
	}
	if got[0].URL != "https://s/a.iso" || got[0].Size != "1.13 GB" || got[0].SHA256 != "ab" || got[0].Path != "" {
		t.Errorf("image = %+v", got[0])
	}
	if got[1].Type != "other" || got[1].Size != "" {
		t.Errorf("untyped = %+v", got[1])
	}
}

func TestImageName(t *testing.T) {
	t.Parallel()
	if got := imageName("image:\n  name: demo\n"); got != "demo" {
		t.Errorf("imageName = %q", got)
	}
	if got := imageName("not: yaml: ["); got != "" {
		t.Errorf("imageName(invalid) = %q", got)
	}
}

// The reference service follows the protocol document: idempotent submission,
// numbered log lines resumable with from, phase and meta resent on connect, and
// a done event that ends the stream.
func TestReferenceServiceFollowsTheProtocol(t *testing.T) {
	f := newFakeService()
	defer f.Close()
	f.seed("b1", "queued")
	f.log("b1", "l0", "l1")
	f.meta("b1", "worker", "w-1")
	f.phase("b1", "dispatching", "Preparing infra")
	f.finish("b1", "succeeded", "", artifact{Name: "x.iso", Type: "image", URL: "https://s/x.iso"})

	stream, err := openTestStream(f.URL() + "/v1/builds/b1/events?from=1")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var seen []string
	err = readEvents(stream, func(ev event) bool {
		seen = append(seen, ev.name+":"+strings.TrimSpace(string(ev.data)))
		return ev.name != "done"
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(seen, " ")
	wants := []string{
		`meta:{"key":"worker","value":"w-1"}`, `phase:{"id":"dispatching"`,
		`log:{"n":1,"line":"l1"}`, `done:{"status":"succeeded"`,
	}
	for _, want := range wants {
		if !strings.Contains(joined, want) {
			t.Errorf("stream lacks %s:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `"line":"l0"`) {
		t.Errorf("from=1 replayed line 0:\n%s", joined)
	}
}
