package main

import (
	"fmt"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/spf13/cobra"
)

// Validate command flags
var (
	validateMerged bool = false // Whether to validate after merging with defaults
)

// createValidateCommand creates the validate subcommand
func createValidateCommand() *cobra.Command {
	validateCmd := &cobra.Command{
		Use:   "validate [flags] TEMPLATE_FILE",
		Short: "Validate an image template file",
		Long: `Validate an image template file for syntax and schema compliance.
The template file must be in YAML format following the image template schema.
By default, this validates the user template against the input schema.
Use --merged to validate the template after merging with defaults.`,
		Args:              cobra.ExactArgs(1),
		RunE:              executeValidate,
		ValidArgsFunction: templateFileCompletion,
	}

	// Add flags
	validateCmd.Flags().BoolVar(&validateMerged, "merged", false,
		"Validate the template after merging with defaults")

	return validateCmd
}

// executeValidate handles the validate command execution logic
func executeValidate(cmd *cobra.Command, args []string) error {
	log := logger.Logger()
	templateFile := args[0]

	if validateMerged {
		// Validate merged template (with defaults)
		log.Infof("Validating merged template: %s", templateFile)

		mergedTemplate, err := config.LoadAndMergeTemplate(templateFile)
		if err != nil {
			return fmt.Errorf("validation failed during template loading and merging: %v", err)
		}

		log.Info("✓ Merged template validation passed")
		log.Infof("Template: %s (type: %s, os: %s/%s/%s)",
			mergedTemplate.Image.Name,
			mergedTemplate.Target.ImageType,
			mergedTemplate.Target.OS,
			mergedTemplate.Target.Dist,
			mergedTemplate.Target.Arch)

		// Show details about merged configuration
		log.Infof("System Config: %s", mergedTemplate.SystemConfig.Name)
		log.Infof("Packages: %d", len(mergedTemplate.GetPackages()))
		log.Infof("Users: %d", len(mergedTemplate.SystemConfig.Users))
		log.Infof("Kernel: %s", mergedTemplate.GetKernel().Version)

		if mergedTemplate.Disk.Name != "" {
			log.Infof("Disk Config: %s (%s)", mergedTemplate.Disk.Name, mergedTemplate.Disk.Size)
			log.Infof("Partitions: %d", len(mergedTemplate.Disk.Partitions))
		}
	} else {
		// Validate user template only
		log.Infof("Validating user template: %s", templateFile)

		template, err := config.LoadTemplate(templateFile, false)
		if err != nil {
			return fmt.Errorf("validation failed: %v", err)
		}

		log.Info("✓ Template validation passed")
		log.Infof("Template: %s (type: %s, os: %s/%s/%s)",
			template.Image.Name,
			template.Target.ImageType,
			template.Target.OS,
			template.Target.Dist,
			template.Target.Arch)

		// Show details about user configuration
		log.Infof("System Config: %s", template.SystemConfig.Name)
		log.Infof("   User Packages: %d", len(template.GetPackages()))
		if len(template.GetUsers()) > 0 {
			log.Infof("   Configured users: %d", len(template.SystemConfig.Users))
		} else {
			log.Infof("   No configured users")
		}
		if template.GetKernel().Version != "" {
			log.Infof("   Kernel: %s", template.GetKernel().Version)
		} else {
			log.Infof("   No kernel specified")
		}
	}

	return nil
}
