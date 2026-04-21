package main

import (
	"fmt"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/spf13/cobra"
)

// createConfigCommand creates the config subcommand
func createConfigCommand() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Long: `Manage global configuration for the ICT.

Available commands:
  init    Initialize a new configuration file with default values`,
	}

	// Add only the init subcommand
	configCmd.AddCommand(createConfigInitCommand())

	return configCmd
}

// createConfigInitCommand creates the config init subcommand
func createConfigInitCommand() *cobra.Command {
	initCmd := &cobra.Command{
		Use:   "init [config-file]",
		Short: "Initialize a new configuration file",
		Long: `Initialize a new configuration file with default values.

If no path is specified, the config will be created in the current directory as image-composer-tool.yml

Examples:
  # Create config in current directory
  image-composer-tool config init
  
  # Create config at specific location
  image-composer-tool config init /etc/image-composer-tool/config.yml
  
  # Create config in user's home directory
  image-composer-tool config init ~/.ict/config.yml`,
		Args: cobra.MaximumNArgs(1),
		RunE: executeConfigInit,
	}

	return initCmd
}

// executeConfigInit handles the config init command logic
func executeConfigInit(cmd *cobra.Command, args []string) error {
	configPath := "image-composer-tool.yml"
	if len(args) > 0 {
		configPath = args[0]
	}

	// Create default config
	defaultConfig := config.DefaultGlobalConfig()

	// Save to file with descriptive comments
	if err := defaultConfig.SaveGlobalConfigWithComments(configPath); err != nil {
		return fmt.Errorf("failed to save config file: %v", err)
	}

	fmt.Printf("Configuration file created at: %s\n", configPath)
	fmt.Printf("\nDefault configuration settings:\n")
	fmt.Printf("  Workers: %d\n", defaultConfig.Workers)
	fmt.Printf("  Cache Directory: %s\n", defaultConfig.CacheDir)
	fmt.Printf("  Work Directory: %s\n", defaultConfig.WorkDir)
	fmt.Printf("  Temp Directory: %s\n", defaultConfig.TempDir)
	fmt.Printf("  Log Level: %s\n", defaultConfig.Logging.Level)
	fmt.Printf("  Log File: %s\n", defaultConfig.Logging.File)
	fmt.Printf("\nEdit the configuration file to customize these settings.\n")

	return nil
}
