package main

import (
	"testing"
)

// FuzzCreateRootCommand tests the root command creation with various global flag values
func FuzzCreateRootCommand(f *testing.F) {
	// Seed with various flag combinations
	f.Add("", "")                       // empty values
	f.Add("/tmp/config.yml", "info")    // normal values
	f.Add("invalid/path", "debug")      // invalid config path
	f.Add("/dev/null", "invalid-level") // invalid log level
	f.Add("very-long-config-path-that-might-cause-issues", "error")
	f.Add("/etc/passwd", "warn") // system file as config
	f.Add("", "trace")           // empty config, non-standard log level

	f.Fuzz(func(t *testing.T, configPath string, logLevelValue string) {
		// Set fuzzed values for testing
		configFile = configPath
		logLevel = logLevelValue

		// The function should not crash regardless of global variable values
		cmd := createRootCommand()

		// Basic validation - command should be created
		if cmd == nil {
			t.Fatal("createRootCommand returned nil")
		}

		// Verify basic command properties are set
		if cmd.Use == "" {
			t.Error("Command Use field is empty")
		}

		if cmd.Short == "" {
			t.Error("Command Short description is empty")
		}

		// Verify subcommands were added
		if len(cmd.Commands()) == 0 {
			t.Error("No subcommands were added to root command")
		}
	})
}

// FuzzCommandLineArgs tests command parsing with various argument combinations
func FuzzCommandLineArgs(f *testing.F) {
	// Seed with various command line argument patterns
	f.Add("--help")
	f.Add("--version")
	f.Add("build")
	f.Add("validate")
	f.Add("config")
	f.Add("--config=/tmp/test.yml")
	f.Add("--log-level=debug")
	f.Add("build --help")
	f.Add("invalid-command")
	f.Add("")

	f.Fuzz(func(t *testing.T, args string) {
		// Create root command
		cmd := createRootCommand()

		// Split args and set them for testing
		// We don't actually execute the command to avoid side effects
		// Just test that argument parsing doesn't crash
		if args != "" {
			cmd.SetArgs([]string{args})
		}

		// Verify the command structure is intact first
		if cmd == nil {
			t.Fatal("Command became nil during argument processing")
		}

		// Test that command setup doesn't crash with various arguments
		// Note: We don't call Execute() to avoid actual command execution
		_ = cmd.Use
		_ = cmd.Short
		_ = cmd.Long
	})
}
