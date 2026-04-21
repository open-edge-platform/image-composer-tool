package system

import (
	"bufio"
	"fmt"
	"os"
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
