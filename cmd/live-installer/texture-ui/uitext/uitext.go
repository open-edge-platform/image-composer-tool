// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package uitext

// Useful prefix strings
const (
	RequiredInputMark = "* "
	BoldPrefix        = "[::b]"
	WhiteBoldPrefix   = "[#ffffff::b]"
)

// "]" is a special character for the TUI text, escape it with "[]"

// Navigation text.
const (
	ButtonAccept          = "[Accept[]"
	ButtonCancel          = "[Cancel[]"
	ButtonCancelWhiteBold = WhiteBoldPrefix + ButtonCancel
	ButtonConfirm         = "[Confirm[]"
	ButtonGoBack          = "[Go Back[]"
	ButtonNext            = "[Next[]"
	ButtonYes             = "[Yes[]"
	ButtonQuit            = "[Quit[]"
	ButtonQuitWhiteBold   = WhiteBoldPrefix + ButtonQuit
	ButtonRestart         = "[Restart[]"
)

// AttendedInstaller wrapper text.
const (
	NavigationHelp = "Arrow keys make selections. Enter activates."
	ExitModalTitle = BoldPrefix + "Do you want to quit setup?"
)

// ConfirmView text.
const (
	ConfirmTitle  = "Confirm"
	ConfirmPrompt = `Start installation?
All data on the selected disk will be lost.`
)

// DiskView text.
const (
	// Buttons
	DiskButtonAddPartition    = "[Add Partition[]"
	DiskButtonAuto            = "[Auto Partition[]"
	DiskButtonCustom          = "[Custom Partition[]"
	DiskButtonRemovePartition = "[Remove Partition[]"

	// Auto partition
	DiskHelp  = "Please select a disk to install OS on."
	DiskTitle = "Select a Disk"

	// Custom Partition
	DiskAdvanceTitleFmt      = "Partitions for: %v"
	DiskAddPartitionTitle    = "Add Partition"
	DiskFormatLabel          = "Format"
	DiskLabelLabel           = "Label"
	DiskMountPointLabel      = "Mount Point"
	DiskNameLabel            = "Name"
	DiskSizeLabel            = "Size"
	DiskSpaceLeftFmt         = "Remaining space: %v"
	FormDiskSizeLabelMaxHelp = "(* for max)"
	FormDiskSizeUnitLabel    = "* Unit size"
	FormDiskFormatLabel      = RequiredInputMark + DiskFormatLabel
	FormDiskLabelLabel       = DiskLabelLabel
	FormDiskMountPointLabel  = RequiredInputMark + DiskMountPointLabel
	FormDiskNameLabel        = RequiredInputMark + DiskNameLabel
	FormDiskSizeLabel        = RequiredInputMark + DiskSizeLabel

	// Errors
	InvalidBootPartitionErrorFmt       = "invalid boot partition: first partition must be of type '%s'"
	InvalidRootPartitionErrorFmt       = "must specify a partition to have the mount point '%s'"
	InvalidRootPartitionErrorFormatFmt = "root partition cannot be %s"
	MountPointAlreadyInUseError        = "mount point is already in use"
	MountPointStartError               = "mount point must start with `/`"
	MountPointInvalidCharacterError    = "mount point only supports alphanumeric characters and `/`"
	LabelInvalidCharacterError         = "label only supports alphanumeric characters, '-' and '_'"
	NameInvalidCharacterError          = "name only supports alphanumeric characters"
	NoPartitionsError                  = "must specify at least one boot and one root partition"
	NoPartitionSelectedError           = "no partition selected"
	NoSizeSpecifiedError               = "size must be specified"
	NotEnoughDiskSpaceError            = "not enough space left on disk"
	PartitionExceedsDiskErrorFmt       = "device space exceeded by partition (%d)"
	SizeStartError                     = "size can not start with `0`"
	SizeInvalidCharacterError          = "size must be a number"
	UnexpectedPartitionErrorFmt        = "unexpected partition size '%s'"
)

// EncryptView text.
const (
	EncryptTitle                = "Enter Disk Encryption Password"
	SkipEncryption              = "[Skip Disk Encryption[]"
	EncryptPasswordLabel        = "* Disk Encryption Password"
	ConfirmEncryptPasswordLabel = "* Confirm Disk Encryption Password"
)

// InstallerView text.
const (
	InstallerExperienceTitle = "Select Installation Experience"
	InstallerTerminalOption  = "Terminal Installer"
	InstallerGraphicalOption = "Graphical Installer"
	InstallerMemTestOption   = "Memory Test"
)

// EulaView text.
const (
	EulaTitle = "Welcome to the OS Installer"
)

// HostNameView text.
const (
	HostNameTitle      = "Choose the Host Name for Your System"
	HostNameInputLabel = "* Host Name:"

	HostNameSegment   = "host name"
	DomainNameSegment = "domain name"

	FQDNEmptyErrorFmt         = "empty %s is not allowed"
	FQDNEndsInDashErrorFmt    = "%s should not end with '-'"
	FQDNInvalidRuneErrorFmt   = "%s should only contain alpha-numeric, '.' and '-' characters"
	FQDNInvalidStartErrorFmt  = "%s should start with an alpha character"
	FQDNInvalidLengthErrorFmt = "host name must be <= %d characters"
)

// InstallationView text.
const (
	InstallationTitle = "Select Installation Type"
)

// UserView text.
const (
	SetupUserTitle = "Set Up User Account"

	PasswordInputLabel        = "* Password"
	ConfirmPasswordInputLabel = "* Confirm Password"
	UserNameInputLabel        = "* User Name"

	PasswordMismatchFeedback = "Passwords do not match"
	EnumNavigationFeedback   = "Use left or right arrow keys to change the selection"

	UserNameEmptyError            = "user name cannot be empty"
	UserNameInvalidRuneError      = "user name should only contain alpha-numeric and '-', '.' or '_' characters"
	UserNameInvalidStartError     = "user name should start with an alpha-numeric character"
	UserNameInvalidLengthErrorFmt = "user name must be <= %d characters"
)

// ProgressView text.
const (
	ProgressTitle      = "Installing OS"
	ProgressSpinnerFmt = "Installing OS, please wait %v"
)

// FinishView text.
const (
	FinishTitle   = "OS Installation Complete"
	FinishTextFmt = "Total installation time: %v. Press Enter to restart."
)
