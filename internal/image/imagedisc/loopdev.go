package imagedisc

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

type LoopDevInterface interface {
	LoopSetupDelete(loopDevPath string) error
	CreateRawImageLoopDev(filePath string, template *config.ImageTemplate) (string, map[string]string, error)
}

type LoopDev struct{}

func NewLoopDev() *LoopDev {
	return &LoopDev{}
}

func loopSetupCreate(imagePath string) (string, error) {
	cmd := fmt.Sprintf("losetup --direct-io=on --show -f -P %s", imagePath)
	loopDevPath, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Losetup failed for %s: %v", imagePath, err)
		return "", err
	}

	loopDevPath = strings.TrimSpace(loopDevPath)
	if strings.Contains(loopDevPath, "/dev/loop") {
		log.Infof(fmt.Sprintf("Losetup %s created loopback device at %s\n", imagePath, loopDevPath))
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

func (loopDev *LoopDev) LoopSetupDelete(loopDevPath string) error {
	cmd := fmt.Sprintf("losetup -d %s", loopDevPath)
	if _, err := shell.ExecCmd(cmd, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to delete loop device %s: %v", loopDevPath, err)
		return fmt.Errorf("failed to delete loop device %s: %w", loopDevPath, err)
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
