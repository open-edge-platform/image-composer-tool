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
	configFile       string = ""    // Path to config file
	logLevel         string = ""    // Empty means use config file value
	verbose          bool   = false // default verbose off
	logFilePath      string = ""    // Optional log file override
	actualConfigFile string = ""    // Actual config file path found during init
	loggerCleanup    func()
)

func main() {
	cobra.OnInitialize(initConfig)

	defer func() {
		if loggerCleanup != nil {
			loggerCleanup()
		}
	}()

	// Create and execute root command
	rootCmd := createRootCommand()
	security.AttachRecursive(rootCmd, security.DefaultLimits())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	// Initialize global configuration
	configFilePath := configFile
	if configFilePath == "" {
		configFilePath = config.FindConfigFile()
	}
	actualConfigFile = configFilePath

	globalConfig, err := config.LoadGlobalConfig(configFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	if logFilePath != "" {
		globalConfig.Logging.File = logFilePath
	}
	if logLevel != "" {
		globalConfig.Logging.Level = logLevel
	}

	// Set global config singleton
	config.SetGlobal(globalConfig)

	// Setup logger with configured level and optional file output (overridden later if needed)
	_, cleanup, logErr := logger.InitWithConfig(logger.Config{
		Level:    globalConfig.Logging.Level,
		FilePath: globalConfig.Logging.File,
	})
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", logErr)
		os.Exit(1)
	}
	loggerCleanup = cleanup
}

// createRootCommand creates and configures the root cobra command with all subcommands
func createRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "image-composer-tool",
		Short: "ICT for building Linux distributions",
		Long: `ICT is a toolchain that enables building immutable
Linux distributions using a simple toolchain from pre-built packages emanating
from different Operating System Vendors (OSVs).

The tool supports building custom images for:
- EMT (Edge Microvisor Toolkit)
- Azure Linux
- Wind River eLxr
- Ubuntu
- RCD (Red Hat Compatible Distro)
	Use 'image-composer-tool --help' to see available commands.
	Use 'image-composer-tool <command> --help' for more information about a command.`,
	}

	// Add global flags
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "",
		"Path to configuration file")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "",
		"Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().StringVar(&logFilePath, "log-file", "",
		"Log file path to tee logs (overrides configuration file)")

	// Add all subcommands
	rootCmd.AddCommand(createBuildCommand())
	rootCmd.AddCommand(createValidateCommand())
	rootCmd.AddCommand(createVersionCommand())
	rootCmd.AddCommand(createConfigCommand())
	rootCmd.AddCommand(createCacheCommand())
	rootCmd.AddCommand(createInspectCommand())
	rootCmd.AddCommand(createAICommand())
	rootCmd.AddCommand(createCompareCommand())

	// Initialize Cobra's default completion command
	rootCmd.InitDefaultCompletionCmd()

	// Add install subcommand to the completion command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "completion" {
			cmd.AddCommand(createCompletionInstallCommand())
			break
		}
	}

	attachLoggingHooks(rootCmd)

	return rootCmd
}

func attachLoggingHooks(cmd *cobra.Command) {
	wrapWithLogging(cmd)
	for _, child := range cmd.Commands() {
		attachLoggingHooks(child)
	}
}

func wrapWithLogging(cmd *cobra.Command) {
	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		applyLogOverrides(c)

		logConfigurationDetails()
		if prev != nil {
			return prev(c, args)
		}
		return nil
	}
}

func applyLogOverrides(cmd *cobra.Command) {
	requested := resolveRequestedLogLevel(cmd)
	if requested == "" {
		return
	}

	globalConfig := config.Global()
	if globalConfig.Logging.Level != requested {
		globalConfig.Logging.Level = requested
		config.SetGlobal(globalConfig)
	}
	logger.SetLogLevel(requested)
}

func resolveRequestedLogLevel(cmd *cobra.Command) string {
	if logLevel != "" {
		return logLevel
	}
	if cmd == nil {
		return ""
	}
	flag := cmd.Flags().Lookup("verbose")
	if flag == nil || !flag.Changed {
		return ""
	}
	isVerbose, err := cmd.Flags().GetBool("verbose")
	if err != nil || !isVerbose {
		return ""
	}
	return "debug"
}

func logConfigurationDetails() {
	log := logger.Logger()
	if actualConfigFile != "" {
		log.Infof("Using configuration from: %s", actualConfigFile)
	}
	cacheDir, _ := config.CacheDir()
	workDir, _ := config.WorkDir()
	log.Debugf("Config: workers=%d, cache_dir=%s, work_dir=%s, temp_dir=%s",
		config.Workers(), cacheDir, workDir, config.TempDir())
}
