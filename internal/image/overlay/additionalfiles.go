package overlay

import (
	"fmt"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
)

// copyAdditionalFileFn is the seam over the host-side file copy so the
// additional-files orchestration is unit-testable without root or a real mounted
// baseline. Production uses file.CopyFile (host `cp` under sudo, which also creates
// the destination directory); tests override it.
var copyAdditionalFileFn = file.CopyFile

// RunOverlayAdditionalFiles copies the template's systemConfig.additionalFiles into
// the mounted baseline root, mirroring create mode's addImageAdditionalFiles so an
// overlay build honors the same "drop these host files into the image" contract.
//
// Each entry's Local path is copied to <rootMount>/<Final>. GetAdditionalFileInfo
// has already resolved every Local path (absolute, or relative to the template
// search paths) and dropped entries whose source is missing or whose Local/Final is
// empty, so this stage copies exactly the entries that survived that check. The copy
// is host-side (no chroot is entered): rootMount is the host path at which the
// baseline root is mounted, and file.CopyFile creates the destination directory and
// preserves mode/ownership/timestamps via `cp -p`.
//
// Unlike create mode, overlay mode does NOT auto-inject apt source/preferences/GPG
// files into additionalFiles: GenerateAptSourcesFromRepositories runs only on the
// create-mode branch (the overlay preprocess path returns before it), and overlay
// installs from prepared artifacts rather than live apt repositories. So in overlay
// mode additionalFiles carries only user-authored entries, and every one is copied
// verbatim — there is nothing tool-injected to filter out.
//
// Copy timing is per-file, controlled by each entry's Stage marker:
//
//   - Stage "" (default) copies LAST in the build, AFTER initramfs and GRUB
//     regeneration. That is deliberate: a user who ships a prebuilt boot artifact
//     (e.g. a custom /boot/initrd.img) needs it to land after regeneration, or
//     update-initramfs would overwrite it. This is the historical behavior, so
//     existing templates are unaffected.
//   - Stage "pre-initramfs" copies BEFORE boot/initramfs regeneration, so content
//     the initramfs generator consumes — a dracut module under /usr/lib/dracut, an
//     initramfs-tools hook — is in place when the initramfs is (re)built. Without
//     this such a file, dropped at the end, would be ignored by the already-built
//     initramfs.
//
// RunOverlayAdditionalFiles copies only the entries matching the requested stage,
// so the Build pipeline invokes it once before regeneration (pre-initramfs) and
// once after (default). It is a no-op when no entry matches the stage, so builds
// that do not use the feature — or a stage — pay nothing for it.
func RunOverlayAdditionalFiles(template *config.ImageTemplate, rootMount string, stage string) error {
	if template == nil {
		return fmt.Errorf("overlay additional files: template cannot be nil")
	}
	if strings.TrimSpace(rootMount) == "" {
		return fmt.Errorf("overlay additional files: baseline root mount path cannot be empty")
	}

	// GetAdditionalFileInfo already resolved Local to an existing path and
	// guaranteed Final is non-empty; select only the entries for this stage.
	var additionalFiles []config.AdditionalFileInfo
	for _, fileInfo := range template.GetAdditionalFileInfo() {
		if additionalFileStage(fileInfo) == stage {
			additionalFiles = append(additionalFiles, fileInfo)
		}
	}
	if len(additionalFiles) == 0 {
		log.Debugf("Overlay additional files: none to copy for stage %q", stageLabel(stage))
		return nil
	}

	log.Infof("Overlay additional files: copying %d %s file(s) into %s", len(additionalFiles), stageLabel(stage), rootMount)

	for _, fileInfo := range additionalFiles {
		// The baseline is UNTRUSTED and the copy runs under sudo via `cp` (which follows
		// destination symlinks). Two escapes must be blocked before copying:
		//   1. `final: ../../etc/passwd` — filepath.Join would let the destination climb
		//      out of rootMount. Confine the cleaned in-image path under rootMount and
		//      reject any entry whose Final escapes it.
		//   2. a symlinked destination component (e.g. baseline made /etc a symlink to a
		//      host dir) — reject a symlink anywhere in the destination chain so the
		//      elevated copy cannot be redirected onto a host path.
		dstFile, derr := safeChrootDest(rootMount, fileInfo.Final)
		if derr != nil {
			return fmt.Errorf("overlay additional files: unsafe destination %q: %w", fileInfo.Final, derr)
		}
		if err := assertNoSymlinkInChrootPath(rootMount, fileInfo.Final); err != nil {
			return fmt.Errorf("overlay additional files: unsafe destination %q: %w", fileInfo.Final, err)
		}
		if err := copyAdditionalFileFn(fileInfo.Local, dstFile, "-p", true); err != nil {
			return fmt.Errorf("overlay additional files: failed to copy %s to %s: %w", fileInfo.Local, fileInfo.Final, err)
		}
		log.Debugf("Overlay additional files: copied %s -> %s", fileInfo.Local, dstFile)
	}

	log.Infof("Overlay additional files: %d %s file(s) copied", len(additionalFiles), stageLabel(stage))
	return nil
}

// additionalFileStage normalizes an entry's Stage marker. An unset stage maps to
// the default (end-of-build) stage, preserving historical behavior.
func additionalFileStage(f config.AdditionalFileInfo) string {
	return strings.TrimSpace(f.Stage)
}

// stageLabel renders a stage marker for logs, naming the default explicitly.
func stageLabel(stage string) string {
	if stage == config.AdditionalFileStageDefault {
		return "default-stage"
	}
	return stage
}
