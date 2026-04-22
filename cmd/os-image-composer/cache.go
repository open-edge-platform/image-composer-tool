package main

import (
	"fmt"

	"github.com/open-edge-platform/os-image-composer/internal/cache"
	"github.com/spf13/cobra"
)

func createCacheCommand() *cobra.Command {
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage cached artifacts",
		Long: `Manage cache directories used by OS Image Composer.

Available commands:
  clean    Remove cached packages or workspace chroot data`,
	}

	cacheCmd.AddCommand(createCacheCleanCommand())

	return cacheCmd
}

func createCacheCleanCommand() *cobra.Command {
	var (
		opts cache.CleanOptions
		all  bool
	)

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove cached packages or chroot artifacts",
		Long: `Remove cached packages or workspace chroot data to reclaim disk space.

By default, the command removes cached packages. Use flags to target workspace
caches or to restrict cleanup to a specific provider.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			packagesFlag := cmd.Flags().Changed("packages")
			workspaceFlag := cmd.Flags().Changed("workspace")

			if all {
				opts.CleanPackages = true
				opts.CleanWorkspace = true
			} else if !packagesFlag && !workspaceFlag {
				opts.CleanPackages = true
			}

			if !opts.CleanPackages && !opts.CleanWorkspace {
				return fmt.Errorf("nothing to clean: specify --packages, --workspace, or --all")
			}

			result, err := cache.Clean(opts)
			if err != nil {
				return err
			}

			output := []string{}
			if opts.DryRun {
				output = append(output, "Dry run: no files were deleted.")
			}

			if len(result.RemovedPaths) > 0 {
				header := "Removed paths:"
				if opts.DryRun {
					header = "Would remove:"
				}
				output = append(output, header)
				output = append(output, indentPaths(result.RemovedPaths)...)
			}

			if len(result.RemovedPaths) == 0 && len(result.SkippedPaths) == 0 {
				scopeDesc := ""
				if opts.CleanPackages && opts.CleanWorkspace {
					scopeDesc = "package or workspace cache"
				} else if opts.CleanPackages {
					scopeDesc = "package cache"
				} else if opts.CleanWorkspace {
					scopeDesc = "workspace cache"
				}

				if opts.ProviderID != "" {
					scopeDesc += fmt.Sprintf(" for provider '%s'", opts.ProviderID)
				}

				output = append(output, fmt.Sprintf("No %s entries found.", scopeDesc))
			}

			if len(result.SkippedPaths) > 0 {
				output = append(output, "Skipped (not found):")
				output = append(output, indentPaths(result.SkippedPaths)...)
			}

			writer := cmd.OutOrStdout()
			for _, line := range output {
				if _, err := fmt.Fprintln(writer, line); err != nil {
					return fmt.Errorf("failed to write cache output: %w", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Remove both package and workspace caches")
	cmd.Flags().BoolVar(&opts.CleanPackages, "packages", false, "Remove cached packages")
	cmd.Flags().BoolVar(&opts.CleanWorkspace, "workspace", false, "Remove workspace chroot caches")
	cmd.Flags().StringVar(&opts.ProviderID, "provider-id", "", "Restrict cleanup to a specific provider (os-dist-arch)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be removed without deleting anything")

	return cmd
}

func indentPaths(values []string) []string {
	lines := make([]string, len(values))
	for i, v := range values {
		lines[i] = "  " + v
	}
	return lines
}
