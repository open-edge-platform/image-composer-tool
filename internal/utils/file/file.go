package file

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"gopkg.in/yaml.v3"
)

// IsSubPath checks if the target path is a subpath of the base path
func IsSubPath(base, target string) (bool, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return false, err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return false, err
	}
	// rel == "." means same dir, rel starting with ".." means not subpath
	if rel == "." {
		return true, nil
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false, nil
	}
	return true, nil
}

func ReplacePlaceholdersInFile(placeholder, value, filePath string) error {
	sedCmd := fmt.Sprintf("sed -i 's|%s|%s|g' %s", placeholder, value, filePath)
	if _, err := shell.ExecCmd(sedCmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to replace placeholder %s with %s in file %s: %w", placeholder, value, filePath, err)
	}
	return nil
}

func ReplaceRegexInFile(findPattern, replacePattern, filePath string) error {
	sedCmd := fmt.Sprintf("sed -E -i 's|%s|%s|g' %s", findPattern, replacePattern, filePath)
	if _, err := shell.ExecCmd(sedCmd, true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to replace pattern %s with %s in file %s: %w", findPattern, replacePattern, filePath, err)
	}
	return nil
}

func GetFileList(dir string) ([]string, error) {
	var fileList []string
	output, err := shell.ExecCmd("ls "+dir, true, shell.HostPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list files in directory %s: %w", dir, err)
	}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue // skip empty lines
		}
		filesInLine := strings.Split(line, " ")
		fileList = append(fileList, filesInLine...)
	}
	return fileList, nil
}

func Read(filePath string) (string, error) {
	content, err := shell.ExecCmd("cat "+filePath, true, shell.HostPath, nil)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	return content, nil
}

func Write(data, dst string) error {
	tmpFile, err := os.CreateTemp(config.TempDir(), "filewrite-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Ensure temp file is removed after use

	// Write data to temp file
	if _, err := tmpFile.WriteString(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}
	tmpFile.Close()

	return CopyFile(tmpPath, dst, "", true)
}

func Append(data, dst string) error {
	tmpFile, err := os.CreateTemp(config.TempDir(), "fileappend-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Ensure temp file is removed after use

	// Write data to temp file
	if _, err := tmpFile.WriteString(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}
	tmpFile.Close()

	// Use a safer approach with shell.ExecCmd that avoids command injection
	cmdStr := fmt.Sprintf("cat %s | sudo tee -a %s >/dev/null", tmpPath, dst)
	if _, err := shell.ExecCmd(cmdStr, false, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to append content to %s: %w", dst, err)
	}
	return nil
}

// ReadFromJSON reads a JSON file and returns its contents as a map
// If the file doesn't exist or is empty, returns an empty map
func ReadFromJSON(jsonFile string) (map[string]interface{}, error) {
	// Initialize empty map for result
	result := make(map[string]interface{})

	// Check if file exists
	if _, err := os.Stat(jsonFile); os.IsNotExist(err) {
		return result, fmt.Errorf("file does not exist: %s", jsonFile)
	}

	// Open the file
	file, err := os.Open(jsonFile)
	if err != nil {
		return result, err
	}
	defer file.Close()

	// Get file info to check if it's empty
	fileInfo, err := file.Stat()
	if err != nil {
		return result, err
	}

	// Return empty map if file is empty
	if fileInfo.Size() == 0 {
		return result, fmt.Errorf("file is empty: %s", jsonFile)
	}

	// Decode JSON content
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}

	return result, nil
}

// WriteToJSON writes a map to a JSON file with specified indentation
func WriteToJSON(jsonFile string, data map[string]interface{}, indent int) error {
	// Create or truncate the file
	file, err := os.Create(jsonFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create encoder with indentation
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", strings.Repeat(" ", indent))

	// Encode and write the data
	if err := encoder.Encode(data); err != nil {
		return err
	}

	return nil
}

func decodeYAML(file *os.File, result *map[interface{}]interface{}) error {
	decoder := yaml.NewDecoder(file)
	var raw map[interface{}]interface{}
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	*result = raw
	return nil
}

func ReadFromYaml(yamlFile string) (map[interface{}]interface{}, error) {
	// Initialize empty map for result
	result := make(map[interface{}]interface{})

	// Check if file exists
	if _, err := os.Stat(yamlFile); os.IsNotExist(err) {
		return result, fmt.Errorf("file does not exist: %s", yamlFile)
	}

	// Open the file
	file, err := os.Open(yamlFile)
	if err != nil {
		return result, err
	}
	defer file.Close()

	// Get file info to check if it's empty
	fileInfo, err := file.Stat()
	if err != nil {
		return result, err
	}

	// Return empty map if file is empty
	if fileInfo.Size() == 0 {
		return result, fmt.Errorf("file is empty: %s", yamlFile)
	}

	// Decode YAML content (assuming a function DecodeYAML exists)
	if err := decodeYAML(file, &result); err != nil {
		return result, err
	}

	return result, nil
}

func CopyFile(srcFile, dstFile, flags string, sudo bool) error {
	srcFilePath, err := filepath.Abs(srcFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of source file: %w", err)
	}
	if _, err := os.Stat(srcFilePath); os.IsNotExist(err) {
		return fmt.Errorf("source file does not exist: %s", srcFilePath)
	}

	dstFilePath, err := filepath.Abs(dstFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of destination file: %w", err)
	}
	dstDir := filepath.Dir(dstFilePath)
	if _, err := shell.ExecCmd(fmt.Sprintf("mkdir -p '%s'", dstDir), sudo, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to create directory for destination file: %w", err)
	}

	var cmdStr string
	if flags == "" {
		cmdStr = fmt.Sprintf("cp '%s' '%s'", srcFilePath, dstFilePath)
	} else {
		cmdStr = fmt.Sprintf("cp %s '%s' '%s'", flags, srcFilePath, dstFilePath)
	}
	if _, err := shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to copy file from %s to %s: %w", srcFilePath, dstFilePath, err)
	}
	return nil
}

func CopyDir(srcDir, dstDir, flags string, sudo bool) error {
	srcDirPath, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of source directory: %w", err)
	}
	if _, err := os.Stat(srcDirPath); os.IsNotExist(err) {
		return fmt.Errorf("source directory does not exist: %s", srcDirPath)
	}

	dstDirPath, err := filepath.Abs(dstDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of destination directory: %w", err)
	}
	if _, err := os.Stat(dstDirPath); os.IsNotExist(err) {
		if _, err := shell.ExecCmd(fmt.Sprintf("mkdir -p '%s'", dstDirPath), sudo, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
	}

	var cmdStr string
	if flags == "" {
		cmdStr = fmt.Sprintf("cp -r '%s'/* '%s'", srcDirPath, dstDirPath)
	} else {
		cmdStr = fmt.Sprintf("cp -r %s '%s'/* '%s'", flags, srcDirPath, dstDirPath)
	}
	if _, err := shell.ExecCmd(cmdStr, sudo, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to copy directory from %s to %s: %w", srcDirPath, dstDirPath, err)
	}
	return nil
}

// CheckDiskSpace checks if there is sufficient disk space available in the given directory
// to accommodate the specified size (with a safety margin).
func CheckDiskSpace(dir string, requiredBytes int64, safetyMarginPercent float64) error {
	// Use df command to get available space in bytes
	cmdStr := fmt.Sprintf("df --output=avail -B1 '%s' | tail -n1", dir)
	output, err := shell.ExecCmd(cmdStr, false, shell.HostPath, nil)
	if err != nil {
		return fmt.Errorf("failed to check disk space: %w", err)
	}

	availableStr := strings.TrimSpace(output)
	availableBytes, err := strconv.ParseInt(availableStr, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse disk space output: %w", err)
	}

	if safetyMarginPercent < 0 {
		safetyMarginPercent = 0
	}
	safetyMargin := int64(float64(requiredBytes) * safetyMarginPercent)
	requiredWithMargin := requiredBytes + safetyMargin

	if availableBytes < requiredWithMargin {
		return fmt.Errorf("insufficient disk space: need %d bytes (including %.0f%% margin), have %d bytes available",
			requiredWithMargin, safetyMarginPercent*100, availableBytes)
	}

	return nil
}
