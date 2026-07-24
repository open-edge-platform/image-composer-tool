package imagedisc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/runctx"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

type LoopDevInterface interface {
	LoopSetupDelete(loopDevPath string) error
	CreateRawImageLoopDev(filePath string, template *config.ImageTemplate) (string, map[string]string, func(), error)
	AttachImageToLoopDev(imagePath string) (string, []string, func(), error)
}

type LoopDev struct{}

// canonicalLoopDevPath matches a bare loop device path, e.g. "/dev/loop0" or
// "/dev/loop12", with no trailing partition suffix or surrounding text. It is
// used to validate losetup output before that value is interpolated into
// privileged shell commands.
var canonicalLoopDevPath = regexp.MustCompile(`^/dev/loop\d+$`)

func NewLoopDev() *LoopDev {
	return &LoopDev{}
}

// loopSetupCreate attaches imagePath as a loop device with partition scanning
// (losetup -fP) and returns the canonical device path plus an unregister
// closure. When a runctx.Coordinator is bound, the returned closure removes
// the auto-registered detach callback.
//
// Ordering contract for callers: invoke LoopSetupDelete FIRST, then call
// unregister() only on successful detach. On the happy path this prevents a
// double-detach warning when the coordinator backstop runs; on a cancelled
// build the caller's LoopSetupDelete may fail-fast because its internal
// shell.ExecCmd sees the still-cancelled ambient ctx (build.go's PostProcess
// context re-binding hasn't happened yet when caller-side loop defers run),
// and in that case leaving the coord entry registered lets the deferred
// coord.Run in build.go retry the detach under its own fresh per-entry ctx
// that binds shell/runctx to a detached timeout budget.
//
// When no coordinator is bound the closure is a no-op.
func loopSetupCreate(imagePath string) (string, func(), error) {
	// losetup runs with sudo through a bash -c string. Single-quote the path so
	// bash performs no expansion on it: strconv.Quote uses double quotes, inside
	// which $(...), ${...} and backticks still expand, so a crafted work-dir or
	// image path could trigger command substitution. shell.QuoteArg neutralizes
	// that by wrapping the value in single quotes.
	cmd := fmt.Sprintf("losetup --direct-io=on --show -f -P %s", shell.QuoteArg(imagePath))
	loopDevPath, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Losetup failed for %s: %v", imagePath, err)
		return "", func() {}, err
	}

	loopDevPath = strings.TrimSpace(loopDevPath)
	// losetup output is interpolated into later privileged bash -c commands
	// (partition scan, detach, lsblk, ...). Accept only a canonical loop device
	// path ("/dev/loopN") via an anchored regex, so a malformed or unexpected
	// output string can never be propagated into shell execution. A substring
	// match would let arbitrary surrounding text through.
	if !canonicalLoopDevPath.MatchString(loopDevPath) {
		log.Errorf("Failed to create loopback device for %s", imagePath)
		return "", func() {}, fmt.Errorf("failed to create loopback device for %s", imagePath)
	}
	log.Infof("Losetup %s created loopback device at %s", imagePath, loopDevPath)

	// Register a detach with the build-scoped cleanup coordinator so a
	// SIGINT/SIGTERM mid-BuildImage releases this device even if the
	// happy-path defer in the caller is bypassed. LoopSetupDelete is
	// best-effort under a torn-down chroot: a still-mounted partition
	// prevents detach and the coordinator surfaces the error in its
	// residual list, but it won't wedge the coordinator.
	//
	// The callback receives the coordinator's per-entry timeout ctx
	// (see runctx.Coordinator.Run). Bind that ctx to the shell and
	// runctx layers for the callback's duration so LoopSetupDelete's
	// shell.ExecCmd calls run under the cleanup budget instead of the
	// already-cancelled parent ctx that fired the coordinator.
	unregister := func() {}
	if c := runctx.Get(); c != nil {
		devPath := loopDevPath
		loopDev := &LoopDev{}
		unregister = c.Register(
			"loop:"+devPath,
			func(ctx context.Context) error {
				restoreShell := shell.SetContext(ctx)
				defer restoreShell()
				restoreRun := runctx.SetContext(ctx)
				defer restoreRun()
				return loopDev.LoopSetupDelete(devPath)
			},
		)
	}
	return loopDevPath, unregister, nil
}

func loopSetupCreateEmptyRawDisk(filePath, fileSize string) (string, func(), error) {
	// For the raw image file, create it without sudo as the folder is owned by user.
	if err := CreateRawFile(filePath, fileSize, false); err != nil {
		return "", func() {}, err
	}

	if _, err := os.Stat(filePath); err == nil {
		return loopSetupCreate(filePath)
	}
	log.Errorf("Can't find %s after creating raw file", filePath)
	return "", func() {}, fmt.Errorf("can't find %s", filePath)
}

// AttachImageToLoopDev attaches an already-existing disk image to a loop device
// with partition scanning enabled (losetup -fP) and returns the loop device path
// along with its enumerated partition nodes and an unregister closure. Callers
// should invoke LoopSetupDelete first, then unregister() only on successful
// detach (see loopSetupCreate's docstring for the reasoning: on cancel the
// caller-side detach may fail-fast under the still-cancelled ambient shell
// ctx, and keeping the coord entry registered lets the build.go backstop
// retry). Unlike CreateRawImageLoopDev it does not create or partition the
// backing file, so it is safe to use on a baseline image that must not be
// modified. On partition-enumeration failure the loop device is detached
// before returning so no loop device is leaked. If that detach itself fails,
// the device is genuinely leaked: the returned path is the leaked device
// (non-empty) and the error is annotated with it so the caller can retain
// the backing file and operators can reclaim the device.
func (loopDev *LoopDev) AttachImageToLoopDev(imagePath string) (string, []string, func(), error) {
	if _, err := os.Stat(imagePath); err != nil {
		return "", nil, func() {}, fmt.Errorf("cannot access baseline image at %s: %w", imagePath, err)
	}

	loopDevPath, unregister, err := loopSetupCreate(imagePath)
	if err != nil {
		return "", nil, func() {}, fmt.Errorf("failed to attach loop device for %s: %w", imagePath, err)
	}

	partitions, err := loopDevPartitions(loopDevPath)
	if err != nil {
		// Enumeration failed; detach before returning. Detach first, then
		// unregister only on success. If detach also fails (leaked device
		// case), leave the coord entry registered so a subsequent
		// coordinator run can retry — the returned loopDevPath signals
		// leaked state to the caller and the entry gives the backstop
		// something to retry.
		if detachErr := loopDev.LoopSetupDelete(loopDevPath); detachErr != nil {
			// Detach also failed: the loop device is leaked. Return the leaked
			// device path (not "") together with both the enumeration error and
			// the detach failure, wrapped with the path, so the caller can retain
			// the backing file and operators can identify and reclaim the device.
			// Also return a no-op unregister — the caller intentionally cannot
			// remove the coord entry so the backstop retries.
			log.Errorf("Failed to detach loop device %s after partition enumeration failure: %v", loopDevPath, detachErr)
			return loopDevPath, nil, func() {}, fmt.Errorf("leaked loop device %s: %w", loopDevPath, errors.Join(err, detachErr))
		}
		unregister()
		return "", nil, func() {}, err
	}

	return loopDevPath, partitions, unregister, nil
}

// loopDevPartitions returns the partition device nodes of a loop device, e.g.
// ["/dev/loop0p1", "/dev/loop0p2"]. The base loop device itself is excluded.
func loopDevPartitions(loopDevPath string) ([]string, error) {
	// loopDevPath originates from losetup output; single-quote it defensively so
	// a malformed value is never reinterpreted by the bash -c shell. Single
	// quotes (via shell.QuoteArg) suppress the $(...)/backtick expansion that
	// double-quoting (strconv.Quote) would still allow.
	cmd := fmt.Sprintf("lsblk -prno NAME %s", shell.QuoteArg(loopDevPath))
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to list partitions for loop device %s: %v", loopDevPath, err)
		return nil, fmt.Errorf("failed to list partitions for loop device %s: %w", loopDevPath, err)
	}

	var partitions []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || name == loopDevPath {
			continue
		}
		partitions = append(partitions, name)
	}
	return partitions, nil
}

func (loopDev *LoopDev) LoopSetupDelete(loopDevPath string) error {
	// Handle SWAP partitions before detaching
	if err := loopDev.disableSwapPartitions(loopDevPath); err != nil {
		log.Warnf("Warning while disabling SWAP partitions on %s: %v", loopDevPath, err)
		// Don't return error, try to continue with detach
	}

	cmd := fmt.Sprintf("losetup -d %s", loopDevPath)
	if _, err := shell.ExecCmd(cmd, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to delete loop device %s: %v", loopDevPath, err)
		return fmt.Errorf("failed to delete loop device %s: %w", loopDevPath, err)
	}
	return nil
}

// disableSwapPartitions finds and disables SWAP partitions on a loop device
func (loopDev *LoopDev) disableSwapPartitions(loopDevPath string) error {
	// List all partitions to find SWAP ones
	// Get base loop device number from path (e.g., /dev/loop0 from /dev/loop0p1)
	re := regexp.MustCompile(`^(/dev/loop\d+)(?:p\d+)?$`)
	match := re.FindStringSubmatch(loopDevPath)
	if len(match) < 2 {
		// If not a loop device, no SWAP to disable
		return nil
	}

	// Get all partitions of this loop device
	cmd := fmt.Sprintf("lsblk -o NAME,FSTYPE %s -J", match[1])
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Debugf("Could not list block devices for %s: %v", match[1], err)
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		log.Debugf("Failed to parse lsblk output: %v", err)
		return nil
	}

	// Recursively find and disable SWAP partitions
	if err := loopDev.findAndDisableSwap(result); err != nil {
		return err
	}

	return nil
}

// findAndDisableSwap recursively searches for SWAP filesystems and disables them
func (loopDev *LoopDev) findAndDisableSwap(data interface{}) error {
	switch v := data.(type) {
	case map[string]interface{}:
		// Check if this entry is a SWAP partition
		if fsType, ok := v["fstype"].(string); ok && fsType == "swap" {
			if name, ok := v["name"].(string); ok {
				swapDev := fmt.Sprintf("/dev/%s", name)
				log.Infof("Found SWAP partition: %s, disabling it", swapDev)
				if _, err := shell.ExecCmd(fmt.Sprintf("swapoff %s", swapDev), true, shell.HostPath, nil); err != nil {
					log.Warnf("Failed to disable SWAP on %s: %v", swapDev, err)
					// Continue processing other partitions
				} else {
					log.Infof("Successfully disabled SWAP on %s", swapDev)
				}
			}
		}

		// Recurse into nested structures
		for _, val := range v {
			if err := loopDev.findAndDisableSwap(val); err != nil {
				return err
			}
		}

	case []interface{}:
		for _, item := range v {
			if err := loopDev.findAndDisableSwap(item); err != nil {
				return err
			}
		}
	}

	return nil
}

func LoopDevGetInfo(loopDevPath string) (map[string]interface{}, error) {
	cmd := fmt.Sprintf("losetup -l %s --json", loopDevPath)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get info for loop device %s: %v", loopDevPath, err)
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		log.Errorf("Failed to parse JSON output for loop device %s: %v", loopDevPath, err)
		return nil, err
	}

	if devices, ok := result["loopdevices"].([]interface{}); ok && len(devices) > 0 {
		if info, ok := devices[0].(map[string]interface{}); ok {
			return info, nil
		}
	}
	log.Errorf("No loop device info found for %s", loopDevPath)
	return nil, fmt.Errorf("no loop device info found")
}

func LoopDevGetBackFile(loopDevPath string) (string, error) {
	info, err := LoopDevGetInfo(loopDevPath)
	if err != nil {
		return "", err
	}

	if backFile, ok := info["back-file"].(string); ok {
		return backFile, nil
	}
	log.Errorf("Back-file not found for loop device %s", loopDevPath)
	return "", fmt.Errorf("back-file not found")
}

func LoopDevGetInfoAll() ([]map[string]interface{}, error) {
	cmd := "losetup -l --json"
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get info for all loop devices: %v", err)
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		log.Errorf("Failed to parse JSON output for all loop devices: %v", err)
		return nil, err
	}

	var list []map[string]interface{}
	if devices, ok := result["loopdevices"].([]interface{}); ok {
		for _, dev := range devices {
			if m, ok := dev.(map[string]interface{}); ok {
				list = append(list, m)
			}
		}
	}
	return list, nil
}

func GetLoopDevPathFromLoopDevPart(loopDevPart string) (string, error) {
	re := regexp.MustCompile(`^(/dev/loop\d+)p(\d+)`)
	match := re.FindStringSubmatch(loopDevPart)
	if len(match) > 1 {
		return match[1], nil
	} else {
		log.Errorf("Invalid loop device partition format: %s", loopDevPart)
		return "", fmt.Errorf("invalid loop device partition format: %s", loopDevPart)
	}
}

// CreateRawImageLoopDev fallocates and losetup-attaches a fresh raw image
// file, then creates the partition table on it. Returns the loop device path,
// the partition-path-to-partition-id map, and an unregister closure that
// callers must invoke from their happy-path defer right before their own
// explicit LoopSetupDelete so the build-scoped cleanup coordinator does not
// try to detach a device that has already been released.
func (loopDev *LoopDev) CreateRawImageLoopDev(filePath string, template *config.ImageTemplate) (string, map[string]string, func(), error) {
	var diskPathIdMap map[string]string
	var loopDevPath string

	diskInfo := template.GetDiskConfig()
	loopDevPath, unregister, err := loopSetupCreateEmptyRawDisk(filePath, diskInfo.Size)
	if err != nil {
		return loopDevPath, diskPathIdMap, func() {}, fmt.Errorf("failed to create loop device: %w", err)
	}
	diskPathIdMap, err = DiskPartitionsCreate(loopDevPath, diskInfo.Partitions, diskInfo.PartitionTableType)
	if err != nil {
		return loopDevPath, diskPathIdMap, unregister, fmt.Errorf("failed to create partitions on loop device %s: %w", loopDevPath, err)
	}
	return loopDevPath, diskPathIdMap, unregister, nil
}
