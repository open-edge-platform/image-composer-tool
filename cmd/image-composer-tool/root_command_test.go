package main

import (
	"testing"
)

func TestCreateRootCommand_Wiring(t *testing.T) {
	root := createRootCommand()

	// Check global flags
	if f := root.PersistentFlags().Lookup("config"); f == nil {
		t.Fatalf("--config flag missing")
	}
	if f := root.PersistentFlags().Lookup("log-level"); f == nil {
		t.Fatalf("--log-level flag missing")
	}
	if f := root.PersistentFlags().Lookup("log-file"); f == nil {
		t.Fatalf("--log-file flag missing")
	}

	// Expected subcommands
	want := map[string]bool{
		"build":      false,
		"validate":   false,
		"version":    false,
		"config":     false,
		"cache":      false,
		"completion": false,
	}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("expected subcommand %q to be registered", name)
		}
	}
}
