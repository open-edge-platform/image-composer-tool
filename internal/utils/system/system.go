package system

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

var (
	log           = logger.Logger()
	OsReleaseFile = "/etc/os-release"
)

func GetHostOsInfo() (map[string]string, error) {
	var hostOsInfo = map[string]string{
		"name":    "",
		"version": "",
		"arch":    "",
	}

	// Get architecture using uname command
	output, err := shell.ExecCmd("uname -m", false, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get host architecture: %v", err)
		return hostOsInfo, fmt.Errorf("failed to get host architecture: %w", err)
	} else {
		hostOsInfo["arch"] = strings.TrimSpace(output)
	}

	// Read from /etc/os-release if it exists
	if _, err := os.Stat(OsReleaseFile); err == nil {
		file, err := os.Open(OsReleaseFile)
		if err == nil {
			defer file.Close()
			scanner := bufio.NewScanner(file)

			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "NAME=") {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						hostOsInfo["name"] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
					}
				} else if strings.HasPrefix(line, "VERSION_ID=") {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						hostOsInfo["version"] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
					}
				}
			}

			log.Infof("Detected OS info: " + hostOsInfo["name"] + " " +
				hostOsInfo["version"] + " " + hostOsInfo["arch"])

			return hostOsInfo, nil
		}
	}

	output, err = shell.ExecCmd("lsb_release -si", false, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get host OS name: %v", err)
		return hostOsInfo, fmt.Errorf("failed to get host OS name: %w", err)
	} else {
		if output != "" {
			hostOsInfo["name"] = strings.TrimSpace(output)
			output, err = shell.ExecCmd("lsb_release -sr", false, shell.HostPath, nil)
			if err != nil {
				log.Errorf("Failed to get host OS version: %v", err)
				return hostOsInfo, fmt.Errorf("failed to get host OS version: %w", err)
			} else {
				if output != "" {
					hostOsInfo["version"] = strings.TrimSpace(output)
					log.Infof("Detected OS info: " + hostOsInfo["name"] + " " +
						hostOsInfo["version"] + " " + hostOsInfo["arch"])
					return hostOsInfo, nil
				}
			}
		}
	}

	log.Errorf("Failed to detect host OS info!")
	return hostOsInfo, fmt.Errorf("failed to detect host OS info")
}

func GetHostOsPkgManager() (string, error) {
	hostOsInfo, err := GetHostOsInfo()
	if err != nil {
		return "", err
	}

	switch hostOsInfo["name"] {
	case "Ubuntu", "Debian", "Debian GNU/Linux", "eLxr":
		return "apt", nil
	case "Fedora", "CentOS", "Red Hat Enterprise Linux":
		return "yum", nil
	case "Microsoft Azure Linux", "Edge Microvisor Toolkit":
		return "tdnf", nil
	default:
		log.Errorf("Unsupported host OS: %s", hostOsInfo["name"])
		return "", fmt.Errorf("unsupported host OS: %s", hostOsInfo["name"])
	}
}

func GetProviderId(os, dist, arch string) string {
	return os + "-" + dist + "-" + arch
}

// IsLiveInstallerExecution returns true when the current process is live-installer.
// It checks the executable name and supports an env override for tests/tooling.
func IsLiveInstallerExecution() bool {
	mode := strings.TrimSpace(os.Getenv("ICT_EXECUTION_MODE"))
	if strings.EqualFold(mode, "live-installer") {
		return true
	}

	if len(os.Args) == 0 {
		return false
	}

	exe := strings.ToLower(filepath.Base(strings.TrimSpace(os.Args[0])))
	return strings.Contains(exe, "live-installer")
}

func StopGPGComponents(chrootPath string) error {
	log := logger.Logger()

	if !shell.IsBashAvailable(chrootPath) {
		log.Debugf("Bash not available in chroot environment, skipping GPG components stop")
		return nil
	}

	cmdExist, err := shell.IsCommandExist("gpgconf", chrootPath)
	if err != nil {
		return fmt.Errorf("failed to check if gpgconf command exists in chroot environment: %w", err)
	}
	if !cmdExist {
		log.Debugf("gpgconf command not found in chroot environment, skipping GPG components stop")
		return nil
	}
	output, err := shell.ExecCmd("gpgconf --list-components", false, chrootPath, nil)
	if err != nil {
		return fmt.Errorf("failed to list GPG components in chroot environment: %w", err)
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue // Skip empty lines or lines without a colon
		}
		component := strings.TrimSpace(strings.Split(line, ":")[0])
		log.Debugf("Stopping GPG component: %s", component)
		if _, err := shell.ExecCmd("gpgconf --kill "+component, true, chrootPath, nil); err != nil {
			return fmt.Errorf("failed to stop GPG component %s: %w", component, err)
		}
	}

	return nil
}

// QemuUserPackage returns the package that provides qemu user-mode emulation
// and binfmt registration on the host. Ubuntu 26.04 and later no longer ship
// qemu-user-static; it is only a virtual package there, provided by
// qemu-user-binfmt, so "apt install qemu-user-static" has no installation
// candidate. Older releases and Debian still use qemu-user-static.
//
// qemu-user-binfmt is chosen only on an apt host where apt itself reports that
// qemu-user-static cannot be installed; everywhere else the legacy name is kept.
func QemuUserPackage() string {
	const (
		legacyPkg = "qemu-user-static"
		binfmtPkg = "qemu-user-binfmt"
	)
	hasApt, err := shell.IsCommandExist("apt-get", shell.HostPath)
	if err != nil || !hasApt {
		return legacyPkg
	}
	// Ask apt to simulate the install. Its exit status applies the same rules as
	// the real install (virtual packages, pins, held candidates) and does not
	// depend on the host locale, unlike parsing "apt-cache" output. The
	// simulation changes nothing and needs no root.
	if _, err := shell.ExecCmdSilent("apt-get -s install "+legacyPkg, false, shell.HostPath, nil); err != nil {
		log.Debugf("%s cannot be installed on this host, using %s", legacyPkg, binfmtPkg)
		return binfmtPkg
	}
	return legacyPkg
}
