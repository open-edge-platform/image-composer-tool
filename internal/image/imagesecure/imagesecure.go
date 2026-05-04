package imagesecure

import (
	"strings"

	"fmt"

	"path/filepath"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func ConfigImageSecurity(installRoot string, template *config.ImageTemplate) error {

	log := logger.Logger()

	// 0. Check if immutability is enabled
	if !template.IsImmutabilityEnabled() {
		return nil
	}

	//Mounting overlay for read write directories
	if err := configOverlay(installRoot); err != nil {
		return fmt.Errorf("failed to configure overlay: %w", err)
	}

	log.Debugf("Root filesystem made read-only successfully")
	return nil
}

func configOverlay(installRoot string) error {

	log := logger.Logger()
	log.Debugf("Configuring overlay for read-only root filesystem is not implemented yet")

	overlayDirs := []string{
		"/etc",
		"/home",
		"/var",
	}

	ovlyDir, err := prepareOverlayDir(installRoot, overlayDirs)
	if err != nil {
		return fmt.Errorf("failed to prepare ESP directory: %w", err)
	}
	log.Debugf("Succesfully Creating Overlay Path:", ovlyDir)

	err = updateImageFstab(installRoot, overlayDirs)
	if err != nil {
		return fmt.Errorf("failed to update fstab: %w", err)
	}
	log.Debugf("Succesfully Updating fstab for overlay")

	err = createOverlayMntSvc(installRoot, overlayDirs)
	if err != nil {
		return fmt.Errorf("failed to create overlay mounting service: %w", err)
	}
	log.Debugf("Succesfully Created overlay mounting service")

	return nil
}

func createOverlayMntSvc(installRoot string, overlayDirs []string) error {
	log := logger.Logger()

	scriptLines := []string{
		"#!/bin/bash",
		"",
		"if [ ! -d /opt/overlay/etc/upper ]; then",
		"    echo \"Missing /opt/overlay/etc/upper\"",
		"    exit 1",
		"fi",
		"",
		"if [ ! -d /ro/etc ]; then",
		"    echo \"Missing /ro/etc\"",
		"    exit 1",
		"fi",
		"",
		"# Bind mount rootfs /etc to lowerdir",
		"mount --bind /etc /ro/etc",
		"",
		"# Mount overlay",
		"mount -t overlay overlay -o lowerdir=/ro/etc,upperdir=/opt/overlay/etc/upper,workdir=/opt/overlay/etc/work /etc",
		"",
		"# Bind-mount persistent /var and /home",
		// "mount --bind /opt/var /var",
		// "mount --bind /opt/home /home",
	}
	//adding overlay directories to the list
	for _, dir := range overlayDirs {
		if dir == "/etc" {
			continue // handle /etc separately
		}
		// Remove leading slash for opt subdir
		base := strings.TrimPrefix(dir, "/")
		mntLn := "mount --bind /opt/" + base + " " + dir
		// Check if overlayDir is already in dirs
		exists := false
		for _, d := range scriptLines {
			if d == mntLn {
				exists = true
				break
			}
		}
		if !exists {
			scriptLines = append(scriptLines, mntLn)
		}
	}
	scriptContent := strings.Join(scriptLines, "\n") + "\n"

	mountScriptPath := filepath.Join(installRoot, "usr", "local", "bin", "setup-overlay.sh")
	err := file.Append(scriptContent, mountScriptPath)
	if err != nil {
		return fmt.Errorf("failed to append to mountScriptPath: %w", err)
	}

	//grant execute permissions to the script
	chmodCmd := fmt.Sprintf("chmod -R 755 %s", filepath.Dir(mountScriptPath))
	if _, err = shell.ExecCmd(chmodCmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to set permissions for overlay mounting script: %w", err)
	}

	//create service
	svcLines := []string{
		"[Unit]",
		"Description=Set up OverlayFS for /etc",
		"Requires=opt.mount",
		"After=opt.mount",
		"",
		"[Service]",
		"Type=oneshot",
		"ExecStart=/usr/local/bin/setup-overlay.sh",
		"RemainAfterExit=true",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
	}
	svcContent := strings.Join(svcLines, "\n") + "\n"

	svcPath := filepath.Join(installRoot, "etc", "systemd", "system", "setup-overlay.service")
	err = file.Append(svcContent, svcPath)
	if err != nil {
		return fmt.Errorf("failed to append to service file: %w", err)
	}
	// Enable the overlay mounting service
	enableCmd := `bash -c "systemctl enable setup-overlay.service"`
	if _, err = shell.ExecCmd(enableCmd, true, installRoot, nil); err != nil {
		return fmt.Errorf("failed to enable overlay mounting service: %w", err)
	}
	log.Debugf("Updated mountScriptPath with overlay settings")
	return nil
}

func updateImageFstab(installRoot string, overlayDirs []string) error {
	log := logger.Logger()

	lines := []string{
		"", // An empty string for the blank line
		// "/opt/var /var none bind 0 0",
		// "/opt/home /home none bind 0 0",
		"", // An empty string for the blank line
		"tmpfs /tmp tmpfs mode=1777,nosuid,nodev 0 0",
		"tmpfs /run tmpfs mode=0755,nosuid,nodev 0 0",
	}

	//adding overlay directories to the list
	for _, dir := range overlayDirs {
		if dir == "/etc" {
			continue // handle /etc separately
		}
		// Remove leading slash for opt subdir
		base := strings.TrimPrefix(dir, "/")
		overlayDir := "/opt/" + base + " " + dir + " none bind 0 0"
		// Check if overlayDir is already in dirs
		exists := false
		for _, d := range lines {
			if d == overlayDir {
				exists = true
				break
			}
		}
		if !exists {
			lines = append(lines, overlayDir)
		}
	}
	contentToAppend := strings.Join(lines, "\n") + "\n" // Add a final newline if needed

	fstabFullPath := filepath.Join(installRoot, "etc", "fstab")
	err := file.Append(contentToAppend, fstabFullPath)
	if err != nil {
		return fmt.Errorf("failed to append to fstab: %w", err)
	}

	log.Debugf("Updated fstab with overlay settings")
	return nil
}

func prepareOverlayDir(installRoot string, overlayDirs []string) (string, error) {

	dirs := []string{
		"/opt/overlay/etc/",
		"/opt/overlay/etc/upper",
		"/opt/overlay/etc/work",
		"/ro/etc",
		// "/opt/var",
		// "/opt/home",
	}

	//adding overlay directories to the list
	for _, dir := range overlayDirs {
		if dir == "/etc" {
			continue // handle /etc separately
		}
		// Remove leading slash for opt subdir
		base := strings.TrimPrefix(dir, "/")
		overlayDir := "/opt/" + base
		// Check if overlayDir is already in dirs
		exists := false
		for _, d := range dirs {
			if d == overlayDir {
				exists = true
				break
			}
		}
		if !exists {
			dirs = append(dirs, overlayDir)
		}
	}

	// Create required overlay directories
	for _, dir := range dirs {
		cmd := fmt.Sprintf("mkdir -p %s", dir)
		if _, err := shell.ExecCmd(cmd, true, installRoot, nil); err != nil {
			return "", err
		}
	}

	// Return the overlay directory
	return dirs[0], nil
}
