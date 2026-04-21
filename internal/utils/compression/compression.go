package compression

import (
	"fmt"
	"path/filepath"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

func DecompressFile(decompressPath, outputPath, decompressType string, sudo bool) error {
	dirName := filepath.Dir(decompressPath)
	fileName := filepath.Base(decompressPath)
	var cmdStr string
	var sudoStr string = ""
	var err error

	if sudo {
		sudoStr = "sudo"
	}

	switch decompressType {
	case "tar.xz":
		// cd is a shell built-in command, can't be used with sudo.
		cmdStr = fmt.Sprintf("cd %s && %s tar -xJf %s -C %s", dirName, sudoStr, fileName, outputPath)
		_, err = shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
	case "tar.gz":
		// cd is a shell built-in command, can't be used with sudo.
		cmdStr = fmt.Sprintf("cd %s && %s tar -xzf %s -C %s", dirName, sudoStr, fileName, outputPath)
		_, err = shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
	case "gz":
		cmdStr = fmt.Sprintf("gzip -d -c %s > %s", decompressPath, outputPath)
		_, err = shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil)
	case "xz":
		cmdStr = fmt.Sprintf("xz -d -c %s > %s", decompressPath, outputPath)
		_, err = shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil)
	case "zstd":
		cmdStr = fmt.Sprintf("zstd -d -c %s > %s", decompressPath, outputPath)
		_, err = shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil)
	default:
		return fmt.Errorf("unsupported decompression type: %s", decompressType)
	}

	return err
}

func CompressFile(compressPath, outputPath, compressType string, sudo bool) error {
	dirName := filepath.Dir(compressPath)
	fileName := filepath.Base(compressPath)
	var cmdStr string
	var sudoStr string = ""
	var err error

	if sudo {
		sudoStr = "sudo"
	}

	switch compressType {
	case "tar.xz":
		cmdStr = fmt.Sprintf("cd %s && %s tar -cJf %s %s", dirName, sudoStr, outputPath, fileName)
		_, err = shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
	case "tar.gz":
		cmdStr = fmt.Sprintf("cd %s && %s tar -czf %s %s", dirName, sudoStr, outputPath, fileName)
		_, err = shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
	case "gz":
		cmdStr = fmt.Sprintf("gzip -c %s > %s", compressPath, outputPath)
		_, err = shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil)
	case "xz":
		cmdStr = fmt.Sprintf("xz -z -c %s > %s", compressPath, outputPath)
		_, err = shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil)
	case "zstd":
		cmdStr = fmt.Sprintf("zstd --threads=0 -f -o %s %s", outputPath, compressPath)
		_, err = shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil)
	default:
		return fmt.Errorf("unsupported compression type: %s", compressType)
	}

	return err
}

func CompressFolder(compressPath, outputPath, compressType string, sudo bool) error {
	var cmdStr string

	switch compressType {
	case "tar.xz":
		cmdStr = fmt.Sprintf("tar -cJf %s -C %s .", outputPath, compressPath)
	case "tar.gz":
		cmdStr = fmt.Sprintf("tar -czf %s -C %s .", outputPath, compressPath)
	default:
		return fmt.Errorf("unsupported compression type: %s", compressType)
	}
	_, err := shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil)
	return err
}
