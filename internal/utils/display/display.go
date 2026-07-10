package display

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// PrintImageDirectorySummary displays all image artifacts in a directory
// This is called after image build completes to show all generated files
func PrintImageDirectorySummary(
	imageBuildDir,
	imageType string,
) {
	log := logger.Logger()

	log.Infof("Checking for image artifacts in: %s", imageBuildDir)

	// List all files in the directory (excluding SBOM)
	files, err := os.ReadDir(imageBuildDir)
	if err != nil {
		log.Warnf("Unable to read image build directory %s: %v", imageBuildDir, err)
		return
	}

	log.Infof("Found %d total entries in directory", len(files))

	// Collect all files (including SBOM)
	var imageFiles []string
	for _, file := range files {
		name := file.Name()
		log.Infof("Checking file: %s (isDir=%v)", name, file.IsDir())

		if file.IsDir() {
			continue
		}
		imageFiles = append(imageFiles, name)
	}

	log.Infof("Found %d artifacts after filtering", len(imageFiles))

	if len(imageFiles) == 0 {
		log.Warn("No artifacts found in build directory")
		return
	}

	// Print highlighted box with success message
	log.Info("")
	log.Info("╔════════════════════════════════════════════════════════════════════════════╗")
	log.Info("║                    ✓ IMAGE CREATED SUCCESSFULLY                            ║")
	log.Info("╚════════════════════════════════════════════════════════════════════════════╝")
	log.Info("")

	// Print image type
	log.Infof("  Image Type:   %s", imageType)
	log.Info("")
	log.Info("  Generated Artifacts (including SBOM):")

	// Print each artifact with size
	for _, filename := range imageFiles {
		fullPath := filepath.Join(imageBuildDir, filename)
		fileInfo, err := os.Stat(fullPath)
		var sizeStr string
		if err == nil {
			sizeMB := float64(fileInfo.Size()) / (1024 * 1024)
			if sizeMB > 1024 {
				sizeStr = fmt.Sprintf("%.2f GB", sizeMB/1024)
			} else {
				sizeStr = fmt.Sprintf("%.2f MB", sizeMB)
			}
		} else {
			sizeStr = "unknown"
		}

		log.Infof("    • %s (%s)", filename, sizeStr)
		log.Infof("      %s", fullPath)
		log.Info("")
	}

	log.Info("════════════════════════════════════════════════════════════════════════════")
	log.Info("")
}

// TimingRow is one labelled stage duration in a timing table.
type TimingRow struct {
	Stage    string
	Duration time.Duration
}

// PrintImageBuildingTiming displays timing information
// for each stage of the (create-mode) image build process.
func PrintImageBuildingTiming(
	imageType string,
	startToDownloadImagePkgsTime,
	downloadImagePkgsTime,
	chrootPkgDownloadTime,
	downloadImagePkgsToPureBuildTime,
	pureImageBuildTime,
	convertImageTime time.Duration,
	convertImageFileToFinishTime time.Duration,
) {
	PrintTimingTable("Build Timings", []TimingRow{
		{Stage: "Initialization and Configuration", Duration: startToDownloadImagePkgsTime},
		{Stage: "Package Download", Duration: downloadImagePkgsTime},
		{Stage: "Chroot Package Download", Duration: chrootPkgDownloadTime},
		{Stage: "Chroot Env Initialization", Duration: downloadImagePkgsToPureBuildTime},
		{Stage: "Image Build", Duration: pureImageBuildTime},
		{Stage: "Image Conversion", Duration: convertImageTime},
		{Stage: "Finalization and Clean Up", Duration: convertImageFileToFinishTime},
	})
}

// PrintTimingTable renders a per-stage timing table with a "Total Time" footer
// summing the rows. The title labels the table (e.g. "Build Timings"); it is a
// no-op when there are no rows.
func PrintTimingTable(title string, rows []TimingRow) {
	if len(rows) == 0 {
		return
	}
	log := logger.Logger()

	var totalDuration time.Duration
	for _, row := range rows {
		totalDuration += row.Duration
	}

	stageWidth := len("Stage")
	durationWidth := len("Duration")
	durationStrings := make([]string, len(rows))
	totalDurationText := totalDuration.Round(time.Millisecond).String()
	for i, row := range rows {
		durationText := row.Duration.Round(time.Millisecond).String()
		durationStrings[i] = durationText
		if len(row.Stage) > stageWidth {
			stageWidth = len(row.Stage)
		}
		if len(durationText) > durationWidth {
			durationWidth = len(durationText)
		}
	}
	if stageWidth < 20 {
		stageWidth = 20
	}
	if durationWidth < 14 {
		durationWidth = 14
	}
	if len("Total Time") > stageWidth {
		stageWidth = len("Total Time")
	}
	if len(totalDurationText) > durationWidth {
		durationWidth = len(totalDurationText)
	}

	border := fmt.Sprintf("  +-%s-+-%s-+", strings.Repeat("-", stageWidth), strings.Repeat("-", durationWidth))

	log.Infof("  %s:", title)
	log.Info(border)
	log.Infof("  | %-*s | %-*s |", stageWidth, "Stage", durationWidth, "Duration")
	log.Info(border)
	for i, row := range rows {
		log.Infof("  | %-*s | %-*s |", stageWidth, row.Stage, durationWidth, durationStrings[i])
	}
	log.Info(border)
	log.Infof("  | %-*s | %-*s |", stageWidth, "Total Time", durationWidth, totalDurationText)
	log.Info(border)
}
