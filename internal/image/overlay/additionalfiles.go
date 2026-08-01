package overlay

import (
	"fmt"
	"path/filepath"
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
// This stage runs LAST in the build, AFTER initramfs and GRUB regeneration. That is
// deliberate: a user who ships a prebuilt boot artifact (e.g. a custom
// /boot/initrd.img) needs it to land after regeneration, or update-initramfs would
// overwrite it. Files that must instead be PROCESSED by the generator (initramfs
// hooks under /etc/initramfs-tools) belong in a configurations command that also
// runs the generator, which executes before boot regeneration. The division is:
// additionalFiles delivers final files authoritatively; configurations runs commands
// that may trigger regeneration.
//
// It is a no-op when the template declares no additional files, so builds that do
// not use the feature pay nothing for it.
func RunOverlayAdditionalFiles(template *config.ImageTemplate, rootMount string) error {
	if template == nil {
		return fmt.Errorf("overlay additional files: template cannot be nil")
	}
	if strings.TrimSpace(rootMount) == "" {
		return fmt.Errorf("overlay additional files: baseline root mount path cannot be empty")
	}

	additionalFiles := template.GetAdditionalFileInfo()
	if len(additionalFiles) == 0 {
		log.Debug("Overlay additional files: none to copy")
		return nil
	}

	log.Infof("Overlay additional files: copying %d file(s) into %s", len(additionalFiles), rootMount)

	for _, fileInfo := range additionalFiles {
		// GetAdditionalFileInfo already resolved Local to an existing path and
		// guaranteed Final is non-empty, so copy directly. Final is joined onto the
		// mount so it lands at the intended in-image path.
		dstFile := filepath.Join(rootMount, fileInfo.Final)
		if err := copyAdditionalFileFn(fileInfo.Local, dstFile, "-p", true); err != nil {
			return fmt.Errorf("overlay additional files: failed to copy %s to %s: %w", fileInfo.Local, fileInfo.Final, err)
		}
		log.Debugf("Overlay additional files: copied %s -> %s", fileInfo.Local, dstFile)
	}

	log.Infof("Overlay additional files: %d file(s) copied", len(additionalFiles))
	return nil
}
