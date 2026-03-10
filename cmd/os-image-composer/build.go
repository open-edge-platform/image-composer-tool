package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/provider"
	"github.com/open-edge-platform/os-image-composer/internal/provider/azl"
	"github.com/open-edge-platform/os-image-composer/internal/provider/elxr"
	"github.com/open-edge-platform/os-image-composer/internal/provider/emt"
	"github.com/open-edge-platform/os-image-composer/internal/provider/rcd"
	"github.com/open-edge-platform/os-image-composer/internal/provider/ubuntu"
	"github.com/open-edge-platform/os-image-composer/internal/utils/display"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/system"
	"github.com/spf13/cobra"
)

// Build command flags
var (
	workers            int    = -1 // -1 means use config file value
	cacheDir           string = "" // Empty means use config file value
	workDir            string = "" // Empty means use config file value
	dotFile            string = "" // Generate a dot file for the dependency graph
	systemPackagesOnly bool   = false
)

// createBuildCommand creates the build subcommand
func createBuildCommand() *cobra.Command {
	buildCmd := &cobra.Command{
		Use:   "build [flags] TEMPLATE_FILE",
		Short: "Build a Linux distribution image",
		Long: `Build a Linux distribution image based on the specified image template file.
The template file must be in YAML format following the image template schema.`,
		Args:              cobra.ExactArgs(1),
		RunE:              executeBuild,
		ValidArgsFunction: templateFileCompletion,
	}

	// Add flags
	buildCmd.Flags().IntVarP(&workers, "workers", "w", -1,
		"Number of concurrent download workers")
	buildCmd.Flags().StringVarP(&cacheDir, "cache-dir", "d", "",
		"Package cache directory")
	buildCmd.Flags().StringVar(&workDir, "work-dir", "",
		"Working directory for builds")
	buildCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	buildCmd.Flags().StringVarP(&dotFile, "dotfile", "f", "", "Generate a dot file for the dependency graph")
	buildCmd.Flags().BoolVar(&systemPackagesOnly, "system-packages-only", false, "When generating a dot graph, only include roots from SystemConfig.Packages")

	return buildCmd
}

// executeBuild handles the build command execution logic
func executeBuild(cmd *cobra.Command, args []string) error {
	// Parse command-line flags and override global config
	// Note: We update the global singleton with any overrides
	if cmd.Flags().Changed("workers") {
		currentConfig := config.Global()
		currentConfig.Workers = workers
		config.SetGlobal(currentConfig)
	}
	if cmd.Flags().Changed("cache-dir") {
		currentConfig := config.Global()
		currentConfig.CacheDir = cacheDir
		config.SetGlobal(currentConfig)
	}
	if cmd.Flags().Changed("work-dir") {
		currentConfig := config.Global()
		currentConfig.WorkDir = workDir
		config.SetGlobal(currentConfig)
	}

	var buildErr error
	log := logger.Logger()

	// Check if template file is provided as first positional argument
	if len(args) < 1 {
		return fmt.Errorf("no template file provided, usage: os-image-composer build [flags] TEMPLATE_FILE")
	}
	templateFile := args[0]

	// get start time
	startTime := time.Now()

	// Load user template and merge with default configuration
	template, err := config.LoadAndMergeTemplate(templateFile)
	if err != nil {
		return fmt.Errorf("loading and merging template: %v", err)
	}
	template.DotSystemOnly = systemPackagesOnly

	// assign start time to storage
	template.StartBuildTimeline(startTime)

	if dotFile != "" {
		dotFilePath, err := filepath.Abs(dotFile)
		if err != nil {
			return fmt.Errorf("resolving dotfile path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(dotFilePath), 0755); err != nil {
			return fmt.Errorf("preparing dotfile directory: %w", err)
		}
		template.DotFilePath = dotFilePath
		log.Infof("Dependency graph will be written to %s", dotFilePath)
	}

	p, err := InitProvider(template.Target.OS, template.Target.Dist, template.Target.Arch)
	if err != nil {
		buildErr = fmt.Errorf("initializing provider failed: %v", err)
		goto post
	}

	if err := p.PreProcess(template); err != nil {
		buildErr = fmt.Errorf("pre-processing failed: %v", err)
		goto post
	}

	template.StartPureImageBuildTimer()
	if err := p.BuildImage(template); err != nil {
		buildErr = fmt.Errorf("image build failed: %v", err)
		goto post
	}

post:

	if p != nil {
		if err := p.PostProcess(template, buildErr); err != nil {
			return fmt.Errorf("post-processing failed: %v", err)
		}
	}

	if buildErr == nil {
		log.Info("image build completed successfully")
		template.MarkBuildFinished()
		displayImageBuildTiming(template.Target.ImageType, template)
	} else {
		// Avoid logging the full error chain to prevent potential leakage of sensitive data.
		// Log only the error type/category to aid debugging without exposing sensitive details.
		log.Errorf("image build failed (error type: %T)", buildErr)
	}

	return buildErr
}

func displayImageBuildTiming(imageType string, template *config.ImageTemplate) {
	startToDownloadImagePkgsDuration := template.GetDurationStartToDownloadImagePkgs()
	downloadImagePkgsToPureBuildDuration := template.GetDurationDownloadImagePkgsToPureBuild()
	pureImageBuildDuration := template.GetPureImageBuildDuration()
	downloadImagePkgsDuration := template.GetDownloadImagePkgsDuration()
	convertImageDuration := template.GetConvertImageDuration()
	convertImageFileToFinishDuration := template.GetDurationConvertImageFileToFinish()
	display.PrintImageBuildingTiming(
		imageType,
		startToDownloadImagePkgsDuration,
		downloadImagePkgsDuration,
		downloadImagePkgsToPureBuildDuration,
		pureImageBuildDuration,
		convertImageDuration,
		convertImageFileToFinishDuration,
	)
}

func InitProvider(os, dist, arch string) (provider.Provider, error) {
	var p provider.Provider
	switch os {
	case azl.OsName:
		if err := azl.Register(os, dist, arch); err != nil {
			return nil, fmt.Errorf("registering azl provider failed: %v", err)
		}
	case emt.OsName:
		if err := emt.Register(os, dist, arch); err != nil {
			return nil, fmt.Errorf("registering emt provider failed: %v", err)
		}
	case elxr.OsName:
		if err := elxr.Register(os, dist, arch); err != nil {
			return nil, fmt.Errorf("registering elxr provider failed: %v", err)
		}
	case ubuntu.OsName:
		if err := ubuntu.Register(os, dist, arch); err != nil {
			return nil, fmt.Errorf("registering ubuntu provider failed: %v", err)
		}
	case rcd.OsName:
		if err := rcd.Register(os, dist, arch); err != nil {
			return nil, fmt.Errorf("registering rcd provider failed: %v", err)
		}
	default:
		return nil, fmt.Errorf("unsupported provider: %s", os)
	}
	providerId := system.GetProviderId(os, dist, arch)
	p, ok := provider.Get(providerId)
	if !ok {
		return nil, fmt.Errorf("provider not found for %s %s %s", os, dist, arch)
	}
	return p, p.Init(dist, arch)
}

// templateFileCompletion helps with suggesting YAML files for template file argument
func templateFileCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"*.yml", "*.yaml"}, cobra.ShellCompDirectiveFilterFileExt
}
