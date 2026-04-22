package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// createCompletionInstallCommand creates the install subcommand under completion
func createCompletionInstallCommand() *cobra.Command {
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install shell completion script",
		Long: `Install shell completion script for Bash, Zsh, Fish, or PowerShell.
Automatically detects your shell and installs the appropriate completion script.`,
		RunE: executeInstallCompletion,
	}

	// Add flags
	installCmd.Flags().String("shell", "", "Specify shell type (bash, zsh, fish, powershell)")
	installCmd.Flags().Bool("force", false, "Force overwrite existing completion files")

	return installCmd
}

// executeInstallCompletion handles installation of shell completion scripts
func executeInstallCompletion(cmd *cobra.Command, args []string) error {
	shellType := ""
	userForce := false

	// Process flags
	if cmd.Flags().Changed("shell") {
		var err error
		shellType, err = cmd.Flags().GetString("shell")
		if err != nil {
			return err
		}
	}

	if cmd.Flags().Changed("force") {
		var err error
		userForce, err = cmd.Flags().GetBool("force")
		if err != nil {
			return err
		}
	}

	// If no shell specified, detect current shell
	if shellType == "" {
		shellEnv := os.Getenv("SHELL")
		if shellEnv != "" {
			switch {
			case strings.Contains(shellEnv, "bash"):
				shellType = "bash"
			case strings.Contains(shellEnv, "zsh"):
				shellType = "zsh"
			case strings.Contains(shellEnv, "fish"):
				shellType = "fish"
			default:
				return fmt.Errorf("unsupported shell: %s. Please specify shell with --shell flag", shellEnv)
			}
		} else {
			// On Windows, we may not have $SHELL
			if os.Getenv("PSModulePath") != "" {
				shellType = "powershell"
			} else {
				return fmt.Errorf("could not detect shell. Please specify with --shell flag")
			}
		}
	}

	// Generate completion script
	var buf bytes.Buffer
	switch shellType {
	case "bash":
		if err := cmd.Root().GenBashCompletion(&buf); err != nil {
			return fmt.Errorf("error generating Bash completion: %w", err)
		}
	case "zsh":
		if err := cmd.Root().GenZshCompletion(&buf); err != nil {
			return fmt.Errorf("error generating Zsh completion: %w", err)
		}
	case "fish":
		if err := cmd.Root().GenFishCompletion(&buf, true); err != nil {
			return fmt.Errorf("error generating Fish completion: %w", err)
		}
	case "powershell":
		if err := cmd.Root().GenPowerShellCompletion(&buf); err != nil {
			return fmt.Errorf("error generating PowerShell completion: %w", err)
		}
	default:
		return fmt.Errorf("unsupported shell type: %s", shellType)
	}

	// Determine where to save the completion script
	var targetPath string
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %v", err)
	}

	switch shellType {
	case "bash":
		// Prefer user-scoped completions
		userDir := filepath.Join(homeDir, ".bash_completion.d")
		if _, err := os.Stat(userDir); os.IsNotExist(err) {
			if err := os.MkdirAll(userDir, 0700); err != nil {
				return fmt.Errorf("could not create directory %s: %v", userDir, err)
			}
		}
		completionDir := userDir

		// Optional system install if writable and explicitly requested
		// (e.g., export IMAGE_COMPOSER_COMPLETION_SCOPE=system)
		systemDir := "/etc/bash_completion.d"
		if os.Getenv("IMAGE_COMPOSER_COMPLETION_SCOPE") == "system" {
			if _, err := os.Stat(systemDir); !os.IsNotExist(err) && dirWritable(systemDir) {
				completionDir = systemDir
			}
		}

		targetPath = filepath.Join(completionDir, "os-image-composer.bash")
	case "zsh":
		completionDir := filepath.Join(homeDir, ".zsh/completion")
		if _, err := os.Stat(completionDir); os.IsNotExist(err) {
			if err := os.MkdirAll(completionDir, 0700); err != nil {
				return fmt.Errorf("could not create directory %s: %v", completionDir, err)
			}
		}
		targetPath = filepath.Join(completionDir, "_os-image-composer")
	case "fish":
		completionDir := filepath.Join(homeDir, ".config/fish/completions")
		if _, err := os.Stat(completionDir); os.IsNotExist(err) {
			if err := os.MkdirAll(completionDir, 0700); err != nil {
				return fmt.Errorf("could not create directory %s: %v", completionDir, err)
			}
		}
		targetPath = filepath.Join(completionDir, "os-image-composer.fish")
	case "powershell":
		profilePath := filepath.Join(homeDir, "Documents/WindowsPowerShell")
		if _, err := os.Stat(profilePath); os.IsNotExist(err) {
			if err := os.MkdirAll(profilePath, 0700); err != nil {
				return fmt.Errorf("could not create directory %s: %v", profilePath, err)
			}
		}
		targetPath = filepath.Join(profilePath, "os-image-composer-completion.ps1")
	}

	// Check if file exists
	if _, err := os.Stat(targetPath); err == nil && !userForce {
		return fmt.Errorf("completion file already exists at %s. Use --force to overwrite", targetPath)
	}

	// Write completion script to file
	if err := os.WriteFile(targetPath, buf.Bytes(), 0600); err != nil {
		return fmt.Errorf("could not write completion file: %v", err)
	}

	fmt.Printf("Shell completion installed for %s at %s\n", shellType, targetPath)
	fmt.Printf("Refer to the README.md file for further instructions to activate the installed completion file based on your shell type.\n")

	return nil
}

// dirWritable checks if the specified directory is writable by attempting to create and remove a temporary file.
func dirWritable(p string) bool {
	tf, err := os.CreateTemp(p, ".probe-*")
	if err != nil {
		return false
	}
	name := tf.Name()
	if err := tf.Close(); err != nil {
		_ = os.Remove(name)
		return false
	}
	if err := os.Remove(name); err != nil {
		return false
	}
	return true
}
