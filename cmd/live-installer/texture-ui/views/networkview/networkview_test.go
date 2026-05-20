// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package networkview

import (
	"testing"

	"github.com/gdamore/tcell"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/rivo/tview"
)

func TestNew(t *testing.T) {
	nv := New()

	if nv == nil {
		t.Fatal("New() returned nil")
	}

	// Check initial state
	if nv.form != nil {
		t.Error("expected form to be nil before initialization")
	}

	if nv.interfaceField != nil {
		t.Error("expected interfaceField to be nil before initialization")
	}

	if nv.dhcpDropDown != nil {
		t.Error("expected dhcpDropDown to be nil before initialization")
	}

	if nv.ipField != nil {
		t.Error("expected ipField to be nil before initialization")
	}

	if nv.gatewayField != nil {
		t.Error("expected gatewayField to be nil before initialization")
	}

	if nv.dnsField != nil {
		t.Error("expected dnsField to be nil before initialization")
	}

	if nv.navBar != nil {
		t.Error("expected navBar to be nil before initialization")
	}

	if nv.flex != nil {
		t.Error("expected flex to be nil before initialization")
	}

	if nv.centeredFlex != nil {
		t.Error("expected centeredFlex to be nil before initialization")
	}
}

func TestNetworkView_Name(t *testing.T) {
	nv := New()
	name := nv.Name()
	expectedName := "NETWORK"

	if name != expectedName {
		t.Errorf("expected name to be %q, got %q", expectedName, name)
	}
}

func TestNetworkView_Title(t *testing.T) {
	nv := New()
	title := nv.Title()

	if title == "" {
		t.Error("expected Title() to return non-empty string")
	}
}

func TestNetworkView_Primitive_BeforeInitialization(t *testing.T) {
	nv := New()
	primitive := nv.Primitive()

	if primitive != nil {
		t.Logf("Primitive() returned non-nil before initialization: %T", primitive)
	}
}

func TestNetworkView_OnShow(t *testing.T) {
	nv := New()

	// OnShow should not panic even if not initialized
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("OnShow() panicked: %v", r)
		}
	}()

	nv.OnShow()
}

func TestNetworkView_Initialize(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	nv := New()
	err := nv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)

	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Check that UI elements are initialized
	if nv.form == nil {
		t.Error("expected form to be initialized")
	}

	if nv.interfaceField == nil {
		t.Error("expected interfaceField to be initialized")
	}

	if nv.dhcpDropDown == nil {
		t.Error("expected dhcpDropDown to be initialized")
	}

	if nv.ipField == nil {
		t.Error("expected ipField to be initialized")
	}

	if nv.gatewayField == nil {
		t.Error("expected gatewayField to be initialized")
	}

	if nv.dnsField == nil {
		t.Error("expected dnsField to be initialized")
	}

	if nv.navBar == nil {
		t.Error("expected navBar to be initialized")
	}

	if nv.flex == nil {
		t.Error("expected flex to be initialized")
	}

	if nv.centeredFlex == nil {
		t.Error("expected centeredFlex to be initialized")
	}
}

func TestNetworkView_Primitive_AfterInitialization(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	nv := New()
	err := nv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	primitive := nv.Primitive()

	if primitive == nil {
		t.Error("expected Primitive() to return non-nil after initialization")
	}

	if primitive != nv.centeredFlex {
		t.Error("expected Primitive() to return centeredFlex")
	}
}

func TestNetworkView_Reset(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	nv := New()
	err := nv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Set some values
	nv.interfaceField.SetText("eth0")
	nv.ipField.SetText("192.168.1.10/24")
	nv.gatewayField.SetText("192.168.1.1")
	nv.dnsField.SetText("8.8.8.8")

	// Reset should clear all fields
	err = nv.Reset()
	if err != nil {
		t.Errorf("Reset() returned error: %v", err)
	}

	// Check fields are cleared
	if nv.interfaceField.GetText() != "" {
		t.Errorf("expected interfaceField to be empty after reset, got %q", nv.interfaceField.GetText())
	}

	if nv.ipField.GetText() != "" {
		t.Errorf("expected ipField to be empty after reset, got %q", nv.ipField.GetText())
	}

	if nv.gatewayField.GetText() != "" {
		t.Errorf("expected gatewayField to be empty after reset, got %q", nv.gatewayField.GetText())
	}

	if nv.dnsField.GetText() != "" {
		t.Errorf("expected dnsField to be empty after reset, got %q", nv.dnsField.GetText())
	}
}

func TestNetworkView_HandleInput(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	mockFunc := func() {}

	nv := New()
	err := nv.Initialize("Back", template, app, mockFunc, mockFunc, mockFunc, mockFunc)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Test up arrow converts to Backtab
	event := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	result := nv.HandleInput(event)
	if result == nil || result.Key() != tcell.KeyBacktab {
		t.Error("expected up arrow to convert to Backtab")
	}

	// Test down arrow converts to Tab
	event = tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	result = nv.HandleInput(event)
	if result == nil || result.Key() != tcell.KeyTab {
		t.Error("expected down arrow to convert to Tab")
	}

	// Test other keys pass through
	event = tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	result = nv.HandleInput(event)
	if result == nil || result.Key() != tcell.KeyEnter {
		t.Error("expected other keys to pass through")
	}
}

func TestNetworkView_EmptyInterfaceName(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Network: config.NetworkConfig{
				Backend: "netplan",
				Interfaces: []config.NetworkInterface{
					{Name: "eth0", DHCP4: boolPtr(true)},
				},
			},
		},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Leave interface name empty
	nv.interfaceField.SetText("")

	// Simulate clicking next button
	nv.onNextButton(nextPage)

	if !nextCalled {
		t.Error("expected next page to be called")
	}

	// Verify existing config is preserved
	if len(template.SystemConfig.Network.Interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(template.SystemConfig.Network.Interfaces))
	}
	if template.SystemConfig.Network.Interfaces[0].Name != "eth0" {
		t.Errorf("expected eth0 to be preserved")
	}
}

func TestNetworkView_DHCPMode(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	nv.interfaceField.SetText("eth0")
	// DHCP is selected by default (index 0)

	nv.onNextButton(nextPage)

	if !nextCalled {
		t.Error("expected next page to be called")
	}

	if len(template.SystemConfig.Network.Interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(template.SystemConfig.Network.Interfaces))
	}

	iface := template.SystemConfig.Network.Interfaces[0]
	if iface.Name != "eth0" {
		t.Errorf("expected interface name eth0, got %q", iface.Name)
	}

	if iface.DHCP4 == nil || !*iface.DHCP4 {
		t.Error("expected DHCP4 to be enabled")
	}
}

func TestNetworkView_StaticIPMode_Valid(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	nv.interfaceField.SetText("eth0")
	nv.dhcpDropDown.SetCurrentOption(1) // Static IP mode
	nv.ipField.SetText("192.168.1.10/24")
	nv.gatewayField.SetText("192.168.1.1")
	nv.dnsField.SetText("8.8.8.8,8.8.4.4")

	nv.onNextButton(nextPage)

	if !nextCalled {
		t.Error("expected next page to be called")
	}

	if len(template.SystemConfig.Network.Interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(template.SystemConfig.Network.Interfaces))
	}

	iface := template.SystemConfig.Network.Interfaces[0]
	if iface.Name != "eth0" {
		t.Errorf("expected interface name eth0, got %q", iface.Name)
	}

	if len(iface.Addresses) != 1 || iface.Addresses[0] != "192.168.1.10/24" {
		t.Errorf("expected address 192.168.1.10/24, got %v", iface.Addresses)
	}

	if len(iface.Routes) != 1 || iface.Routes[0].Via != "192.168.1.1" {
		t.Errorf("expected gateway 192.168.1.1, got %v", iface.Routes)
	}

	if len(iface.Nameservers) != 2 {
		t.Errorf("expected 2 DNS servers, got %d", len(iface.Nameservers))
	}
}

func TestNetworkView_StaticIPMode_InvalidIP(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	nv.interfaceField.SetText("eth0")
	nv.dhcpDropDown.SetCurrentOption(1) // Static IP mode
	nv.ipField.SetText("invalid-ip")    // Invalid

	nv.onNextButton(nextPage)

	if nextCalled {
		t.Error("expected next page to NOT be called with invalid IP")
	}

	// Network config should not be updated
	if len(template.SystemConfig.Network.Interfaces) > 0 {
		t.Error("expected network config to remain empty with invalid input")
	}
}

func TestNetworkView_StaticIPMode_MissingIP(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	nv.interfaceField.SetText("eth0")
	nv.dhcpDropDown.SetCurrentOption(1) // Static IP mode
	nv.ipField.SetText("")              // Missing

	nv.onNextButton(nextPage)

	if nextCalled {
		t.Error("expected next page to NOT be called with missing IP")
	}
}

func TestNetworkView_MergeInterfaces(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Network: config.NetworkConfig{
				Backend: "systemd-networkd",
				Interfaces: []config.NetworkInterface{
					{Name: "eth0", DHCP4: boolPtr(true)},
					{Name: "eth1", Addresses: []string{"10.0.0.100/24"}},
				},
			},
		},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Add a new interface
	nv.interfaceField.SetText("eth2")
	nv.dhcpDropDown.SetCurrentOption(0) // DHCP
	nv.onNextButton(nextPage)

	if !nextCalled {
		t.Error("expected next page to be called")
	}

	// Should have 3 interfaces now
	if len(template.SystemConfig.Network.Interfaces) != 3 {
		t.Errorf("expected 3 interfaces, got %d", len(template.SystemConfig.Network.Interfaces))
	}

	// Verify eth0 and eth1 are preserved
	names := make(map[string]bool)
	for _, iface := range template.SystemConfig.Network.Interfaces {
		names[iface.Name] = true
	}

	if !names["eth0"] || !names["eth1"] || !names["eth2"] {
		t.Error("expected eth0, eth1, and eth2 to all be present")
	}

	// Verify backend is preserved
	if template.SystemConfig.Network.Backend != "systemd-networkd" {
		t.Errorf("expected backend to remain systemd-networkd, got %q", template.SystemConfig.Network.Backend)
	}
}

func TestNetworkView_ReplaceExistingInterface(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{
			Network: config.NetworkConfig{
				Backend: "netplan",
				Interfaces: []config.NetworkInterface{
					{Name: "eth0", DHCP4: boolPtr(true)},
				},
			},
		},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	// Replace existing eth0 interface
	nv.interfaceField.SetText("eth0")
	nv.dhcpDropDown.SetCurrentOption(1) // Static IP mode
	nv.ipField.SetText("192.168.1.10/24")
	nv.onNextButton(nextPage)

	if !nextCalled {
		t.Error("expected next page to be called")
	}

	// Should still have only 1 interface
	if len(template.SystemConfig.Network.Interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(template.SystemConfig.Network.Interfaces))
	}

	// Verify eth0 is updated
	iface := template.SystemConfig.Network.Interfaces[0]
	if iface.Name != "eth0" {
		t.Errorf("expected eth0, got %q", iface.Name)
	}

	if iface.DHCP4 != nil {
		t.Error("expected DHCP4 to be nil after replacing with static IP")
	}

	if len(iface.Addresses) != 1 || iface.Addresses[0] != "192.168.1.10/24" {
		t.Errorf("expected static address, got %v", iface.Addresses)
	}
}

func TestNetworkView_InvalidGateway(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	nv.interfaceField.SetText("eth0")
	nv.dhcpDropDown.SetCurrentOption(1) // Static IP mode
	nv.ipField.SetText("192.168.1.10/24")
	nv.gatewayField.SetText("invalid-gateway")

	nv.onNextButton(nextPage)

	if nextCalled {
		t.Error("expected next page to NOT be called with invalid gateway")
	}
}

func TestNetworkView_InvalidDNS(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	nv.interfaceField.SetText("eth0")
	nv.dhcpDropDown.SetCurrentOption(1) // Static IP mode
	nv.ipField.SetText("192.168.1.10/24")
	nv.dnsField.SetText("8.8.8.8,invalid-dns")

	nv.onNextButton(nextPage)

	if nextCalled {
		t.Error("expected next page to NOT be called with invalid DNS")
	}
}

func TestNetworkView_OptionalGatewayAndDNS(t *testing.T) {
	template := &config.ImageTemplate{
		Target: config.TargetInfo{
			OS:   "ubuntu",
			Dist: "ubuntu24",
			Arch: "x86_64",
		},
		SystemConfig: config.SystemConfig{},
	}

	app := tview.NewApplication()
	nextCalled := false
	nextPage := func() {
		nextCalled = true
	}

	nv := New()
	err := nv.Initialize("Back", template, app, nextPage, func() {}, func() {}, func() {})
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	nv.interfaceField.SetText("eth0")
	nv.dhcpDropDown.SetCurrentOption(1) // Static IP mode
	nv.ipField.SetText("192.168.1.10/24")
	// Leave gateway and DNS empty (optional)

	nv.onNextButton(nextPage)

	if !nextCalled {
		t.Error("expected next page to be called with minimal static IP config")
	}

	iface := template.SystemConfig.Network.Interfaces[0]
	if len(iface.Routes) != 0 {
		t.Errorf("expected no routes when gateway not specified, got %d", len(iface.Routes))
	}

	if len(iface.Nameservers) != 0 {
		t.Errorf("expected no nameservers when DNS not specified, got %d", len(iface.Nameservers))
	}
}

// Helper function to create a bool pointer
func boolPtr(b bool) *bool {
	return &b
}
