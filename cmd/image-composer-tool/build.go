package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/isomaker"
	"github.com/open-edge-platform/image-composer-tool/internal/provider"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/azl"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/debian13"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/elxr"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/emt"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/rcd"
	"github.com/open-edge-platform/image-composer-tool/internal/provider/ubuntu"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/display"
	fileutil "github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
	"github.com/spf13/cobra"
)

// defaultWorkers is the default number of concurrent download workers from config
var defaultWorkers = config.DefaultGlobalConfig().Workers

// Build command flags
var (
	workers            int    = defaultWorkers
	cacheDir           string = "" // Empty means use config file value
	workDir            string = "" // Empty means use config file value
	dotFile            string = "" // Generate a dot file for the dependency graph
	systemPackagesOnly bool   = false
	noCache            bool   = false // Build from scratch in fresh, unique cache/workspace dirs

	// Overlay-mode flags.
	inspectImage  bool   = true  // --inspect/--no-inspect: toggle image inspection (default on)
	cveCheck      bool   = false // --cve-check: enable CVE analysis from the CLI
	baselineImage string = ""    // --baseline-image: override baseline.source.path from the template
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
	buildCmd.Flags().IntVarP(&workers, "workers", "w", defaultWorkers,
		"Number of concurrent download workers, overrides config file value")
	buildCmd.Flags().StringVarP(&cacheDir, "cache-dir", "d", "",
		"Package cache directory")
	buildCmd.Flags().StringVar(&workDir, "work-dir", "",
		"Working directory for builds")
	buildCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	buildCmd.Flags().StringVarP(&dotFile, "dotfile", "f", "", "Generate a dot file for the dependency graph")
	buildCmd.Flags().BoolVar(&systemPackagesOnly, "system-packages-only", false, "When generating a dot graph, only include roots from SystemConfig.Packages")
	buildCmd.Flags().BoolVar(&noCache, "nocache", false,
		"Build from scratch using fresh, unique cache and workspace directories that are removed after the build (the final image is copied to the configured workspace)")

	// Overlay-mode flags. --inspect defaults on; --no-inspect is its negation.
	buildCmd.Flags().BoolVar(&inspectImage, "inspect", true, "Inspect the image during overlay builds (use --no-inspect to disable)")
	buildCmd.Flags().Bool("no-inspect", false, "Disable image inspection during overlay builds (overrides --inspect)")
	buildCmd.Flags().BoolVar(&cveCheck, "cve-check", false, "Enable CVE analysis of the built image")
	buildCmd.Flags().StringVar(&baselineImage, "baseline-image", "", "Override baseline.source.path from the template (overlay mode)")

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

	// When --nocache is set, run the build in fresh, unique cache and workspace
	// directories so nothing is reused from previous builds, then remove them once the
	// build finishes. The final image is copied back to the configured workspace.
	var noCacheContext *noCacheContext
	if noCache {
		if cmd.Flags().Changed("cache-dir") || cmd.Flags().Changed("work-dir") {
			return fmt.Errorf("--nocache cannot be combined with --cache-dir or --work-dir")
		}
		createdContext, cleanup, setupErr := setupNoCache()
		if setupErr != nil {
			return fmt.Errorf("setting up --nocache directories: %w", setupErr)
		}
		defer cleanup()
		noCacheContext = createdContext
	}

	var buildErr error
	log := logger.Logger()

	// Check if template file is provided as first positional argument
	if len(args) < 1 {
		return fmt.Errorf("no template file provided, usage: image-composer-tool build [flags] TEMPLATE_FILE")
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

	// Apply overlay-mode CLI flags onto the loaded template. CLI values take
	// precedence over template values.
	if err := applyOverlayFlagOverrides(cmd, template); err != nil {
		return err
	}

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

	// For ISO builds, validate prerequisites (e.g., live-installer binary)
	// before starting expensive provider init and package downloads
	if template.Target.ImageType == "iso" {
		if err := isomaker.ValidateISOPrerequisites(template); err != nil {
			return fmt.Errorf("ISO prerequisites check failed: %w", err)
		}
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
			// In --nocache mode the deferred cleanup would otherwise remove the unique
			// workspace on return, discarding a successfully built image. Preserve it so
			// the image (and any state needed for recovery) survives a PostProcess failure.
			if noCacheContext != nil {
				noCacheContext.keepWorkDir = true
			}
			return fmt.Errorf("post-processing failed: %v", err)
		}
	}

	if buildErr == nil {
		// For --nocache, copy the freshly built image out of the temporary workspace before
		// reporting success: a copy-out failure is a build failure and must not be logged as a
		// completed build. The deferred cleanup removes the temporary workspace on return.
		if noCacheContext != nil {
			providerId := system.GetProviderId(template.Target.OS, template.Target.Dist, template.Target.Arch)
			if err := preserveNoCacheOutput(noCacheContext, providerId, template.GetSystemConfigName()); err != nil {
				noCacheContext.keepWorkDir = true
				return fmt.Errorf("preserving --nocache build output: %w", err)
			}
		}

		log.Info("image build completed successfully")
		template.MarkBuildFinished()
		// Overlay builds do not run through the create-mode stages that populate the
		// build timers, so the create-mode timing table would be all zeros; the
		// overlay provider prints its own per-stage table in postprocess instead.
		if !template.IsOverlayMode() {
			displayImageBuildTiming(template.Target.ImageType, template)
		}
	} else {
		// Avoid logging the full error chain to prevent potential leakage of sensitive data.
		// Log only the error type/category to aid debugging without exposing sensitive details.
		log.Errorf("image build failed (error type: %T)", buildErr)
	}

	return buildErr
}

// applyOverlayFlagOverrides applies the overlay-mode CLI flags onto the loaded
// template. CLI values take precedence over template values. The inspection
// toggle has no YAML representation (it is a yaml:"-" field), so the CLI is its
// only source; --baseline-image overrides baseline.source.path.
func applyOverlayFlagOverrides(cmd *cobra.Command, template *config.ImageTemplate) error {
	// Inspection defaults on. --no-inspect wins over --inspect when both appear.
	template.InspectEnabled = inspectImage
	noInspect, err := cmd.Flags().GetBool("no-inspect")
	if err != nil {
		return fmt.Errorf("failed to read --no-inspect flag: %w", err)
	}
	if noInspect {
		template.InspectEnabled = false
	}

	// --cve-check is accepted by the parser (so it appears in --help and stays a
	// stable CLI surface) but the CVE analysis engine does not exist yet. Fail
	// clearly rather than silently ignoring the flag, until that engine lands.
	if cveCheck {
		return fmt.Errorf("--cve-check is not yet implemented")
	}

	// --baseline-image overrides baseline.source.path. Enforce overlay mode
	// explicitly (not just non-nil source) to match the flag's documented behavior.
	// Clearing URL keeps the "exactly one of path/url" invariant that
	// BaselineSource.Validate enforces (NewIngestor re-validates downstream).
	if cmd.Flags().Changed("baseline-image") {
		if !template.IsOverlayMode() {
			return fmt.Errorf("--baseline-image requires an overlay-mode template (baseline.mode must be %q)", config.BaselineModeOverlay)
		}
		// IsOverlayMode only guarantees Baseline is non-nil; the normal load path
		// also validates Source != nil, but a programmatically-built template may
		// omit it. Materialize an empty source so the override can't panic.
		if template.Baseline.Source == nil {
			template.Baseline.Source = &config.BaselineSource{}
		}
		template.Baseline.Source.Path = baselineImage
		template.Baseline.Source.URL = ""
		if err := template.Baseline.Source.Validate(); err != nil {
			return fmt.Errorf("invalid --baseline-image override: %w", err)
		}
		logger.Logger().Infof("Overriding baseline image path with %s", baselineImage)
	}
	return nil
}

// noCacheContext holds the unique directories created for a --nocache build so the
// final image can be copied out and the directories removed once the build finishes.
type noCacheContext struct {
	uniqueCacheDir string
	uniqueWorkDir  string
	origWorkDir    string
	keepWorkDir    bool // preserve the workspace for recovery when copy-out or PostProcess fails
}

// setupNoCache creates fresh, unique cache and workspace directories adjacent to the
// configured ones, points the global config at them, and returns the context plus a
// cleanup function that removes the unique directories.
func setupNoCache() (*noCacheContext, func(), error) {
	origCacheDir, err := config.CacheDir()
	if err != nil {
		return nil, nil, fmt.Errorf("resolving cache directory: %w", err)
	}
	origWorkDir, err := config.WorkDir()
	if err != nil {
		return nil, nil, fmt.Errorf("resolving work directory: %w", err)
	}

	uniqueCacheDir, err := os.MkdirTemp(filepath.Dir(origCacheDir), "ict-nocache-cache-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating unique cache directory: %w", err)
	}
	uniqueWorkDir, err := os.MkdirTemp(filepath.Dir(origWorkDir), "ict-nocache-workspace-*")
	if err != nil {
		if rmErr := os.RemoveAll(uniqueCacheDir); rmErr != nil {
			logger.Logger().Warnf("failed to remove unique cache directory %s: %v", uniqueCacheDir, rmErr)
		}
		return nil, nil, fmt.Errorf("creating unique workspace directory: %w", err)
	}

	currentConfig := config.Global()
	currentConfig.CacheDir = uniqueCacheDir
	currentConfig.WorkDir = uniqueWorkDir
	config.SetGlobal(currentConfig)

	noCacheContext := &noCacheContext{
		uniqueCacheDir: uniqueCacheDir,
		uniqueWorkDir:  uniqueWorkDir,
		origWorkDir:    origWorkDir,
	}

	log := logger.Logger()
	log.Infof("--nocache: building in fresh cache %s and workspace %s", uniqueCacheDir, uniqueWorkDir)

	cleanup := func() {
		// Restore the original cache/work directories so later code in the same process
		// does not consult the now-removed --nocache directories.
		currentConfig := config.Global()
		currentConfig.CacheDir = origCacheDir
		currentConfig.WorkDir = origWorkDir
		config.SetGlobal(currentConfig)

		if err := os.RemoveAll(uniqueCacheDir); err != nil {
			log.Warnf("failed to remove --nocache cache directory %s: %v", uniqueCacheDir, err)
		}
		if noCacheContext.keepWorkDir {
			log.Infof("--nocache: preserving workspace %s for recovery", uniqueWorkDir)
			return
		}
		if err := os.RemoveAll(uniqueWorkDir); err != nil {
			log.Warnf("failed to remove --nocache workspace directory %s: %v", uniqueWorkDir, err)
		}
	}

	return noCacheContext, cleanup, nil
}

// preserveNoCacheOutput copies the built image directory from the unique --nocache
// workspace back to the originally configured workspace so it survives cleanup.
func preserveNoCacheOutput(noCacheContext *noCacheContext, providerId, configName string) error {
	srcImageDir := filepath.Join(noCacheContext.uniqueWorkDir, providerId, "imagebuild", configName)
	if _, err := os.Stat(srcImageDir); err != nil {
		if os.IsNotExist(err) {
			// No image build directory was produced; nothing to preserve.
			return nil
		}
		return fmt.Errorf("checking image output directory: %w", err)
	}

	// The build runs as root (image files are root-owned) and the copy happens in the
	// same process, so no privilege escalation is needed here.
	dstImageDir := filepath.Join(noCacheContext.origWorkDir, providerId, "imagebuild", configName)
	// Pre-create the destination with 0700 to match the image build directory permissions;
	// CopyDir's mkdir -p would otherwise create it with more permissive defaults.
	if err := os.MkdirAll(dstImageDir, 0700); err != nil {
		return fmt.Errorf("creating image output directory %s: %w", dstImageDir, err)
	}
	if err := fileutil.CopyDir(srcImageDir, dstImageDir, "-p", false); err != nil {
		return fmt.Errorf("copying image output to %s: %w", dstImageDir, err)
	}

	logger.Logger().Infof("--nocache: build output copied to %s", dstImageDir)
	return nil
}

func displayImageBuildTiming(imageType string, template *config.ImageTemplate) {
	startToDownloadImagePkgsDuration := template.GetDurationStartToDownloadImagePkgs()
	chrootPkgDownloadDuration := template.GetChrootPkgDownloadDuration()
	downloadImagePkgsToPureBuildDuration := template.GetDurationDownloadImagePkgsToPureBuild()
	pureImageBuildDuration := template.GetPureImageBuildDuration()
	downloadImagePkgsDuration := template.GetDownloadImagePkgsDuration()
	convertImageDuration := template.GetConvertImageDuration()
	convertImageFileToFinishDuration := template.GetDurationConvertImageFileToFinish()
	display.PrintImageBuildingTiming(
		imageType,
		startToDownloadImagePkgsDuration,
		downloadImagePkgsDuration,
		chrootPkgDownloadDuration,
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
	case debian13.OsName:
		if err := debian13.Register(os, dist, arch); err != nil {
			return nil, fmt.Errorf("registering debian13 provider failed: %v", err)
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
