// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package networkview

import (
	"fmt"
	"net"
	"strings"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"

	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/primitives/navigationbar"
	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/uitext"
	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/uiutils"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// UI constants.
const (
	defaultNavButton = 1
	noSelection      = -1

	formProportion = 0

	interfaceFieldWidth = 32
	ipFieldWidth        = 40
	dnsFieldWidth       = 60

	dhcpOptionYes = "Yes (DHCP)"
	dhcpOptionNo  = "No (Static IP)"
)

// NetworkView contains the network configuration UI.
type NetworkView struct {
	form           *tview.Form
	interfaceField *tview.InputField
	dhcpDropDown   *tview.DropDown
	ipField        *tview.InputField
	gatewayField   *tview.InputField
	dnsField       *tview.InputField
	navBar         *navigationbar.NavigationBar
	flex           *tview.Flex
	centeredFlex   *tview.Flex

	template *config.ImageTemplate
}

// New creates and returns a new NetworkView.
func New() *NetworkView {
	return &NetworkView{}
}

// Initialize initializes the view.
func (nv *NetworkView) Initialize(backButtonText string, template *config.ImageTemplate, app *tview.Application, nextPage, previousPage, quit, refreshTitle func()) (err error) {
	nv.template = template

	nv.interfaceField = tview.NewInputField().
		SetLabel(uitext.NetworkInterfaceLabel).
		SetFieldWidth(interfaceFieldWidth)

	nv.ipField = tview.NewInputField().
		SetLabel(uitext.NetworkIPLabel).
		SetFieldWidth(ipFieldWidth).
		SetPlaceholder(uitext.NetworkIPPlaceholder)

	nv.gatewayField = tview.NewInputField().
		SetLabel(uitext.NetworkGatewayLabel).
		SetFieldWidth(ipFieldWidth).
		SetPlaceholder(uitext.NetworkGatewayPlaceholder)

	nv.dnsField = tview.NewInputField().
		SetLabel(uitext.NetworkDNSLabel).
		SetFieldWidth(dnsFieldWidth).
		SetPlaceholder(uitext.NetworkDNSPlaceholder)

	nv.dhcpDropDown = tview.NewDropDown().
		SetLabel(uitext.NetworkDHCPLabel).
		SetOptions([]string{dhcpOptionYes, dhcpOptionNo}, func(text string, index int) {
			nv.updateStaticFieldVisibility(text == dhcpOptionNo, app)
		}).
		SetCurrentOption(0)

	nv.navBar = navigationbar.NewNavigationBar().
		AddButton(backButtonText, previousPage).
		AddButton(uitext.ButtonNext, func() {
			nv.onNextButton(nextPage)
		}).
		SetAlign(tview.AlignCenter).
		SetOnFocusFunc(func() {
			nv.navBar.SetSelectedButton(defaultNavButton)
		}).
		SetOnBlurFunc(func() {
			nv.navBar.SetSelectedButton(noSelection)
		})

	nv.form = tview.NewForm().
		SetButtonsAlign(tview.AlignCenter).
		AddFormItem(nv.interfaceField).
		AddFormItem(nv.dhcpDropDown).
		AddFormItem(nv.ipField).
		AddFormItem(nv.gatewayField).
		AddFormItem(nv.dnsField).
		AddFormItem(nv.navBar)

	nv.flex = tview.NewFlex().
		SetDirection(tview.FlexRow)

	formWidth, formHeight := uiutils.MinFormSize(nv.form)
	centeredForm := uiutils.CenterHorizontally(formWidth, nv.form)

	nv.flex.AddItem(centeredForm, formHeight+nv.navBar.GetHeight(), formProportion, true)
	nv.centeredFlex = uiutils.CenterVerticallyDynamically(nv.flex)
	nv.centeredFlex.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	return
}

// HandleInput handles custom input.
func (nv *NetworkView) HandleInput(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyUp:
		return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
	case tcell.KeyDown:
		return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	}
	return event
}

// Reset resets the page, undoing any user input.
func (nv *NetworkView) Reset() (err error) {
	nv.navBar.ClearUserFeedback()
	nv.navBar.SetSelectedButton(noSelection)
	nv.form.SetFocus(0)
	nv.interfaceField.SetText("")
	nv.dhcpDropDown.SetCurrentOption(0)
	nv.ipField.SetText("")
	nv.gatewayField.SetText("")
	nv.dnsField.SetText("")
	return
}

// Name returns the friendly name of the view.
func (nv *NetworkView) Name() string {
	return "NETWORK"
}

// Title returns the title of the view.
func (nv *NetworkView) Title() string {
	return uitext.NetworkTitle
}

// Primitive returns the primary primitive to be rendered for the view.
func (nv *NetworkView) Primitive() tview.Primitive {
	return nv.centeredFlex
}

// OnShow gets called when the view is shown to the user.
func (nv *NetworkView) OnShow() {
}

func (nv *NetworkView) updateStaticFieldVisibility(showStatic bool, app *tview.Application) {
	// Static IP fields are always present in the form; we just clear them
	// when the user switches back to DHCP so they don't get applied.
	if nv.ipField == nil || nv.gatewayField == nil || nv.dnsField == nil {
		return
	}

	if !showStatic {
		nv.ipField.SetText("")
		nv.gatewayField.SetText("")
		nv.dnsField.SetText("")
	}
}

func (nv *NetworkView) onNextButton(nextPage func()) {
	nv.navBar.ClearUserFeedback()

	ifaceName := strings.TrimSpace(nv.interfaceField.GetText())

	// Interface name is optional — if empty, preserve existing network config and continue.
	if ifaceName == "" {
		nextPage()
		return
	}

	_, selectedDHCP := nv.dhcpDropDown.GetCurrentOption()
	useDHCP := selectedDHCP == dhcpOptionYes

	iface := config.NetworkInterface{
		Name: ifaceName,
	}

	if useDHCP {
		dhcp4 := true
		iface.DHCP4 = &dhcp4
	} else {
		// Validate static IP (CIDR notation, e.g. 192.168.1.10/24)
		ipText := strings.TrimSpace(nv.ipField.GetText())
		if ipText == "" {
			nv.navBar.SetUserFeedback(uitext.NetworkIPRequiredError, tview.Styles.TertiaryTextColor)
			return
		}
		if _, _, err := net.ParseCIDR(ipText); err != nil {
			nv.navBar.SetUserFeedback(fmt.Sprintf(uitext.NetworkIPInvalidErrorFmt, ipText), tview.Styles.TertiaryTextColor)
			return
		}
		iface.Addresses = []string{ipText}

		// Validate gateway (optional but must be valid if provided)
		gatewayText := strings.TrimSpace(nv.gatewayField.GetText())
		if gatewayText != "" {
			if net.ParseIP(gatewayText) == nil {
				nv.navBar.SetUserFeedback(fmt.Sprintf(uitext.NetworkGatewayInvalidErrorFmt, gatewayText), tview.Styles.TertiaryTextColor)
				return
			}
			iface.Routes = []config.NetworkRoute{
				{To: "default", Via: gatewayText},
			}
		}

		// Parse DNS servers (comma-separated, optional)
		dnsText := strings.TrimSpace(nv.dnsField.GetText())
		if dnsText != "" {
			dnsServers := strings.Split(dnsText, ",")
			for _, dns := range dnsServers {
				dns = strings.TrimSpace(dns)
				if net.ParseIP(dns) == nil {
					nv.navBar.SetUserFeedback(fmt.Sprintf(uitext.NetworkDNSInvalidErrorFmt, dns), tview.Styles.TertiaryTextColor)
					return
				}
			}
			iface.Nameservers = dnsServers
		}
	}

	// Preserve existing network configuration and merge in the new interface.
	// If no existing config, create a new one.
	backend := nv.template.SystemConfig.Network.Backend
	if backend == "" {
		backend = "netplan"
	}

	interfaces := nv.template.SystemConfig.Network.Interfaces
	if interfaces == nil {
		interfaces = []config.NetworkInterface{}
	}

	// Replace or add the interface by name
	found := false
	for i, existingIface := range interfaces {
		if existingIface.Name == ifaceName {
			interfaces[i] = iface
			found = true
			break
		}
	}
	if !found {
		interfaces = append(interfaces, iface)
	}

	nv.template.SystemConfig.Network = config.NetworkConfig{
		Backend:    backend,
		Interfaces: interfaces,
	}

	nextPage()
}
