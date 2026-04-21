package main

import (
	"fmt"
	"os"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
	"github.com/spf13/cobra"
)

// Command-line flags that can override config file settings
var (
	log                 = logger.Logger()
	logLevel     string = ""
	globalConfig *config.GlobalConfig
)

func main() {
	// Initialize global configuration first
	globalConfig = config.DefaultGlobalConfig()
	globalConfig.WorkDir = "/workspace"
	globalConfig.TempDir = "/tmp"
	globalConfig.Logging.Level = "info"
	config.SetGlobal(globalConfig)

	// Setup logger with configured level
	_, cleanup := logger.InitWithLevel(globalConfig.Logging.Level)
	defer cleanup()

	// Create and execute root command
	rootCmd := createRootCommand()
	security.AttachRecursive(rootCmd, security.DefaultLimits())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// createRootCommand creates and configures the root cobra command with all subcommands
func createRootCommand() *cobra.Command {
	var config, repo string
	var attendedInstaller bool

	rootCmd := &cobra.Command{
		Use:   "live-installer",
		Short: "Live installer for install os image to the target disk",
		Long: `Live installer is expected to be run within the ISO initrd system.

The tool supports install custom os images according to the OSI image template yml file:
- Create partitions and format the filesystem on the target disk
- Install OS packages from the package config list
- Update system configuration according to the image template

Use 'live-installer --help' to see available params.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if logLevel != "" {
				logger.SetLogLevel(logLevel)
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			if !attendedInstaller {
				logger.SetLogLevel("debug")
				if err := unattendedInstall(config, repo); err != nil {
					fmt.Fprintf(os.Stderr, "Unattended install failed: %v\n", err)
					os.Exit(1)
				}
			} else {
				installationQuit, err := attendedInstall(config, repo)
				if installationQuit {
					log.Errorf("Installation was quit by the user")
					os.Exit(1)
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "Attended install failed: %v\n", err)
					os.Exit(1)
				}
			}
		},
	}

	// Add global flags
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "",
		"Log level (debug, info, warn, error)")

	rootCmd.Flags().BoolVarP(&attendedInstaller, "attended", "a", false, "Enable UI for user input during installation")
	rootCmd.Flags().StringVarP(&config, "config", "c", "", "Template yaml file path")
	rootCmd.Flags().StringVarP(&repo, "repo", "r", "", "Local package cache directory")

	if err := rootCmd.MarkFlagRequired("config"); err != nil {
		log.Fatalf("Failed to mark 'config' flag as required: %v", err)
	}
	if err := rootCmd.MarkFlagRequired("repo"); err != nil {
		log.Fatalf("Failed to mark 'repo' flag as required: %v", err)
	}

	return rootCmd
}
