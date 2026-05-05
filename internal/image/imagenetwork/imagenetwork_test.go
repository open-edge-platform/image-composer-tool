package imagenetwork

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

func boolPtr(v bool) *bool {
	return &v
}

func TestWriteNetworkConfig_EmptyConfig(t *testing.T) {
	err := WriteNetworkConfig("/tmp", nil)
	if err != nil {
		t.Errorf("expected no error for nil config, got %v", err)
	}

	emptyConfig := &config.NetworkConfig{}
	err = WriteNetworkConfig("/tmp", emptyConfig)
	if err != nil {
		t.Errorf("expected no error for empty config, got %v", err)
	}
}

func TestWriteNetworkConfig_UnsupportedBackend(t *testing.T) {
	tempDir := t.TempDir()
	netConfig := &config.NetworkConfig{
		Backend: "unsupported",
		Interfaces: []config.NetworkInterface{
			{Name: "eth0", DHCP4: boolPtr(true)},
		},
	}

	err := WriteNetworkConfig(tempDir, netConfig)
	if err == nil {
		t.Errorf("expected error for unsupported backend, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported network backend") {
		t.Errorf("expected 'unsupported backend' error, got %v", err)
	}
}

func TestWriteNetworkdConfig_SingleDHCPInterface(t *testing.T) {
	tempDir := t.TempDir()
	netConfig := &config.NetworkConfig{
		Backend: "systemd-networkd",
		Interfaces: []config.NetworkInterface{
			{Name: "eth0", DHCP4: boolPtr(true)},
		},
	}

	err := WriteNetworkConfig(tempDir, netConfig)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify file created
	filePath := filepath.Join(tempDir, "etc/systemd/network/10-eth0.network")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "[Match]") {
		t.Errorf("expected [Match] section in config")
	}
	if !strings.Contains(contentStr, "Name=eth0") {
		t.Errorf("expected Name=eth0 in config")
	}
	if !strings.Contains(contentStr, "[Network]") {
		t.Errorf("expected [Network] section in config")
	}
	if !strings.Contains(contentStr, "DHCP=yes") {
		t.Errorf("expected DHCP=yes in config")
	}
}

func TestWriteNetworkdConfig_StaticIP(t *testing.T) {
	tempDir := t.TempDir()
	netConfig := &config.NetworkConfig{
		Backend: "systemd-networkd",
		Interfaces: []config.NetworkInterface{
			{
				Name:        "eth1",
				Addresses:   []string{"10.0.0.5/24"},
				Gateway4:    "10.0.0.1",
				Nameservers: []string{"8.8.8.8", "8.8.4.4"},
			},
		},
	}

	err := WriteNetworkConfig(tempDir, netConfig)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	filePath := filepath.Join(tempDir, "etc/systemd/network/10-eth1.network")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "Name=eth1") {
		t.Errorf("expected Name=eth1 in config")
	}
	if !strings.Contains(contentStr, "Address=10.0.0.5/24") {
		t.Errorf("expected Address=10.0.0.5/24 in config")
	}
	if !strings.Contains(contentStr, "Gateway=10.0.0.1") {
		t.Errorf("expected Gateway=10.0.0.1 in config")
	}
	if !strings.Contains(contentStr, "DNS=8.8.8.8 8.8.4.4") {
		t.Errorf("expected DNS servers in config")
	}
}

func TestWriteNetworkdConfig_MultipleInterfaces(t *testing.T) {
	tempDir := t.TempDir()
	netConfig := &config.NetworkConfig{
		Backend: "systemd-networkd",
		Interfaces: []config.NetworkInterface{
			{Name: "eth0", DHCP4: boolPtr(true)},
			{Name: "eth1", Addresses: []string{"192.168.1.10/24"}, Gateway4: "192.168.1.1"},
		},
	}

	err := WriteNetworkConfig(tempDir, netConfig)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify both files created
	eth0Path := filepath.Join(tempDir, "etc/systemd/network/10-eth0.network")
	eth1Path := filepath.Join(tempDir, "etc/systemd/network/10-eth1.network")

	if _, err := os.ReadFile(eth0Path); err != nil {
		t.Errorf("eth0.network not created: %v", err)
	}
	if _, err := os.ReadFile(eth1Path); err != nil {
		t.Errorf("eth1.network not created: %v", err)
	}
}

func TestWriteNetplanConfig_SingleInterface(t *testing.T) {
	tempDir := t.TempDir()
	netConfig := &config.NetworkConfig{
		Backend: "netplan",
		Interfaces: []config.NetworkInterface{
			{Name: "eth0", DHCP4: boolPtr(true)},
		},
	}

	err := WriteNetworkConfig(tempDir, netConfig)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	filePath := filepath.Join(tempDir, "etc/netplan/01-installer.yaml")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "network:") {
		t.Errorf("expected 'network:' in netplan config")
	}
	if !strings.Contains(contentStr, "version: 2") {
		t.Errorf("expected 'version: 2' in netplan config")
	}
	if !strings.Contains(contentStr, "ethernets:") {
		t.Errorf("expected 'ethernets:' in netplan config")
	}
	if !strings.Contains(contentStr, "eth0:") {
		t.Errorf("expected 'eth0:' interface in netplan config")
	}
	if !strings.Contains(contentStr, "dhcp4: true") {
		t.Errorf("expected 'dhcp4: true' in netplan config")
	}
}

func TestWriteNetplanConfig_MultipleInterfaces(t *testing.T) {
	tempDir := t.TempDir()
	netConfig := &config.NetworkConfig{
		Backend: "netplan",
		Interfaces: []config.NetworkInterface{
			{Name: "eth0", DHCP4: boolPtr(true)},
			{
				Name:        "eth1",
				Addresses:   []string{"10.0.0.5/24"},
				Gateway4:    "10.0.0.1",
				Nameservers: []string{"8.8.8.8"},
			},
		},
	}

	err := WriteNetworkConfig(tempDir, netConfig)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	filePath := filepath.Join(tempDir, "etc/netplan/01-installer.yaml")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "eth0:") {
		t.Errorf("expected 'eth0:' in netplan config")
	}
	if !strings.Contains(contentStr, "eth1:") {
		t.Errorf("expected 'eth1:' in netplan config")
	}
	if !strings.Contains(contentStr, "addresses:") {
		t.Errorf("expected 'addresses:' for static config")
	}
}

func TestGenerateNetworkdFile_DHCPv4Only(t *testing.T) {
	iface := config.NetworkInterface{
		Name:  "eth0",
		DHCP4: boolPtr(true),
	}

	content := generateNetworkdFile(iface)

	if !strings.Contains(content, "[Match]") {
		t.Errorf("expected [Match] section")
	}
	if !strings.Contains(content, "Name=eth0") {
		t.Errorf("expected Name=eth0")
	}
	if !strings.Contains(content, "[Network]") {
		t.Errorf("expected [Network] section")
	}
	if !strings.Contains(content, "DHCP=yes") {
		t.Errorf("expected DHCP=yes")
	}
}

func TestGenerateNetworkdFile_StaticIPWithDNS(t *testing.T) {
	iface := config.NetworkInterface{
		Name:        "eth0",
		Addresses:   []string{"192.168.1.10/24", "192.168.1.11/24"},
		Gateway4:    "192.168.1.1",
		Nameservers: []string{"8.8.8.8", "1.1.1.1"},
	}

	content := generateNetworkdFile(iface)

	if !strings.Contains(content, "Address=192.168.1.10/24") {
		t.Errorf("expected first address in config")
	}
	if !strings.Contains(content, "Address=192.168.1.11/24") {
		t.Errorf("expected second address in config")
	}
	if !strings.Contains(content, "Gateway=192.168.1.1") {
		t.Errorf("expected gateway in config")
	}
	if !strings.Contains(content, "DNS=8.8.8.8 1.1.1.1") {
		t.Errorf("expected DNS servers in config")
	}
}

func TestGenerateNetworkdFile_DualStack(t *testing.T) {
	iface := config.NetworkInterface{
		Name:     "eth0",
		DHCP4:    boolPtr(true),
		DHCP6:    boolPtr(true),
		Gateway6: "fe80::1",
	}

	content := generateNetworkdFile(iface)

	if !strings.Contains(content, "DHCP=yes") {
		t.Errorf("expected DHCP=yes for dual stack")
	}
	if !strings.Contains(content, "Gateway=fe80::1") {
		t.Errorf("expected IPv6 gateway")
	}
}

func TestGenerateNetplanFile_DHCPInterface(t *testing.T) {
	ifaces := []config.NetworkInterface{
		{Name: "eth0", DHCP4: boolPtr(true)},
	}

	content := generateNetplanFile(ifaces)

	if !strings.Contains(content, "network:") {
		t.Errorf("expected 'network:' section")
	}
	if !strings.Contains(content, "version: 2") {
		t.Errorf("expected 'version: 2'")
	}
	if !strings.Contains(content, "ethernets:") {
		t.Errorf("expected 'ethernets:' section")
	}
	if !strings.Contains(content, "eth0:") {
		t.Errorf("expected 'eth0:' interface")
	}
	if !strings.Contains(content, "dhcp4: true") {
		t.Errorf("expected 'dhcp4: true'")
	}
}

func TestGenerateNetplanFile_StaticIP(t *testing.T) {
	ifaces := []config.NetworkInterface{
		{
			Name:        "eth0",
			Addresses:   []string{"10.0.0.5/24"},
			Gateway4:    "10.0.0.1",
			Nameservers: []string{"8.8.8.8"},
		},
	}

	content := generateNetplanFile(ifaces)

	if !strings.Contains(content, "addresses:") {
		t.Errorf("expected 'addresses:' section")
	}
	if !strings.Contains(content, "- 10.0.0.5/24") {
		t.Errorf("expected address in list format")
	}
	if !strings.Contains(content, "gateway4: 10.0.0.1") {
		t.Errorf("expected gateway4")
	}
	if !strings.Contains(content, "nameservers:") {
		t.Errorf("expected 'nameservers:' section")
	}
}

func TestGenerateNetplanFile_MultipleInterfaces(t *testing.T) {
	ifaces := []config.NetworkInterface{
		{Name: "eth0", DHCP4: boolPtr(true)},
		{Name: "eth1", Addresses: []string{"192.168.1.100/24"}},
	}

	content := generateNetplanFile(ifaces)

	if strings.Count(content, "ethernets:") != 1 {
		t.Errorf("expected single 'ethernets:' section")
	}
	if !strings.Contains(content, "eth0:") {
		t.Errorf("expected eth0 interface")
	}
	if !strings.Contains(content, "eth1:") {
		t.Errorf("expected eth1 interface")
	}
}

func TestWriteNetworkdConfig_MissingInterfaceName(t *testing.T) {
	tempDir := t.TempDir()
	netConfig := &config.NetworkConfig{
		Backend: "systemd-networkd",
		Interfaces: []config.NetworkInterface{
			{DHCP4: boolPtr(true)}, // Missing name
		},
	}

	err := WriteNetworkConfig(tempDir, netConfig)
	if err == nil {
		t.Errorf("expected error for missing interface name, got nil")
	}
}
