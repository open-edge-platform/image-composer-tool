package imagenetwork

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

var log = logger.Logger()

// WriteNetworkConfig orchestrates network configuration generation.
func WriteNetworkConfig(installRoot string, netConfig *config.NetworkConfig) error {
	if netConfig == nil || netConfig.Backend == "" {
		log.Debugf("No network configuration specified")
		return nil
	}

	if netConfig.Backend != "systemd-networkd" && netConfig.Backend != "netplan" {
		return fmt.Errorf("unsupported network backend: %s (must be 'systemd-networkd' or 'netplan')", netConfig.Backend)
	}

	if len(netConfig.Interfaces) == 0 {
		log.Warnf("Network backend specified but no interfaces configured")
		return nil
	}

	log.Infof("Configuring network with backend: %s", netConfig.Backend)

	switch netConfig.Backend {
	case "systemd-networkd":
		return writeNetworkdConfig(installRoot, netConfig.Interfaces)
	case "netplan":
		return writeNetplanConfig(installRoot, netConfig.Interfaces)
	default:
		return fmt.Errorf("unsupported network backend: %s", netConfig.Backend)
	}
}

func writeNetworkdConfig(installRoot string, interfaces []config.NetworkInterface) error {
	networkDir := filepath.Join(installRoot, "etc/systemd/network")
	if err := os.MkdirAll(networkDir, 0755); err != nil {
		return fmt.Errorf("failed to create network directory: %w", err)
	}

	for i, iface := range interfaces {
		if iface.Name == "" {
			return fmt.Errorf("interface %d: name is required", i)
		}

		content := generateNetworkdFile(iface)
		filePath := filepath.Join(networkDir, fmt.Sprintf("10-%s.network", iface.Name))
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write networkd config for %s: %w", iface.Name, err)
		}
	}

	return nil
}

func writeNetplanConfig(installRoot string, interfaces []config.NetworkInterface) error {
	networkDir := filepath.Join(installRoot, "etc/netplan")
	if err := os.MkdirAll(networkDir, 0755); err != nil {
		return fmt.Errorf("failed to create netplan directory: %w", err)
	}

	for i, iface := range interfaces {
		if iface.Name == "" {
			return fmt.Errorf("interface %d: name is required", i)
		}
	}

	content := generateNetplanFile(interfaces)
	filePath := filepath.Join(networkDir, "01-installer.yaml")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write netplan config: %w", err)
	}

	return nil
}

func generateNetworkdFile(iface config.NetworkInterface) string {
	var sb strings.Builder

	sb.WriteString("[Match]\n")
	sb.WriteString(fmt.Sprintf("Name=%s\n", iface.Name))
	sb.WriteString("\n[Network]\n")

	dhcp4 := iface.DHCP4 != nil && *iface.DHCP4
	dhcp6 := iface.DHCP6 != nil && *iface.DHCP6
	switch {
	case dhcp4 && dhcp6:
		sb.WriteString("DHCP=yes\n")
	case dhcp4:
		sb.WriteString("DHCP=yes\n")
	case dhcp6:
		sb.WriteString("DHCP=ipv6\n")
	}

	for _, addr := range iface.Addresses {
		sb.WriteString(fmt.Sprintf("Address=%s\n", addr))
	}

	if iface.Gateway4 != "" {
		sb.WriteString(fmt.Sprintf("Gateway=%s\n", iface.Gateway4))
	}
	if iface.Gateway6 != "" {
		sb.WriteString(fmt.Sprintf("Gateway=%s\n", iface.Gateway6))
	}
	if len(iface.Nameservers) > 0 {
		sb.WriteString(fmt.Sprintf("DNS=%s\n", strings.Join(iface.Nameservers, " ")))
	}

	return sb.String()
}

func generateNetplanFile(interfaces []config.NetworkInterface) string {
	var sb strings.Builder

	sb.WriteString("network:\n")
	sb.WriteString("  version: 2\n")
	sb.WriteString("  ethernets:\n")

	for _, iface := range interfaces {
		sb.WriteString(fmt.Sprintf("    %s:\n", iface.Name))

		if iface.DHCP4 != nil && *iface.DHCP4 {
			sb.WriteString("      dhcp4: true\n")
		}
		if iface.DHCP6 != nil && *iface.DHCP6 {
			sb.WriteString("      dhcp6: true\n")
		}

		if len(iface.Addresses) > 0 {
			sb.WriteString("      addresses:\n")
			for _, addr := range iface.Addresses {
				sb.WriteString(fmt.Sprintf("        - %s\n", addr))
			}
		}
		if iface.Gateway4 != "" {
			sb.WriteString(fmt.Sprintf("      gateway4: %s\n", iface.Gateway4))
		}
		if iface.Gateway6 != "" {
			sb.WriteString(fmt.Sprintf("      gateway6: %s\n", iface.Gateway6))
		}

		if len(iface.Nameservers) > 0 {
			sb.WriteString("      nameservers:\n")
			sb.WriteString("        addresses:\n")
			for _, ns := range iface.Nameservers {
				sb.WriteString(fmt.Sprintf("          - %s\n", ns))
			}
		}
	}

	return sb.String()
}
