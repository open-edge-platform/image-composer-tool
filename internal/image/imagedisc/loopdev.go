package imagedisc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

type LoopDevInterface interface {
	LoopSetupDelete(loopDevPath string) error
	CreateRawImageLoopDev(filePath string, template *config.ImageTemplate) (string, map[string]string, error)
	AttachImageToLoopDev(imagePath string) (string, []string, error)
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

func loopSetupCreate(imagePath string) (string, error) {
	// losetup runs with sudo through a bash -c string. Single-quote the path so
	// bash performs no expansion on it: strconv.Quote uses double quotes, inside
	// which $(...), ${...} and backticks still expand, so a crafted work-dir or
	// image path could trigger command substitution. shell.QuoteArg neutralizes
	// that by wrapping the value in single quotes.
	cmd := fmt.Sprintf("losetup --direct-io=on --show -f -P %s", shell.QuoteArg(imagePath))
	loopDevPath, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Losetup failed for %s: %v", imagePath, err)
		return "", err
	}

	loopDevPath = strings.TrimSpace(loopDevPath)
	// losetup output is interpolated into later privileged bash -c commands
	// (partition scan, detach, lsblk, ...). Accept only a canonical loop device
	// path ("/dev/loopN") via an anchored regex, so a malformed or unexpected
	// output string can never be propagated into shell execution. A substring
	// match would let arbitrary surrounding text through.
	if canonicalLoopDevPath.MatchString(loopDevPath) {
		log.Infof("Losetup %s created loopback device at %s", imagePath, loopDevPath)
		return loopDevPath, nil
	} else {
		log.Errorf("Failed to create loopback device for %s", imagePath)
		return "", fmt.Errorf("failed to create loopback device for %s", imagePath)
	}
}

func loopSetupCreateEmptyRawDisk(filePath, fileSize string) (string, error) {
	// For the raw image file, create it without sudo as the folder is owned by user.
	if err := CreateRawFile(filePath, fileSize, false); err != nil {
		return "", err
	}

	if _, err := os.Stat(filePath); err == nil {
		return loopSetupCreate(filePath)
	}
	log.Errorf("Can't find %s after creating raw file", filePath)
	return "", fmt.Errorf("can't find %s", filePath)
}

// AttachImageToLoopDev attaches an already-existing disk image to a loop device
// with partition scanning enabled (losetup -fP) and returns the loop device path
// along with its enumerated partition nodes. Unlike CreateRawImageLoopDev it does
// not create or partition the backing file, so it is safe to use on a baseline
// image that must not be modified. On partition-enumeration failure the loop
// device is detached before returning so no loop device is leaked. If that
// detach itself fails, the device is genuinely leaked: the returned path is the
// leaked device (non-empty) and the error is annotated with it so the caller can
// retain the backing file and operators can reclaim the device.
func (loopDev *LoopDev) AttachImageToLoopDev(imagePath string) (string, []string, error) {
	if _, err := os.Stat(imagePath); err != nil {
		return "", nil, fmt.Errorf("cannot access baseline image at %s: %w", imagePath, err)
	}

	loopDevPath, err := loopSetupCreate(imagePath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to attach loop device for %s: %w", imagePath, err)
	}

	partitions, err := loopDevPartitions(loopDevPath)
	if err != nil {
		if detachErr := loopDev.LoopSetupDelete(loopDevPath); detachErr != nil {
			// Detach also failed: the loop device is leaked. Return the leaked
			// device path (not "") together with both the enumeration error and
			// the detach failure, wrapped with the path, so the caller can retain
			// the backing file and operators can identify and reclaim the device.
			log.Errorf("Failed to detach loop device %s after partition enumeration failure: %v", loopDevPath, detachErr)
			return loopDevPath, nil, fmt.Errorf("leaked loop device %s: %w", loopDevPath, errors.Join(err, detachErr))
		}
		return "", nil, err
	}

	return loopDevPath, partitions, nil
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

	// Unmount any partitions of this loop device
	if err := loopDev.unmountLoopDevicePartitions(loopDevPath); err != nil {
		log.Warnf("Warning while unmounting partitions on %s: %v", loopDevPath, err)
		// Don't return error, try to continue with detach
	}

	cmd := fmt.Sprintf("losetup -d %s", loopDevPath)
	if _, err := shell.ExecCmd(cmd, true, shell.HostPath, nil); err != nil {
		errMsg := err.Error()
		// Check if device was already detached (no such device - this is success)
		if strings.Contains(errMsg, "No such device") || strings.Contains(errMsg, "cannot find") {
			log.Infof("Loop device %s already detached or doesn't exist (likely cleaned by system)", loopDevPath)
			return nil
		}

		log.Warnf("Standard detach failed for %s, attempting force detach: %v", loopDevPath, err)
		// Try detach again after a brief retry. NOTE: do NOT use `losetup -D` here
		// — that flag detaches EVERY loop device on the host and ignores the path
		// argument, which would tear down unrelated loop mounts. Stick with `-d`.
		forceCmd := fmt.Sprintf("losetup -d %s", loopDevPath)
		if _, forceErr := shell.ExecCmd(forceCmd, true, shell.HostPath, nil); forceErr != nil {
			forceErrMsg := forceErr.Error()
			// Check if already gone
			if strings.Contains(forceErrMsg, "No such device") || strings.Contains(forceErrMsg, "cannot find") {
				log.Infof("Loop device %s already cleaned by system", loopDevPath)
				return nil
			}
			log.Errorf("Failed to detach loop device %s after retry: %v, %v", loopDevPath, err, forceErr)
			return fmt.Errorf("failed to delete loop device %s: %w", loopDevPath, err)
		}
		log.Infof("Successfully force-detached loop device: %s", loopDevPath)
		return nil
	}
	return nil
}

// unmountLoopDevicePartitions unmounts all partitions of a loop device
func (loopDev *LoopDev) unmountLoopDevicePartitions(loopDevPath string) error {
	// Extract base loop device (e.g., /dev/loop0 from /dev/loop0p1)
	re := regexp.MustCompile(`^(/dev/loop\d+)(?:p\d+)?$`)
	match := re.FindStringSubmatch(loopDevPath)
	if len(match) < 2 {
		return nil
	}

	baseLoopDev := match[1]

	// Get all partitions using lsblk
	cmd := fmt.Sprintf("lsblk -o NAME,MOUNTPOINT %s -J", baseLoopDev)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Debugf("Could not list block devices for %s: %v", baseLoopDev, err)
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		log.Debugf("Failed to parse lsblk output: %v", err)
		return nil
	}

	// Find and unmount all mounted partitions
	return loopDev.findAndUnmount(result)
}

// findAndUnmount recursively finds mounted partitions and unmounts them
func (loopDev *LoopDev) findAndUnmount(data interface{}) error {
	switch v := data.(type) {
	case map[string]interface{}:
		// Check if this entry has a mountpoint
		if mountPoint, ok := v["mountpoint"].(string); ok && mountPoint != "" {
			if name, ok := v["name"].(string); ok {
				mountDev := fmt.Sprintf("/dev/%s", name)
				log.Infof("Unmounting partition: %s from %s", mountDev, mountPoint)
				// Shell-escape the mount point to safely handle spaces and special characters
				escapedMountPoint := strconv.Quote(mountPoint)
				// Already a lazy unmount (-l). If this fails, log and continue —
				// the loop device detach below will still proceed.
				if _, err := shell.ExecCmd(fmt.Sprintf("umount -l %s", escapedMountPoint), true, shell.HostPath, nil); err != nil {
					log.Warnf("Failed to lazy-unmount %s: %v", mountPoint, err)
				} else {
					log.Infof("Successfully unmounted: %s", mountPoint)
				}
			}
		}

		// Recurse into nested structures
		for _, val := range v {
			if err := loopDev.findAndUnmount(val); err != nil {
				return err
			}
		}

	case []interface{}:
		for _, item := range v {
			if err := loopDev.findAndUnmount(item); err != nil {
				return err
			}
		}
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

func (loopDev *LoopDev) CreateRawImageLoopDev(filePath string, template *config.ImageTemplate) (string, map[string]string, error) {
	var diskPathIdMap map[string]string
	var loopDevPath string

	diskInfo := template.GetDiskConfig()
	loopDevPath, err := loopSetupCreateEmptyRawDisk(filePath, diskInfo.Size)
	if err != nil {
		return loopDevPath, diskPathIdMap, fmt.Errorf("failed to create loop device: %w", err)
	}
	diskPathIdMap, err = DiskPartitionsCreate(loopDevPath, diskInfo.Partitions, diskInfo.PartitionTableType)
	if err != nil {
		return loopDevPath, diskPathIdMap, fmt.Errorf("failed to create partitions on loop device %s: %w", loopDevPath, err)
	}
	return loopDevPath, diskPathIdMap, nil
}
