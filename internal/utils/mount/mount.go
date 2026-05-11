package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/slice"
)

var log = logger.Logger()

func GetMountPathList() ([]string, error) {
	var mountPathList []string
	output, err := shell.ExecCmdSilent("mount", false, shell.HostPath, nil)
	if err != nil {
		return mountPathList, err
	}
	if output != "" {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			mountInfoList := strings.Fields(line)
			if len(mountInfoList) > 2 {
				mountPathList = append(mountPathList, mountInfoList[2])
			}
		}
	}
	return mountPathList, nil
}

// GetMountSubPathList returns a list of mount points that are subdirectories of the specified root mount point
func GetMountSubPathList(rootMountPoint string) ([]string, error) {
	var mountSubpathList []string
	mountPathList, err := GetMountPathList()
	if err != nil {
		return mountSubpathList, fmt.Errorf("failed to get mount path list: %w", err)
	}
	for _, mountPath := range mountPathList {
		if strings.HasPrefix(mountPath, rootMountPoint) {
			mountSubpathList = append(mountSubpathList, mountPath)
		}
	}
	return mountSubpathList, nil
}

// IsMountPathExist checks if a given path is currently mounted
func IsMountPathExist(mountPoint string) (bool, error) {
	mountPathList, err := GetMountPathList()
	if err != nil {
		return false, fmt.Errorf("failed to get mount path list: %w", err)
	}
	for _, path := range mountPathList {
		if path == mountPoint {
			return true, nil
		}
	}

	return false, nil
}

// MountPath mounts a target path to a mount point with specific flags
func MountPath(targetPath, mountPoint, mountFlags string) error {
	if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
		if _, err := shell.ExecCmd("mkdir -p "+mountPoint, true, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to create mount point %s: %w", mountPoint, err)
		}
	}
	pathExist, err := IsMountPathExist(mountPoint)
	if err != nil {
		return fmt.Errorf("failed to check if mount point %s exists: %w", mountPoint, err)
	}
	if !pathExist {
		mountCmdStr := "mount " + mountFlags + " " + targetPath + " " + mountPoint
		if _, err := shell.ExecCmd(mountCmdStr, true, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to mount %s to %s: %w", targetPath, mountPoint, err)
		} else {
			log.Debugf("Mounted:", targetPath, "to", mountPoint)
		}
	} else {
		log.Debugf("Mount point already exists:", mountPoint)
	}
	return nil
}

func umountPath(mountPoint string) error {
	// Try different unmount strategies with increasing aggressiveness
	unmountStrategies := []struct {
		cmd  string
		desc string
	}{
		{"umount " + mountPoint, "standard"},
		{"umount -l " + mountPoint, "lazy"},
		{"umount -f " + mountPoint, "force"},
		{"umount -lf " + mountPoint, "lazy-force"},
	}
	for _, strategy := range unmountStrategies {
		log.Debugf("Trying %s unmount for %s", strategy.desc, mountPoint)
		if output, err := shell.ExecCmd(strategy.cmd, true, shell.HostPath, nil); err == nil {
			log.Debugf("Successfully unmounted %s using %s approach", mountPoint, strategy.desc)
			return nil
		} else {
			log.Debugf("Unmount failed with %s approach: %v, output: %s", strategy.desc, err, output)
		}
	}
	return nil
}

func UmountPath(mountPoint string) error {
	pathExist, err := IsMountPathExist(mountPoint)
	if err != nil {
		return fmt.Errorf("failed to check if mount point %s exists: %w", mountPoint, err)
	}
	if !pathExist {
		log.Debugf("Mount point does not exist:", mountPoint)
		return nil
	}
	return umountPath(mountPoint)
}

func UmountAndDeletePath(mountPoint string) error {
	if err := UmountPath(mountPoint); err != nil {
		return fmt.Errorf("failed to unmount %s: %w", mountPoint, err)
	}
	if _, err := shell.ExecCmd("rm -rf "+mountPoint, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to remove mount point directory %s: %w", mountPoint, err)
	}
	return nil
}

func UmountSubPath(mountPoint string) error {
	mountSubpathList, err := GetMountSubPathList(mountPoint)
	if err != nil {
		return fmt.Errorf("failed to get mount subpath list for %s: %w", mountPoint, err)
	}

	if len(mountSubpathList) == 0 {
		log.Debugf("No mount subpaths found for %s", mountPoint)
		return nil
	}

	sort.Sort(sort.Reverse(sort.StringSlice(mountSubpathList)))
	for _, path := range mountSubpathList {
		if err := umountPath(path); err != nil {
			return fmt.Errorf("failed to unmount %s: %w", path, err)
		}
	}
	return nil
}

// MountSysfs mounts system directories (e.g., /dev, /proc, /sys) into the chroot environment
func MountSysfs(mountPoint string) error {
	procMountPoint := filepath.Join(mountPoint, "proc")
	if err := MountPath("proc", procMountPoint, "-t proc"); err != nil {
		return fmt.Errorf("failed to mount /proc to %s: %w", procMountPoint, err)
	}

	sysMountPoint := filepath.Join(mountPoint, "sys")
	if err := MountPath("sysfs", sysMountPoint, "-t sysfs -o nosuid,noexec,nodev"); err != nil {
		return fmt.Errorf("failed to mount /sys to %s: %w", sysMountPoint, err)
	}

	devMountPoint := filepath.Join(mountPoint, "dev")
	if err := MountPath("devtmpfs", devMountPoint, "-t devtmpfs -o mode=0700,nosuid"); err != nil {
		return fmt.Errorf("failed to mount /dev to %s: %w", devMountPoint, err)
	}

	devPtsMountPoint := filepath.Join(mountPoint, "dev/pts")
	if err := MountPath("devpts", devPtsMountPoint, "-t devpts -o gid=5,mode=620"); err != nil {
		return fmt.Errorf("failed to mount /dev/pts to %s: %w", devPtsMountPoint, err)
	}

	devShmMountPoint := filepath.Join(mountPoint, "dev/shm")
	if err := MountPath("tmpfs", devShmMountPoint, "-t tmpfs -o mode=1700"); err != nil {
		return fmt.Errorf("failed to mount /dev/shm to %s: %w", devShmMountPoint, err)
	}

	runMountPoint := filepath.Join(mountPoint, "run")
	if err := MountPath("tmpfs", runMountPoint, "-t tmpfs -o mode=0700"); err != nil {
		return fmt.Errorf("failed to mount /run to %s: %w", runMountPoint, err)
	}

	runShmMountPoint := filepath.Join(mountPoint, "run/shm")
	if _, err := shell.ExecCmd("mkdir -p "+runShmMountPoint, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to create %s: %w", runShmMountPoint, err)
	}
	if _, err := shell.ExecCmd("chmod 1700 "+runShmMountPoint, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", runShmMountPoint, err)
	}

	runLockMountPoint := filepath.Join(mountPoint, "run/lock")
	if _, err := shell.ExecCmd("mkdir -p "+runLockMountPoint, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to create %s: %w", runLockMountPoint, err)
	}

	return nil
}

// UmountSysfs unmounts system directories from the chroot environment
func UmountSysfs(mountPoint string) error {
	var pathList []string
	mountPathList, err := GetMountPathList()
	if err != nil {
		return fmt.Errorf("failed to get mount path list: %w", err)
	}
	if len(mountPathList) == 0 {
		log.Debugf("No mount points found")
		return nil
	}

	for _, path := range mountPathList {
		if strings.Contains(path, mountPoint) {
			pathList = append(pathList, path)
		}
	}

	for _, _mountPoint := range []string{"run", "dev/pts", "dev/shm", "dev", "sys", "proc"} {
		fullPath := filepath.Join(mountPoint, _mountPoint)
		if slice.Contains(pathList, fullPath) {
			if err := umountPath(fullPath); err != nil {
				// Only treat as error if not "not found"
				if !strings.Contains(err.Error(), "not found") {
					return fmt.Errorf("failed to unmount %s: %w", fullPath, err)
				} else {
					log.Warnf("Mount point not found (already unmounted?): %s", fullPath)
				}
			} else {
				log.Debugf("Unmounted: %s", fullPath)
			}
		}
	}
	return nil
}

// CleanSysfs cleans up system directories in the chroot environment
func CleanSysfs(mountPoint string) error {
	var pathList []string
	mountPathList, err := GetMountPathList()
	if err != nil {
		return fmt.Errorf("failed to get mount path list: %w", err)
	}

	for _, path := range mountPathList {
		if strings.Contains(path, mountPoint) {
			pathList = append(pathList, path)
		}
	}

	for _, _mountPoint := range []string{"run", "sys", "proc", "dev"} {
		fullPath := filepath.Join(mountPoint, _mountPoint)
		if !slice.Contains(pathList, fullPath) {
			if _, err := shell.ExecCmd("rm -rf "+fullPath, true, shell.HostPath, nil); err != nil {
				return fmt.Errorf("failed to remove path %s: %w", fullPath, err)
			}
		} else {
			return fmt.Errorf("failed to remove path: %s still mounted", fullPath)
		}
	}

	return nil
}
