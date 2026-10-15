package service

import (
	"bufio"
	"fmt"
	"os"
	"testing"
)

// TestReplayLogPhases is a manual helper (untracked): replays a real compose log
// line by line and prints where the stepper's phase changes.
//
//	ICT_REPLAY_LOG=/path/to/compose.log go test ./internal/api/service -run ReplayLogPhases -v
func TestReplayLogPhases(t *testing.T) {
	path := os.Getenv("ICT_REPLAY_LOG")
	if path == "" {
		t.Skip("set ICT_REPLAY_LOG")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	last := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if p := DetectPhase(lines); p != last {
			fmt.Printf("phase -> %-11s  at line %d: %s\n", p, len(lines), trunc(sc.Text()))
			last = p
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning %s: %v", path, err)
	}
	fmt.Printf("final phase after %d lines: %s\n", len(lines), last)
}

func trunc(s string) string {
	if len(s) > 110 {
		return s[:110] + "…"
	}
	return s
}
