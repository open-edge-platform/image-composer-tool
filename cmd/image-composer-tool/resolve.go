package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/spf13/cobra"
)

// Resolve command flags
var (
	resolveFull bool // Include OS defaults in the merged output
)

// createResolveCommand creates the resolve subcommand
func createResolveCommand() *cobra.Command {
	resolveCmd := &cobra.Command{
		Use:   "resolve [flags] TEMPLATE_FILE",
		Short: "Resolve a template and print the merged YAML",
		Long: `Resolve a template and print the merged YAML to stdout for debugging and traceability.

By default, resolve walks the template's extends chain and prints the chain-merged
result WITHOUT OS defaults. If the template does not use extends, resolve prints
a short message and exits.

Use --full to additionally merge OS defaults, producing the exact template that
would be used at build time.

Sensitive fields (user passwords, hash algorithms, and secure boot key/certificate
paths) are always redacted in the output. The merged output is always computed
on-demand and is never cached.`,
		Args:              cobra.ExactArgs(1),
		RunE:              executeResolve,
		ValidArgsFunction: templateFileCompletion,
	}

	resolveCmd.Flags().BoolVar(&resolveFull, "full", false,
		"Include OS defaults in the merged output, showing exactly what will be built")

	return resolveCmd
}

// executeResolve handles the resolve command execution logic
func executeResolve(cmd *cobra.Command, args []string) error {
	log := logger.Logger()
	templateFile := args[0]

	var merged *config.ImageTemplate
	if resolveFull {
		// --full merges OS defaults regardless of extends, so go straight to the
		// full loader — no separate leaf load is required, avoiding a duplicate
		// parse of the same file.
		log.Infof("Resolving template with OS defaults: %s", templateFile)
		fullMerged, err := config.LoadAndMergeTemplate(templateFile)
		if err != nil {
			return fmt.Errorf("resolving template: %w", err)
		}
		merged = fullMerged
	} else {
		// Load the leaf only to decide whether extends is present. If not,
		// short-circuit before invoking the chain resolver.
		leaf, err := config.LoadTemplate(templateFile, false)
		if err != nil {
			return fmt.Errorf("resolving template: %w", err)
		}
		if strings.TrimSpace(leaf.Extends) == "" {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No extends used in template, nothing to resolve")
			return nil
		}

		log.Infof("Resolving extends chain for template: %s", templateFile)
		chainMerged, chainPaths, chainErr := config.ResolveAndMergeExtendsChain(templateFile, leaf)
		if chainErr != nil {
			return fmt.Errorf("resolving template: %w", chainErr)
		}
		names := make([]string, len(chainPaths))
		for i, p := range chainPaths {
			names[i] = filepath.Base(p)
		}
		log.Infof("Resolved extends chain: %s", strings.Join(names, " -> "))
		merged = chainMerged
	}

	redacted := config.RedactSensitiveData(merged)

	data, err := config.MarshalTemplateYAML(redacted)
	if err != nil {
		return fmt.Errorf("resolving template: %w", err)
	}

	_, _ = fmt.Fprint(cmd.OutOrStdout(), string(data))
	return nil
}
