package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
	"github.com/spf13/cobra"
)

// signalExitCode is the conventional Unix exit code for termination by SIGINT
// (128 + signal number 2 = 130). We use 130 for SIGTERM (signal 15, code 143)
// as well because the build tool's cancel semantics are the same either way
// and callers scripting around the exit code want a single "cancelled" value.
const signalExitCode = 130

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

	// Install a cancellable root context that fires on SIGINT/SIGTERM.
	// The build subcommand observes cmd.Context() to bind the shell layer
	// to this ctx (via shell.SetContext) so a signal cascades into:
	//   - SIGTERM to each spawned bash/sudo/tool process group (Phase 1),
	//   - LIFO cleanup of registered mounts and loop devices (Phase 3+4),
	//   - a wrapped context.Canceled error from executeBuild.
	ctx, stopSignalCtx := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stopSignalCtx()

	// Second-signal hard exit: if the operator hits Ctrl+C again while
	// cleanup is still running (e.g. a wedged umount), skip the rest and
	// exit immediately with 130. See watchForSecondSignal's docstring for
	// why this needs a separate channel — signal.Notify broadcasts to every
	// registered channel, so this one receives the first signal too and
	// intentionally ignores it, then acts on the second.
	go watchForSecondSignal()

	if code := exitCodeForError(rootCmd.ExecuteContext(ctx)); code != 0 {
		os.Exit(code)
	}
}

// exitCodeForError maps a rootCmd.ExecuteContext return value to the
// process exit code:
//   - nil                    → 0    (success)
//   - context.Canceled       → 130  (SIGINT/SIGTERM cooperative cancel)
//   - anything else          → 1    (pre-existing failure semantics,
//     including context.DeadlineExceeded surfaced by an internal timeout
//     such as PostProcess's detached cleanup budget — those are not
//     user-initiated cancellations and callers scripting around the tool
//     need to distinguish them from a Ctrl+C)
//
// Extracted from main so tests can exercise the mapping without spawning
// a subprocess or invoking os.Exit.
func exitCodeForError(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		return signalExitCode
	}
	return 1
}

// watchForSecondSignal installs an independent signal handler that hard-exits
// on the second SIGINT/SIGTERM. Go delivers each signal to every channel
// registered via signal.Notify, so this channel receives the first signal too
// (in parallel with the one NotifyContext uses to cancel the build ctx in
// main) — we drain that first delivery and act only on the second. Runs on its
// own goroutine for the lifetime of the process; no shutdown handshake is
// needed because os.Exit tears the process down. Bypasses deferred
// loggerCleanup — acceptable trade-off for a bounded exit when cleanup is
// wedged.
func watchForSecondSignal() {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh // first signal: also delivered to NotifyContext's channel; ignore here.
	<-sigCh // second signal: hard exit.
	fmt.Fprintln(os.Stderr, "second signal received; exiting immediately")
	os.Exit(signalExitCode)
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
	rootCmd.AddCommand(createServeCommand())
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
