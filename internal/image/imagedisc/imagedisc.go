package imagedisc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
	"github.com/open-edge-platform/os-image-composer/internal/utils/slice"
)

type blockDevicesOutput struct {
	Devices []blockDeviceInfo `json:"blockdevices"`
}

type blockDeviceInfo struct {
	Name   string      `json:"name"`    // Example: sda
	MajMin string      `json:"maj:min"` // Example: 1:2
	Size   json.Number `json:"size"`    // Number of bytes. Can be a quoted string or a JSON number, depending on the util-linux version
	Model  string      `json:"model"`   // Example: 'Virtual Disk'
}

type SystemBlockDevice struct {
	DevicePath  string // Example: /dev/sda
	RawDiskSize uint64 // Size in bytes
	Model       string // Example: Virtual Disk
}

const (
	EFIPartitionType    = "efi"
	LegacyPartitionType = "legacy"

	// PartitionTableTypeGpt selects gpt
	PartitionTableTypeGpt string = "gpt"
	// PartitionTableTypeMbr selects mbr
	PartitionTableTypeMbr string = "mbr"
	// PartitionTableTypeNone selects no partition type
	PartitionTableTypeNone string = ""

	// PartitionFlagESP indicates this is the UEFI esp partition
	PartitionFlagESP string = "esp"
	// PartitionFlagGrub indicates this is a grub boot partition
	PartitionFlagGrub string = "grub"
	// PartitionFlagBiosGrub indicates this is a bios grub boot partition
	PartitionFlagBiosGrub string = "bios_grub"
	// PartitionFlagBiosGrubLegacy indicates this is a bios grub boot partition. Needed to preserve legacy config behavior.
	PartitionFlagBiosGrubLegacy string = "bios-grub"
	// PartitionFlagBoot indicates this is a boot partition
	PartitionFlagBoot string = "boot"
	// PartitionFlagDeviceMapperRoot indicates this partition will be used for a device mapper root device
	PartitionFlagDeviceMapperRoot string = "dmroot"
)

var log = logger.Logger()
var sizeSuffixesList = []string{"KiB", "MiB", "GiB", "K", "M", "G", "KB", "MB", "GB"}
var sizeBytesMap = []int{1024, 1048576, 1073741824, 1024, 1048576, 1073741824, 1000, 1000000, 1000000000}
var partitionTypeNameToGUID = map[string]string{
	"linux":            "0fc63daf-8483-4772-8e79-3d69d8477de4",
	"bios":             "21686148-6449-6e6f-744e-656564454649",
	"esp":              "c12a7328-f81f-11d2-ba4b-00a0c93ec93b",
	"xbootldr":         "bc13c2ff-59e6-4262-a352-b275fd6f7172",
	"linux-root-amd64": "4f68bce3-e8cd-4db1-96e7-fbcaf984b709",
	"linux-root-arm64": "b921b045-1df0-41c3-af44-4c6f280d3fae",
	"linux-swap":       "0657fd6d-a4ab-43c4-84e5-0933c84b4f4f",
	"linux-home":       "933ac7e1-2eb4-4f13-b844-0e14e2aef915",
	"linux-srv":        "3b8f8425-20e0-4f3b-907f-1a25a76f98e8",
	"linux-var":        "4d21b016-b534-45c2-a9fb-5c16e091fd2d",
	"linux-tmp":        "7ec6f557-3bc5-4aca-b293-16ef5df639d1",
	"linux-lvm":        "e6d6d379-f507-44c2-a23c-238f2a3df928",
	"linux-raid":       "a19d880f-05fc-4d3b-a006-743f0f84911e",
	"linux-luks":       "ca7d7ccb-63ed-4c53-861c-1742536059cc",
	"linux-dm-crypt":   "7ffec5c9-2d00-49b7-8941-3ea10a5586b7",
}

// IsDigit checks if a string contains only digits
func IsDigit(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// VerifyFileSize checks and formats the file size string.
func VerifyFileSize(fileSize interface{}) (string, error) {
	switch v := fileSize.(type) {
	case int:
		return fmt.Sprintf("%dMiB", v), nil
	case string:
		if fileSize == "0" {
			return fileSize.(string), nil
		}
		pattern := regexp.MustCompile(`^(\d+)(.*)$`)
		match := pattern.FindStringSubmatch(v)
		if len(match) == 3 {
			num := match[1]
			if !IsDigit(num) {
				return "", fmt.Errorf("file size number incorrect: %s", num)
			}
			sizeSuffix := match[2]
			if !slice.Contains(sizeSuffixesList, sizeSuffix) {
				return "", fmt.Errorf("file size suffix incorrect: %s", sizeSuffix)
			} else {
				return v, nil
			}
		}
		return "", fmt.Errorf("file size format incorrect: %s", v)
	default:
		return "", fmt.Errorf("unsupported fileSize type")
	}
}

// TranslateSizeStrToBytes converts a size string to bytes.
func TranslateSizeStrToBytes(sizeStr string) (uint64, error) {
	pattern := regexp.MustCompile(`^(\d+)(.*)$`)
	match := pattern.FindStringSubmatch(sizeStr)
	if len(match) == 3 {
		numStr := match[1]
		sizeSuffix := match[2]
		for i, s := range sizeSuffixesList {
			if sizeSuffix == s {
				num, err := strconv.Atoi(numStr)
				if err != nil {
					return 0, err
				}
				return uint64(sizeBytesMap[i] * num), nil
			}
		}
		return 0, fmt.Errorf("file size suffix incorrect: %s", sizeSuffix)
	}
	return 0, fmt.Errorf("size format incorrect: %s", sizeStr)
}

func TranslateBytesToSizeStr(byteSize uint64) string {
	if byteSize == 0 {
		return "0B"
	}
	for i := len(sizeBytesMap) - 1; i >= 0; i-- {
		unit := uint64(sizeBytesMap[i])
		if byteSize >= unit {
			v := float64(byteSize) / float64(unit)
			// trim trailing zeros as needed, here show up to 2 decimals
			if v == float64(int64(v)) {
				return fmt.Sprintf("%d%s", int64(v), sizeSuffixesList[i])
			}
			return fmt.Sprintf("%.2f%s", v, sizeSuffixesList[i])
		}
	}
	return fmt.Sprintf("%dB", byteSize)
}

func CreateRawFile(filePath string, fileSize string, sudo bool) error {
	fileSizeStr, err := VerifyFileSize(fileSize)
	if err != nil {
		log.Errorf("Invalid file size %s: %v", fileSize, err)
		return err
	}
	fileDir := filepath.Dir(filePath)
	if _, err := os.Stat(fileDir); os.IsNotExist(err) {
		if err := os.MkdirAll(fileDir, 0700); err != nil {
			log.Errorf("Failed to create directory %s: %v", fileDir, err)
			return fmt.Errorf("failed to create directory %s: %w", fileDir, err)
		}
	}
	cmd := fmt.Sprintf("fallocate -l %s %s", fileSizeStr, filePath)
	if _, err = shell.ExecCmd(cmd, sudo, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to create raw file %s: %v", filePath, err)
		return fmt.Errorf("failed to create raw file %s: %w", filePath, err)
	}
	return nil
}

func GetDiskNameFromDiskPath(diskPath string) (string, error) {
	re := regexp.MustCompile(`^/dev/(.*)`)
	match := re.FindStringSubmatch(diskPath)
	if len(match) > 1 {
		return match[1], nil
	} else {
		return "", fmt.Errorf("failed to extract disk name from path: %s", diskPath)
	}
}

func DiskGetHwSectorSize(diskName string) (int, error) {
	cmd := fmt.Sprintf("cat /sys/block/%s/queue/hw_sector_size", diskName)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get hw sector size for disk %s: %v", diskName, err)
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(output))
}

func DiskGetPhysicalBlockSize(diskName string) (int, error) {
	cmd := fmt.Sprintf("cat /sys/block/%s/queue/physical_block_size", diskName)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get physical block size for disk %s: %v", diskName, err)
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(output))
}

func DiskGetDevInfo(diskPath string) (map[string]interface{}, error) {
	cmd := fmt.Sprintf("lsblk %s --json --list --output NAME,PATH,PARTTYPE,FSTYPE,UUID,MOUNTPOINT,PARTUUID,PARTLABEL,TYPE", diskPath)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get device info for disk %s: %v", diskPath, err)
		return nil, err
	}
	var partitionsInfo map[string]interface{}
	if err := json.Unmarshal([]byte(output), &partitionsInfo); err != nil {
		log.Errorf("Failed to parse device info for disk %s: %v", diskPath, err)
		return nil, err
	}
	if blockDevices, ok := partitionsInfo["blockdevices"].([]interface{}); ok {
		for _, device := range blockDevices {
			dev := device.(map[string]interface{})
			if dev["path"] == diskPath {
				return dev, nil
			}
		}
	}
	log.Errorf("Device info not found for disk %s", diskPath)
	return nil, errors.New("device not found")
}

func DiskGetPartitionsInfo(diskPath string) ([]map[string]interface{}, error) {
	cmd := fmt.Sprintf("lsblk %s --json --list --output NAME,PATH,PARTTYPE,FSTYPE,UUID,MOUNTPOINT,PARTUUID,PARTLABEL,TYPE", diskPath)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get partitions info for disk %s: %v", diskPath, err)
		return nil, err
	}
	var partitionsInfo map[string]interface{}
	if err := json.Unmarshal([]byte(output), &partitionsInfo); err != nil {
		log.Errorf("Failed to parse partitions info for disk %s: %v", diskPath, err)
		return nil, err
	}
	var partitions []map[string]interface{}
	if blockDevices, ok := partitionsInfo["blockdevices"].([]interface{}); ok {
		for _, device := range blockDevices {
			dev := device.(map[string]interface{})
			if dev["type"] == "part" {
				partitions = append(partitions, dev)
			}
		}
	}
	return partitions, nil
}

func DiskGetInfo(diskPath string) (map[string]interface{}, error) {
	cmd := fmt.Sprintf("fdisk -l %s", diskPath)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get disk info for disk %s: %v", diskPath, err)
		return nil, err
	}
	diskInfo := make(map[string]interface{})
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Disk "+diskPath) {
			diskInfo["device"] = diskPath
			sizeInfo := strings.Split(line, ":")
			if len(sizeInfo) > 1 {
				sizeInfoList := strings.Split(sizeInfo[1], ",")
				if len(sizeInfoList) > 2 {
					bytes, _ := strconv.Atoi(strings.Fields(sizeInfoList[1])[0])
					sectors, _ := strconv.Atoi(strings.Fields(sizeInfoList[2])[0])
					diskInfo["bytes"] = bytes
					diskInfo["sectors"] = sectors
					diskInfo["part_num"] = 0
					diskInfo["part_info"] = []map[string]interface{}{}
				}
			}
		} else if strings.Contains(line, "Sector size") {
			sizes := strings.Split(line, ":")
			if len(sizes) > 1 {
				logicalPhysical := strings.Split(sizes[1], "/")
				if len(logicalPhysical) == 2 {
					diskInfo["logical_size"] = strings.TrimSpace(logicalPhysical[0])
					diskInfo["physical_size"] = strings.TrimSpace(logicalPhysical[1])
				}
			}
		} else if strings.Contains(line, "Disklabel type") {
			diskInfo["part_table_type"] = strings.TrimSpace(strings.Split(line, ":")[1])
		} else if strings.Contains(line, "Disk identifier") {
			diskInfo["disk_id"] = strings.TrimSpace(strings.Split(line, ":")[1])
		} else if strings.Contains(line, diskPath) {
			partInfoList := strings.Fields(line)
			if len(partInfoList) >= 5 {
				partInfo := map[string]interface{}{
					"device":    partInfoList[0],
					"start_sec": partInfoList[1],
					"end_sec":   partInfoList[2],
					"sectors":   partInfoList[3],
					"size":      partInfoList[4],
					"type":      strings.Join(partInfoList[5:], " "),
				}
				diskInfo["part_info"] = append(diskInfo["part_info"].([]map[string]interface{}), partInfo)
				diskInfo["part_num"] = diskInfo["part_num"].(int) + 1
			}
		}
	}
	return diskInfo, nil
}

func IsDiskPartitionExist(diskPath string) (bool, error) {
	diskInfo, err := DiskGetInfo(diskPath)
	if err != nil {
		return false, err
	}
	if partInfo, ok := diskInfo["part_info"].([]map[string]interface{}); ok && len(partInfo) >= 1 {
		return true, nil
	}
	return false, nil
}

func CheckDiskIOStats(diskPath string) (bool, error) {
	ioIsBusy := false

	diskName, err := GetDiskNameFromDiskPath(diskPath)
	if err != nil {
		return false, err
	}
	cmd := fmt.Sprintf("cat /proc/diskstats | grep %s*", diskName)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get io stats for disk %s: %v", diskPath, err)
		return false, err
	}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		ioStats := strings.Fields(line)
		if len(ioStats) < 14 {
			continue
		}
		ioInProgress := ioStats[11]
		if ioInProgress != "0" {
			ioIsBusy = true
		}

		log.Debugf(fmt.Sprintf("%s io stats: ", ioStats[2]))
		for i, key := range []string{
			"major_num", "minor_num", "dev_name", "read_completed", "read_merged", "read_sectors",
			"read_milliseconds", "write_completed", "write_merged", "write_sectors", "write_milliseconds",
			"io_in_progress", "io_milliseconds", "io_weighted_milliseconds"} {
			log.Debugf(fmt.Sprintf("	%s: %s", key, ioStats[i]))
		}
	}
	return ioIsBusy, nil
}

func TranslateSectorToBytes(diskName string, sectorOffset int) (int, error) {
	hwSectorSize, err := DiskGetHwSectorSize(diskName)
	if err != nil {
		return 0, err
	}
	return sectorOffset * hwSectorSize, nil
}

func GetAlignedSectorOffset(diskName string, sectorOffset int) (int, error) {
	hwSectorSize, err := DiskGetHwSectorSize(diskName)
	if err != nil {
		return 0, err
	}
	physicalBlockSize, err := DiskGetPhysicalBlockSize(diskName)
	if err != nil {
		return 0, err
	}
	if physicalBlockSize == hwSectorSize {
		return sectorOffset, nil
	}
	physicalBlockSectorNum := physicalBlockSize / hwSectorSize
	if sectorOffset%physicalBlockSectorNum == 0 {
		return sectorOffset, nil
	}
	return ((sectorOffset / physicalBlockSectorNum) + 1) * physicalBlockSectorNum, nil
}

func getSectorOffsetFromSize(diskName, sizeStr string) (uint64, error) {
	hwSectorSize, err := DiskGetHwSectorSize(diskName)
	if err != nil {
		return 0, err
	}
	physicalBlockSize, err := DiskGetPhysicalBlockSize(diskName)
	if err != nil {
		return 0, err
	}
	byteSize, err := TranslateSizeStrToBytes(sizeStr)
	if err != nil {
		return 0, err
	}
	if byteSize < uint64(physicalBlockSize) {
		if byteSize%uint64(hwSectorSize) == 0 {
			return byteSize / uint64(hwSectorSize), nil
		}
	} else if byteSize%uint64(physicalBlockSize) == 0 {
		return byteSize / uint64(hwSectorSize), nil
	} else {
		alignedSize := ((byteSize / uint64(physicalBlockSize)) + 1) * uint64(physicalBlockSize)
		return alignedSize / uint64(hwSectorSize), nil
	}
	return 0, fmt.Errorf("size %s is not aligned to physical block size %d", sizeStr, physicalBlockSize)
}

func PartitionTypeStrToGUID(partitionTypeStr string) (string, error) {
	if guid, ok := partitionTypeNameToGUID[partitionTypeStr]; ok {
		return guid, nil
	}
	return "", fmt.Errorf("partition type not found: %s", partitionTypeStr)
}

func PartitionGUIDToTypeStr(partitionGUID string) (string, error) {
	for k, v := range partitionTypeNameToGUID {
		if v == partitionGUID {
			return k, nil
		}
	}
	return "", fmt.Errorf("partition GUID not found: %s", partitionGUID)
}

func diskPartitionCreate(
	diskPath string,
	partitionNum int,
	partitionInfo config.PartitionInfo,
	partitionTableType string,
	partitionType string) (string, error) {

	partitionTypeList := []string{"primary", "extended", "logical"}
	fsTypeList := []string{"fat32", "fat16", "vfat", "ext2", "ext3", "ext4", "xfs", "linux-swap"}

	// Partition info
	partitionName := partitionInfo.Name
	partitionID := partitionInfo.ID

	// Validate partition type
	if partitionTableType == "mbr" {
		if !slice.Contains(partitionTypeList, partitionType) {
			log.Errorf("Unknown partition type for MBR: %s", partitionType)
			return "", fmt.Errorf("unknown partition type: %s", partitionType)
		}
	} else if partitionTableType == "gpt" {
		if partitionInfo.Name != "" {
			partitionType = partitionName
		}
	}

	if partitionName == "" && partitionID != "" {
		partitionName = partitionID
	}

	log.Infof(fmt.Sprintf("Creating partition %d on disk %s for %s", partitionNum, diskPath, partitionName))

	startSizeStr, err := VerifyFileSize(partitionInfo.Start)
	if err != nil {
		log.Errorf("Invalid start size %s for partition %d: %v", partitionInfo.Start, partitionNum, err)
		return "", fmt.Errorf("invalid start size %s for partition %d: %w", partitionInfo.Start, partitionNum, err)
	}
	endSizeStr, err := VerifyFileSize(partitionInfo.End)
	if err != nil {
		log.Errorf("Invalid end size %s for partition %d: %v", partitionInfo.End, partitionNum, err)
		return "", fmt.Errorf("invalid end size %s for partition %d: %w", partitionInfo.End, partitionNum, err)
	}

	if !slice.Contains(fsTypeList, partitionInfo.FsType) {
		log.Errorf("Unknown fs type for partition %d: %s", partitionNum, partitionInfo.FsType)
		return "", fmt.Errorf("unknown fs type for partition %d: %s", partitionNum, partitionInfo.FsType)
	}

	diskName, err := GetDiskNameFromDiskPath(diskPath)
	if err != nil {
		log.Errorf("Failed to get disk name from path %s: %v", diskPath, err)
		return "", fmt.Errorf("failed to get disk name from path: %s", diskPath)
	}

	startSector, _ := getSectorOffsetFromSize(diskName, startSizeStr)
	var endSector uint64
	if partitionInfo.End == "0" {
		endSector = 0
	} else {
		endSector, _ = getSectorOffsetFromSize(diskName, endSizeStr)
		endSector--
	}

	if partitionType == "logical" {
		// extended partition takes one sector, the following logical partitions will be aligned to the next sector
		startSector++
		if endSector != 0 {
			endSector++
		}
	}

	startSectorStr := fmt.Sprintf("%ds", startSector)
	endSectorStr := fmt.Sprintf("%ds", endSector)
	log.Infof("Input partition start: " + startSizeStr + ", aligned start sector: " + startSectorStr)
	log.Infof("Input partition end: " + endSizeStr + ", aligned end sector: " + endSectorStr)

	// Create partition
	// GPT with sgdisk & MBR with sfdisk
	if partitionTableType == "gpt" {
		typeGUID := partitionInfo.TypeGUID
		if typeGUID == "" && partitionInfo.Type != "" {
			typeGUID, _ = PartitionTypeStrToGUID(partitionInfo.Type)
		}

		startArg := fmt.Sprintf("%d", startSector)
		var endArg string
		if endSector == 0 {
			endArg = "0"
		} else {
			endArg = fmt.Sprintf("%d", endSector)
		}

		// Build sgdisk command: -n (new), -t (type), -c (name)
		var parts []string
		parts = append(parts, fmt.Sprintf("-n %d:%s:%s", partitionNum, startArg, endArg))
		if typeGUID != "" {
			parts = append(parts, fmt.Sprintf("-t %d:%s", partitionNum, typeGUID))
		}
		if partitionName != "" {
			safeName := strings.ReplaceAll(partitionName, "\"", "\\\"")
			parts = append(parts, fmt.Sprintf("-c %d:\"%s\"", partitionNum, safeName))
		}

		cmdStr := fmt.Sprintf("sudo sgdisk %s %s", strings.Join(parts, " "), diskPath)
		_, err = shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to create GPT partition %d on disk %s: %v", partitionNum, diskPath, err)
			return "", fmt.Errorf("failed to create GPT partition %d on disk %s: %w", partitionNum, diskPath, err)
		}

	} else {
		var sfdiskScript strings.Builder
		sfdiskScript.WriteString(fmt.Sprintf("start=%d ", startSector))
		if endSector != 0 {
			size := endSector - startSector
			sfdiskScript.WriteString(fmt.Sprintf("size=%d ", size))
		}

		// Set partition type
		if partitionTableType == "mbr" {
			// For MBR, use hex type code
			var typeCode string
			switch {
			case partitionType == "extended":
				typeCode = "5"
			case partitionInfo.FsType == "linux-swap":
				typeCode = "82"
			default:
				typeCode = "83" // Linux
			}
			sfdiskScript.WriteString(fmt.Sprintf("type=%s ", typeCode))
		}

		// Handle boot flag
		for _, flag := range partitionInfo.Flags {
			if flag == "boot" {
				sfdiskScript.WriteString("bootable ")
				break
			}
		}

		// Create the partition using sfdisk
		cmdStr := fmt.Sprintf("echo '%s' | sudo sfdisk --no-reread --append %s",
			sfdiskScript.String(), diskPath)
		_, err = shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to create partition %d on disk %s: %v", partitionNum, diskPath, err)
			return "", fmt.Errorf("failed to create partition %d on disk %s: %w", partitionNum, diskPath, err)
		}
	}

	// Refresh partition table using partx
	cmdStr := fmt.Sprintf("partx -u %s", diskPath)
	_, err = shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to refresh partition table after creating partition %d: %v", partitionNum, err)
		return "", fmt.Errorf("failed to refresh partition table after creating partition %d: %w", partitionNum, err)
	}

	// Format partition
	var diskPartDev string
	if strings.Contains(diskPath, "loop") || strings.Contains(diskPath, "nvme") {
		diskPartDev = fmt.Sprintf("%sp%d", diskPath, partitionNum)
	} else {
		diskPartDev = fmt.Sprintf("%s%d", diskPath, partitionNum)
	}

	if partitionInfo.FsType == "fat32" || partitionInfo.FsType == "fat16" || partitionInfo.FsType == "vfat" {
		var fatTypeFlag string
		switch partitionInfo.FsType {
		case "fat32":
			fatTypeFlag = "-F 32"
		case "fat16":
			fatTypeFlag = "-F 16"
		default: // "vfat"
			fatTypeFlag = "" // Let mkfs.vfat auto-decide
		}

		if partitionInfo.FsLabel != "" {
			if fatTypeFlag != "" {
				cmdStr = fmt.Sprintf("mkfs -t vfat %s -n %s %s", fatTypeFlag, partitionInfo.FsLabel, diskPartDev)
			} else {
				cmdStr = fmt.Sprintf("mkfs -t vfat -n %s %s", partitionInfo.FsLabel, diskPartDev)
			}
		} else {
			if fatTypeFlag != "" {
				cmdStr = fmt.Sprintf("mkfs -t vfat %s %s", fatTypeFlag, diskPartDev)
			} else {
				cmdStr = fmt.Sprintf("mkfs -t vfat %s", diskPartDev)
			}
		}
		_, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to format partition %d with fs type %s: %v", partitionNum, partitionInfo.FsType, err)
			return "", fmt.Errorf("failed to format partition %d with fs type %s: %w", partitionNum, partitionInfo.FsType, err)
		}
	} else if partitionInfo.FsType == "ext2" || partitionInfo.FsType == "ext3" || partitionInfo.FsType == "ext4" || partitionInfo.FsType == "xfs" {
		var additionalFlags string
		switch partitionInfo.FsType {
		case "ext2":
			additionalFlags = "-b 4096 -O none,sparse_super,large_file,filetype,resize_inode,dir_index,ext_attr"
		case "ext3":
			additionalFlags = "-b 4096 -O none,sparse_super,large_file,filetype,resize_inode,dir_index,ext_attr,has_journal"
		case "ext4":
			additionalFlags = "-b 4096 -O none,sparse_super,large_file,filetype,resize_inode,dir_index,ext_attr,has_journal,extent,huge_file,flex_bg,metadata_csum,64bit,dir_nlink,extra_isize"
		}
		var labelFlag string
		if partitionInfo.FsLabel != "" {
			labelFlag = fmt.Sprintf("-L %s", partitionInfo.FsLabel)
		}
		if additionalFlags != "" && labelFlag != "" {
			cmdStr = fmt.Sprintf("mkfs -t %s %s %s %s", partitionInfo.FsType, labelFlag, additionalFlags, diskPartDev)
		} else if additionalFlags != "" {
			cmdStr = fmt.Sprintf("mkfs -t %s %s %s", partitionInfo.FsType, additionalFlags, diskPartDev)
		} else if labelFlag != "" {
			cmdStr = fmt.Sprintf("mkfs -t %s %s %s", partitionInfo.FsType, labelFlag, diskPartDev)
		} else {
			cmdStr = fmt.Sprintf("mkfs -t %s %s", partitionInfo.FsType, diskPartDev)
		}
		_, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to format partition %d with fs type %s: %v", partitionNum, partitionInfo.FsType, err)
			return "", fmt.Errorf("failed to format partition %d with fs type %s: %w", partitionNum, partitionInfo.FsType, err)
		}
	} else if partitionInfo.FsType == "linux-swap" {
		if partitionInfo.FsLabel != "" {
			cmdStr = fmt.Sprintf("mkswap -L %s %s", partitionInfo.FsLabel, diskPartDev)
		} else {
			cmdStr = fmt.Sprintf("mkswap %s", diskPartDev)
		}
		_, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to format partition %d with fs type %s: %v", partitionNum, partitionInfo.FsType, err)
			return "", fmt.Errorf("failed to format partition %d with fs type %s: %w", partitionNum, partitionInfo.FsType, err)
		}
		cmdStr = fmt.Sprintf("swapon %s", diskPartDev)
		_, err = shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to enable swap on partition %d: %v", partitionNum, err)
			return "", fmt.Errorf("failed to enable swap on partition %d: %w", partitionNum, err)
		}
	}

	return diskPartDev, nil
}

func diskPartitionDelete(diskPath string, partitionNum int) error {
	if partitionNum < 1 {
		log.Errorf("Invalid partition number: %d", partitionNum)
		return fmt.Errorf("invalid partition number: %d", partitionNum)
	}
	cmdStr := fmt.Sprintf("sfdisk --delete %s %d", diskPath, partitionNum)
	_, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to delete partition %d: %v", partitionNum, err)
		return fmt.Errorf("failed to delete partition %d: %w", partitionNum, err)
	}

	// Refresh partition table
	cmdStr = fmt.Sprintf("partx -d --nr %d %s", partitionNum, diskPath)
	_, err = shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
	if err != nil {
		// Non-fatal if partition is already gone
		log.Warnf("Could not remove partition %d from kernel table: %v", partitionNum, err)
	}

	return nil
}

func DiskPartitionsCreate(diskPath string, partitionsList []config.PartitionInfo, partitionTableType string) (map[string]string, error) {
	partIDDiskDevMap := make(map[string]string)

	partitionExist, err := IsDiskPartitionExist(diskPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check if disk %s has partitions: %w", diskPath, err)
	}
	if partitionExist {
		// Wipe the disk first
		log.Infof(fmt.Sprintf("Disk %s already has partitions, wiping it before creating new partitions", diskPath))
		if err := WipePartitions(diskPath); err != nil {
			return nil, fmt.Errorf("failed to wipe disk before creating partitions: %w", err)
		}
	}

	if partitionTableType == "gpt" {
		cmdStr := fmt.Sprintf("echo 'label: gpt' | sudo sfdisk %s", diskPath)
		_, err := shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to create GPT partition table on disk %s: %v", diskPath, err)
			return nil, fmt.Errorf("failed to create GPT partition table on disk %s: %w", diskPath, err)
		}

		indexPlaceholder := map[int]string{}
		for _, p := range partitionsList {
			if p.Index != nil {
				if *p.Index <= 0 {
					return nil, fmt.Errorf("partition %q: index must be > 0 (got %d)", p.ID, *p.Index)
				}
				if prev, ok := indexPlaceholder[*p.Index]; ok {
					return nil, fmt.Errorf("duplicate partition index %d used by %q and %q", *p.Index, prev, p.ID)
				}
				indexPlaceholder[*p.Index] = p.ID
			}
		}

		var partitionNum int
		for i, partitionInfo := range partitionsList {
			if partitionInfo.Index != nil {
				partitionNum = *partitionInfo.Index
			} else {
				assignedIndex := i + 1
				for {
					if _, used := indexPlaceholder[assignedIndex]; !used {
						break
					}
					assignedIndex++
				}
				partitionNum = assignedIndex
			}
			diskPartDev, err := diskPartitionCreate(diskPath, partitionNum, partitionInfo, partitionTableType, "primary")
			if err != nil {
				for i := 1; i < partitionNum; i++ {
					// Clean up previously created partitions if any
					if err := diskPartitionDelete(diskPath, i); err != nil {
						log.Errorf(fmt.Sprintf("%v", err))
					}
				}
				return nil, fmt.Errorf("failed to create partition %d: %w", partitionNum, err)
			}
			partIDDiskDevMap[partitionInfo.ID] = diskPartDev
		}
	} else if partitionTableType == "mbr" {
		var partitionType string
		var partitionNum int
		maxPrimaryPartitionsNum := 4
		cmdStr := fmt.Sprintf("echo 'label: dos' | sudo sfdisk %s", diskPath)
		_, err := shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
		if err != nil {
			log.Errorf("Failed to create MBR partition table on disk %s: %v", diskPath, err)
			return nil, fmt.Errorf("failed to create MBR partition table on disk %s: %w", diskPath, err)
		}

		partitionCount := len(partitionsList)
		for i, partitionInfo := range partitionsList {
			if i >= maxPrimaryPartitionsNum-1 && partitionCount > maxPrimaryPartitionsNum {
				// If we have more than 4 partitions, the last one will be an extended partition
				if i == maxPrimaryPartitionsNum-1 {
					partitionType = "extended"
					partitionNum = i + 1
					logicalPartitionEnd := partitionInfo.End
					extendedPartitionEnd := partitionsList[partitionCount-1].End
					partitionInfo.End = extendedPartitionEnd
					_, err := diskPartitionCreate(diskPath, partitionNum, partitionInfo, partitionTableType, partitionType)
					if err != nil {
						for i := 1; i < partitionNum; i++ {
							// Clean up previously created partitions if any
							if err := diskPartitionDelete(diskPath, i); err != nil {
								log.Errorf(fmt.Sprintf("%v", err))
							}
						}
						return nil, fmt.Errorf("failed to create extended partition %d: %w", partitionNum, err)
					}
					partitionInfo.End = logicalPartitionEnd
					partitionType = "logical"
					partitionNum = i + 1
				} else {
					// For logical partitions, we can create multiple logical partitions within the extended partition
					partitionType = "logical"
					partitionNum = i + 1
				}
			} else {
				// For primary partitions, we can create up to 4 primary partitions
				partitionType = "primary"
				partitionNum = i + 1
			}
			diskPartDev, err := diskPartitionCreate(diskPath, partitionNum, partitionInfo, partitionTableType, partitionType)
			if err != nil {
				for i := 1; i < partitionNum; i++ {
					// Clean up previously created partitions if any
					if err := diskPartitionDelete(diskPath, i); err != nil {
						log.Errorf(fmt.Sprintf("%v", err))
					}
				}
				return nil, fmt.Errorf("failed to create partition %d: %w", partitionNum, err)
			}
			partIDDiskDevMap[partitionInfo.ID] = diskPartDev
		}
	}
	return partIDDiskDevMap, nil
}

func GetPartitionLabel(diskPartDev string) (string, error) {
	cmdStr := fmt.Sprintf("blkid %s -s PARTLABEL -o value", diskPartDev)
	label, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get partition label for %s: %v", diskPartDev, err)
		return "", fmt.Errorf("failed to get partition label for %s: %w", diskPartDev, err)
	}
	return strings.TrimSpace(label), nil
}

func WipePartitions(diskPath string) error {
	// Wipe filesystem signatures
	_, err := shell.ExecCmd(fmt.Sprintf("wipefs -a -f %s", diskPath), true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to wipe filesystem signatures on disk %s: %v", diskPath, err)
		return fmt.Errorf("failed to wipe disk %s: %w", diskPath, err)
	}

	_, err = shell.ExecCmd("sync", true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to sync after wiping disk %s: %v", diskPath, err)
		return fmt.Errorf("failed to sync after wiping disk %s: %w", diskPath, err)
	}
	return nil
}

func GetUUID(diskPartitionPath string) (string, error) {
	cmd := fmt.Sprintf("blkid %s -s UUID -o value", diskPartitionPath)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get UUID for %s: %v", diskPartitionPath, err)
		return output, fmt.Errorf("failed to get partition UUID for %s: %w", diskPartitionPath, err)
	}
	return strings.TrimSpace(output), nil
}

func GetPartUUID(diskPartitionPath string) (string, error) {
	cmd := fmt.Sprintf("blkid %s -s PARTUUID -o value", diskPartitionPath)
	output, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to get PARTUUID for %s: %v", diskPartitionPath, err)
		return output, fmt.Errorf("failed to get partition UUID for %s: %w", diskPartitionPath, err)
	}
	return strings.TrimSpace(output), nil
}

// SystemBlockDevices returns all block devices on the host system.
func SystemBlockDevices() (systemDevices []SystemBlockDevice, err error) {
	const (
		scsiDiskMajorNumber      = "8"
		mmcBlockMajorNumber      = "179"
		virtualDiskMajorNumber   = "252,253,254"
		blockExtendedMajorNumber = "259"
	)

	blockDeviceMajorNumbers := []string{scsiDiskMajorNumber, mmcBlockMajorNumber, virtualDiskMajorNumber, blockExtendedMajorNumber}
	cmd := fmt.Sprintf("lsblk -d --bytes -I %s -n --json --output NAME,SIZE,MODEL", strings.Join(blockDeviceMajorNumbers, ","))
	rawDiskOutput, err := shell.ExecCmd(cmd, true, shell.HostPath, nil)
	if err != nil {
		log.Errorf("Failed to execute lsblk command: %v", err)
		return nil, fmt.Errorf("failed to execute lsblk command: %w", err)
	}

	var blockDevices blockDevicesOutput
	if rawDiskOutput != "" {
		err = json.Unmarshal([]byte(rawDiskOutput), &blockDevices)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal lsblk output: %w", err)
		}
	}

	if len(blockDevices.Devices) <= 0 {
		err = fmt.Errorf("no supported disks found")
		return
	}

	// Process each device to build the filtered list
	systemDevices = []SystemBlockDevice{}
	for _, device := range blockDevices.Devices {
		devicePath := fmt.Sprintf("/dev/%s", device.Name)
		rawSize, err := strconv.ParseUint(device.Size.String(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse size for %s: %v", devicePath, err)
		}

		isISOInstaller := isReadOnlyISO(devicePath)

		log.Debugf("Device: %s, Size: %d, Model: %s, isISOInstaller : %v ",
			devicePath, rawSize, strings.TrimSpace(device.Model), isISOInstaller)

		if !isISOInstaller {
			systemDevices = append(systemDevices, SystemBlockDevice{
				DevicePath:  devicePath,
				RawDiskSize: rawSize,
				Model:       strings.TrimSpace(device.Model),
			})
		} else {
			log.Debugf("Excluded removable installer device: %s", devicePath)
		}
	}

	log.Debugf("Final device list: %v", systemDevices)
	return systemDevices, nil
}

// isReadOnlyISO checks if a device is mounted read-only (ISO on USB/CD).
func isReadOnlyISO(devicePath string) bool {
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		log.Debugf("Failed to read /proc/mounts: %v", err)
		return false
	}
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == devicePath && fields[2] == "iso9660" {
			options := strings.Split(fields[3], ",")
			for _, opt := range options {
				if opt == "ro" {
					return true
				}
			}
		}
	}
	return false
}

// BootPartitionConfig returns the partition flags and mount point that should be used
// for a given boot type.
func BootPartitionConfig(bootType string, partitionTableType string) (mountPoint, mountOptions string, flags []string, err error) {
	switch bootType {
	case EFIPartitionType:
		flags = []string{PartitionFlagESP, PartitionFlagBoot}
		mountPoint = "/boot/efi"
		mountOptions = "umask=0077,nodev"
	case LegacyPartitionType:
		if partitionTableType == PartitionTableTypeGpt {
			flags = []string{PartitionFlagGrub}
		} else if partitionTableType == PartitionTableTypeMbr {
			flags = []string{PartitionFlagBoot}
		} else {
			err = fmt.Errorf("unknown partition table type (%s)", partitionTableType)
		}

		mountPoint = ""
		mountOptions = ""
	default:
		err = fmt.Errorf("unknown boot type (%s)", bootType)
	}

	return
}
