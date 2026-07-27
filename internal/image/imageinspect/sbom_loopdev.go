package imageinspect

import (
	"fmt"
	"os"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
)

// loopDevAttacher is the slice of the loop-device API this reader needs. It is
// an interface (rather than *imagedisc.LoopDev directly) so tests can inject a
// fake and exercise partition-node mapping without touching a real disk. The
// unregister closure returned by AttachImageToLoopDev removes the auto-registered
// cleanup-coordinator entry for this device; callers invoke it AFTER a
// successful LoopSetupDelete, not before — see imagedisc.loopSetupCreate's
// docstring for the ordering contract (a failed detach must leave the coord
// entry registered so the build.go cancel backstop can retry).
type loopDevAttacher interface {
	AttachImageToLoopDev(imagePath string) (string, []string, func(), error)
	LoopSetupDelete(loopDevPath string) error
}

// newLoopDevForSBOM constructs the loop-device backend. Overridable in tests.
var newLoopDevForSBOM = func() loopDevAttacher { return imagedisc.NewLoopDev() }

// geteuidForSBOM reports the effective UID. Overridable in tests so the
// requires-root branch can be exercised without actually dropping privileges.
var geteuidForSBOM = os.Geteuid

// inspectSBOMFromImageLoopDev extracts the embedded SBOM from an ext-family root
// by attaching the image to a loop device (losetup -P) and running debugfs
// directly against the partition node — reading the SBOM in place with NO temp
// copy. This is the path for real multi-GB ext4 roots that go-diskfs cannot walk
// and that are too large to copy to /tmp for the raw/debugfs fallback.
//
// It reads only: debugfs is invoked read-only (no -w), so the baseline image's
// filesystem — including any package-manager database — is never mutated. The
// loop device is always detached before returning.
//
// It requires root (losetup); when not root, or when no ext root candidate
// exists, it records why in Notes and returns Present=false so the caller can
// fall through to the next strategy.
func inspectSBOMFromImageLoopDev(imagePath string, pt PartitionTableSummary) SBOMSummary {
	summary := SBOMSummary{Format: "spdx"}

	rootCandidates := rankRootPartitionCandidates(pt)
	if len(rootCandidates) == 0 {
		summary.Notes = append(summary.Notes, "SBOM root partition candidate not found")
		return summary
	}

	// debugfs can only read ext2/3/4; there is no point paying the cost of a
	// privileged loop attach if no candidate is an ext filesystem.
	if !hasExtRootCandidate(pt, rootCandidates) {
		summary.Notes = append(summary.Notes,
			"loop-device SBOM read skipped: no ext2/3/4 root candidate")
		return summary
	}

	if geteuidForSBOM() != 0 {
		summary.Notes = append(summary.Notes,
			"loop-device SBOM read skipped: requires root (re-run under sudo)")
		return summary
	}

	loop := newLoopDevForSBOM()
	loopDevPath, partitionNodes, unregister, err := loop.AttachImageToLoopDev(imagePath)
	if err != nil {
		summary.Notes = append(summary.Notes, fmt.Sprintf("loop-device attach failed: %v", err))
		return summary
	}
	defer func() {
		// Detach first; unregister only on success. If detach fails (busy
		// partition or short-circuited shell ctx on a cancelled build that
		// went through SBOM inspection), leaving the coord entry registered
		// lets build.go's cancel backstop retry the detach.
		if detachErr := loop.LoopSetupDelete(loopDevPath); detachErr != nil {
			summary.Notes = append(summary.Notes,
				fmt.Sprintf("loop-device detach failed for %s: %v", loopDevPath, detachErr))
			return
		}
		unregister()
	}()

	for _, candidateIndex := range rootCandidates {
		partitionSummary := pt.Partitions[candidateIndex]
		fsType := ""
		if partitionSummary.Filesystem != nil {
			fsType = strings.ToLower(strings.TrimSpace(partitionSummary.Filesystem.Type))
		}
		if fsType != "ext4" && fsType != "ext3" && fsType != "ext2" {
			continue
		}

		partitionNode, ok := partitionNodeForIndex(partitionNodes, partitionSummary.Index)
		if !ok {
			summary.Notes = append(summary.Notes,
				fmt.Sprintf("partition %d (%s): no loop partition node found among %v",
					partitionSummary.Index, partitionSummary.Name, partitionNodes))
			continue
		}

		// debugfs reads the block device in place. These helpers already run
		// debugfs read-only against a filesystem-aligned path; a loop partition
		// node is exactly that, so no partition copy is needed.
		sbomFileName, sbomPath, findErr := findSBOMFileInExtPartitionImage(partitionNode)
		if findErr != nil {
			summary.Notes = append(summary.Notes,
				fmt.Sprintf("partition %d (%s): %v",
					partitionSummary.Index, partitionSummary.Name, findErr))
			continue
		}

		content, readErr := readFileFromExtPartitionImage(partitionNode, sbomPath)
		if readErr != nil {
			summary.Notes = append(summary.Notes,
				fmt.Sprintf("partition %d (%s): read SBOM %s: %v",
					partitionSummary.Index, partitionSummary.Name, sbomPath, readErr))
			continue
		}

		summary.Present = true
		summary.Path = sbomPath
		summary.FileName = sbomFileName
		summary.SizeBytes = int64(len(content))
		summary.SHA256 = sha256Hex(content)
		summary.Content = append([]byte(nil), content...)

		canonicalSHA, pkgCount, canonicalErr := canonicalSPDXSHA256(content)
		if canonicalErr != nil {
			summary.Notes = append(summary.Notes, "SBOM SPDX parse failed; compare falls back to raw hash")
			return summary
		}
		summary.CanonicalSHA256 = canonicalSHA
		summary.PackageCount = pkgCount
		return summary
	}

	summary.Notes = append(summary.Notes, "SBOM not found at /usr/share/sbom")
	return summary
}

// hasExtRootCandidate reports whether any ranked root candidate is an ext2/3/4
// filesystem — the only kind debugfs (and therefore the loop path) can read.
func hasExtRootCandidate(pt PartitionTableSummary, candidates []int) bool {
	for _, candidateIndex := range candidates {
		fs := pt.Partitions[candidateIndex].Filesystem
		if fs == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(fs.Type)) {
		case "ext4", "ext3", "ext2":
			return true
		}
	}
	return false
}

// partitionNodeForIndex returns the loop partition node whose partition number
// matches the 1-based partition index, e.g. index 3 -> "/dev/loop0p3". losetup
// -P names nodes by partition-table number, which is the same 1-based ordering
// summarizePartitionTable assigns, so the suffix match is exact.
func partitionNodeForIndex(nodes []string, index int) (string, bool) {
	suffix := fmt.Sprintf("p%d", index)
	for _, node := range nodes {
		if strings.HasSuffix(node, suffix) {
			return node, true
		}
	}
	return "", false
}
