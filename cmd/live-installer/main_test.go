package main

import (
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func TestCreateRootCommand(t *testing.T) {
	// Initialize global config for testing
	globalConfig = config.DefaultGlobalConfig()
	globalConfig.WorkDir = "/workspace"
	globalConfig.TempDir = "/tmp"
	globalConfig.Logging.Level = "info"
	config.SetGlobal(globalConfig)

	rootCmd := createRootCommand()

	// Check that root command is properly created
	if rootCmd == nil {
		t.Fatal("createRootCommand() returned nil")
	}

	// Check command name
	if rootCmd.Use != "live-installer" {
		t.Errorf("expected Use to be 'live-installer', got %q", rootCmd.Use)
	}

	// Check that required flags are defined
	configFlag := rootCmd.Flags().Lookup("config")
	if configFlag == nil {
		t.Fatal("--config flag is not defined")
	}

	repoFlag := rootCmd.Flags().Lookup("repo")
	if repoFlag == nil {
		t.Fatal("--repo flag is not defined")
	}

	attendedFlag := rootCmd.Flags().Lookup("attended")
	if attendedFlag == nil {
		t.Fatal("--attended flag is not defined")
	}

	// Check persistent flags
	logLevelFlag := rootCmd.PersistentFlags().Lookup("log-level")
	if logLevelFlag == nil {
		t.Fatal("--log-level persistent flag is not defined")
	}
}

func TestCreateRootCommand_ShortDescription(t *testing.T) {
	globalConfig = config.DefaultGlobalConfig()
	config.SetGlobal(globalConfig)

	rootCmd := createRootCommand()

	if rootCmd.Short == "" {
		t.Error("expected Short description to be non-empty")
	}

	if rootCmd.Long == "" {
		t.Error("expected Long description to be non-empty")
	}
}

func TestCreateRootCommand_FlagDefaults(t *testing.T) {
	globalConfig = config.DefaultGlobalConfig()
	config.SetGlobal(globalConfig)

	rootCmd := createRootCommand()

	// Test default values
	attendedFlag := rootCmd.Flags().Lookup("attended")
	if attendedFlag.DefValue != "false" {
		t.Errorf("expected --attended default to be 'false', got %q", attendedFlag.DefValue)
	}

	configFlag := rootCmd.Flags().Lookup("config")
	if configFlag.DefValue != "" {
		t.Errorf("expected --config default to be empty, got %q", configFlag.DefValue)
	}

	repoFlag := rootCmd.Flags().Lookup("repo")
	if repoFlag.DefValue != "" {
		t.Errorf("expected --repo default to be empty, got %q", repoFlag.DefValue)
	}
}
