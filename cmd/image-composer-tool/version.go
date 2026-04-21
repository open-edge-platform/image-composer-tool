package main

import (
	"fmt"

	"github.com/open-edge-platform/image-composer-tool/internal/config/version"
	"github.com/spf13/cobra"
)

// createVersionCommand creates the version subcommand
func createVersionCommand() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Display version information",
		Run:   executeVersion,
	}

	return versionCmd
}

// executeVersion handles the version command logic
func executeVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("%s v%s\n", version.Toolname, version.Version)
	fmt.Printf("Build Date: %s\n", version.BuildDate)
	fmt.Printf("Commit: %s\n", version.CommitSHA)
	fmt.Printf("Organization: %s\n", version.Organization)
}
