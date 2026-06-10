// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package autopartitionwidget

import (
	"fmt"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"

	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/primitives/customshortcutlist"
	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/primitives/navigationbar"
	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/uitext"
	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/uiutils"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
)

// UI constants.
const (
	nextButtonIndex = 2
	defaultPadding  = 1

	textProportion = 0
	listProportion = 0

	navBarHeight     = 0
	navBarProportion = 1
)

// AutoPartitionWidget contains the disk selection UI
type AutoPartitionWidget struct {
	navBar       *navigationbar.NavigationBar
	flex         *tview.Flex
	centeredFlex *tview.Flex
	deviceList   *customshortcutlist.List
	helpText     *tview.TextView

	systemDevices []imagedisc.SystemBlockDevice
	bootType      string
}

// New creates and returns a new AutoPartitionWidget.
func New(systemDevices []imagedisc.SystemBlockDevice, bootType string) *AutoPartitionWidget {
	return &AutoPartitionWidget{
		systemDevices: systemDevices,
		bootType:      bootType,
	}
}

// Initialize initializes the view.
func (ap *AutoPartitionWidget) Initialize(backButtonText string, template *config.ImageTemplate, app *tview.Application, switchMode, nextPage, previousPage, quit, refreshTitle func()) (err error) {
	ap.navBar = navigationbar.NewNavigationBar().
		AddButton(backButtonText, previousPage).
		AddButton(uitext.DiskButtonCustom, switchMode).
		AddButton(uitext.ButtonNext, func() {
			ap.mustUpdateConfiguration(template)
			nextPage()
		}).
		SetAlign(tview.AlignCenter)

	ap.deviceList = customshortcutlist.NewList().
		ShowSecondaryText(false)
	ap.populateBlockDeviceOptions()

	ap.helpText = tview.NewTextView().
		SetText(uitext.DiskHelp)

	textWidth, textHeight := uiutils.MinTextViewWithNoWrapSize(ap.helpText)
	centeredText := uiutils.Center(textWidth, textHeight, ap.helpText)

	listWidth, listHeight := uiutils.MinListSize(ap.deviceList)
	centeredList := uiutils.Center(listWidth, listHeight, ap.deviceList)

	ap.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(centeredText, textHeight, textProportion, false).
		AddItem(centeredList, listHeight, listProportion, true).
		AddItem(ap.navBar, navBarHeight, navBarProportion, false)

	ap.centeredFlex = uiutils.CenterVerticallyDynamically(ap.flex)

	// Box styling
	ap.helpText.SetBorderPadding(defaultPadding, defaultPadding, defaultPadding, defaultPadding)
	ap.centeredFlex.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	return
}

// HandleInput handles custom input.
func (ap *AutoPartitionWidget) HandleInput(event *tcell.EventKey) *tcell.EventKey {
	if ap.navBar.UnfocusedInputHandler(event) {
		return nil
	}

	return event
}

// Reset resets the page, undoing any user input.
func (ap *AutoPartitionWidget) Reset() (err error) {
	ap.deviceList.SetCurrentItem(0)
	ap.navBar.ClearUserFeedback()
	ap.navBar.SetSelectedButton(nextButtonIndex)

	return
}

// Name returns the friendly name of the view.
func (ap *AutoPartitionWidget) Name() string {
	return "AUTOPARTITIONWIDGET"
}

// Title returns the title of the view.
func (ap *AutoPartitionWidget) Title() string {
	return uitext.DiskTitle
}

// Primitive returns the primary primitive to be rendered for the view.
func (ap *AutoPartitionWidget) Primitive() tview.Primitive {
	return ap.centeredFlex
}

// SelectedSystemDevice returns the index of the currently selected system device.
func (ap *AutoPartitionWidget) SelectedSystemDevice() int {
	return ap.deviceList.GetCurrentItem()
}

func (ap *AutoPartitionWidget) mustUpdateConfiguration(template *config.ImageTemplate) {
	template.Disk.Path = ap.systemDevices[ap.deviceList.GetCurrentItem()].DevicePath

	// If template already has partitions defined, use them
	if len(template.Disk.Partitions) > 0 {
		// Ensure partition table type is set (default to GPT if not already set)
		if template.Disk.PartitionTableType == "" {
			template.Disk.PartitionTableType = "gpt"
		}
	} else {
		// No partitions in template, use hardcoded defaults
		template.Disk.PartitionTableType = "gpt"
		template.Disk.Partitions = ap.createDefaultPartitions(template.Target.Arch)
	}
}

// createDefaultPartitions creates default boot and root partitions for auto mode when template has no partitions
func (ap *AutoPartitionWidget) createDefaultPartitions(arch string) []config.PartitionInfo {
	bootMountPoint, bootMountOptions, bootFlags, err := imagedisc.BootPartitionConfig(ap.bootType, imagedisc.PartitionTableTypeGpt)
	if err != nil {
		bootMountPoint = "/boot/efi"
		bootMountOptions = "umask=0077"
		bootFlags = []string{imagedisc.PartitionFlagESP, imagedisc.PartitionFlagBoot}
	}

	var partitions []config.PartitionInfo

	// Create boot partition
	bootPartition := config.PartitionInfo{
		ID:           "boot",
		Type:         "esp",
		Start:        "1MiB",
		End:          "513MiB",
		FsType:       "fat32",
		MountPoint:   bootMountPoint,
		MountOptions: bootMountOptions,
		Flags:        bootFlags,
	}
	partitions = append(partitions, bootPartition)

	// Create root partition - use full remaining space
	// Determine root partition type based on architecture
	rootType := "linux-root-amd64"
	if arch == "aarch64" {
		rootType = "linux-root-aarch64"
	}

	rootPartition := config.PartitionInfo{
		ID:           "rootfs",
		Type:         rootType,
		Start:        "513MiB",
		End:          "0", // 0 means use all remaining space
		FsType:       "ext4",
		MountPoint:   "/",
		MountOptions: "defaults",
		Flags:        []string{},
	}
	partitions = append(partitions, rootPartition)

	return partitions
}

func (ap *AutoPartitionWidget) populateBlockDeviceOptions() {
	for _, disk := range ap.systemDevices {
		formattedSize := imagedisc.TranslateBytesToSizeStr(disk.RawDiskSize)
		diskRepresentation := fmt.Sprintf("%s - %s @ %s", disk.Model, formattedSize, disk.DevicePath)
		ap.deviceList.AddItem(diskRepresentation, "", 0, nil)
	}
}
