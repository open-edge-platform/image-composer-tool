package shell

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

const HostPath string = "/"

var log = logger.Logger()

var commandMap = map[string][]string{
	"apt":                {"/usr/bin/apt"},
	"apt-cache":          {"/usr/bin/apt-cache"},
	"apt-get":            {"/usr/bin/apt-get"},
	"basename":           {"/usr/bin/basename"},
	"bash":               {"/usr/bin/bash"},
	"blkid":              {"/usr/sbin/blkid"},
	"bootctl":            {"/usr/bin/bootctl"},
	"bunzip2":            {"/usr/bin/bunzip2"},
	"cat":                {"/bin/cat"},
	"cd":                 {"cd"}, // 'cd' is a shell builtin, not a standalone command
	"chroot":             {"/usr/sbin/chroot"},
	"chmod":              {"/usr/bin/chmod"},
	"chown":              {"/usr/bin/chown"},
	"command":            {"command"}, // 'command' is a shell builtin
	"cp":                 {"/bin/cp", "/usr/bin/cp"},
	"crontab":            {"/usr/bin/crontab"},
	"curl":               {"/usr/bin/curl"},
	"createrepo_c":       {"/usr/bin/createrepo_c"},
	"cryptsetup":         {"/usr/sbin/cryptsetup"},
	"date":               {"/usr/bin/date"},
	"dd":                 {"/usr/bin/dd"},
	"df":                 {"/usr/bin/df"},
	"dirname":            {"/usr/bin/dirname"},
	"debugfs":            {"/usr/sbin/debugfs", "/usr/bin/debugfs"},
	"dnf":                {"/usr/bin/dnf"},
	"dpkg":               {"/usr/bin/dpkg"},
	"dpkg-divert":        {"/usr/bin/dpkg-divert"},
	"dpkg-scanpackages":  {"/usr/bin/dpkg-scanpackages"},
	"echo":               {"/bin/echo", "/usr/bin/echo"},
	"e2fsck":             {"/usr/sbin/e2fsck"},
	"efibootmgr":         {"/usr/bin/efibootmgr", "/sbin/efibootmgr"},
	"eject":              {"/usr/bin/eject"},
	"fallocate":          {"/usr/bin/fallocate"},
	"fdisk":              {"/usr/sbin/fdisk"},
	"find":               {"/usr/bin/find"},
	"findmnt":            {"/usr/bin/findmnt"},
	"flock":              {"/usr/bin/flock"},
	"fuser":              {"/usr/bin/fuser"},
	"getent":             {"/usr/bin/getent"},
	"gpgconf":            {"/usr/bin/gpgconf"},
	"groupadd":           {"/usr/sbin/groupadd"},
	"gunzip":             {"/usr/bin/gunzip"},
	"grep":               {"/usr/bin/grep", "/bin/grep"},
	"grub-mkconfig":      {"/usr/sbin/grub-mkconfig"},
	"grub2-mkconfig":     {"/usr/sbin/grub2-mkconfig"},
	"gzip":               {"/usr/bin/gzip"},
	"head":               {"/usr/bin/head"},
	"ln":                 {"/usr/bin/ln"},
	"ls":                 {"/bin/ls", "/usr/bin/ls"},
	"lsof":               {"/usr/bin/lsof"},
	"lsb_release":        {"/usr/bin/lsb_release"},
	"lsblk":              {"/usr/bin/lsblk"},
	"losetup":            {"/usr/sbin/losetup"},
	"lvcreate":           {"/usr/sbin/lvcreate"},
	"mformat":            {"/usr/bin/mformat"},
	"mcopy":              {"/usr/bin/mcopy"},
	"mmdebstrap":         {"/usr/bin/mmdebstrap"},
	"mkdir":              {"/bin/mkdir"},
	"mkfs":               {"/usr/sbin/mkfs"},
	"mkswap":             {"/usr/sbin/mkswap"},
	"mktemp":             {"/usr/bin/mktemp"},
	"mount":              {"/usr/bin/mount"},
	"opkg":               {"/usr/bin/opkg"},
	"parted":             {"/usr/sbin/parted"},
	"partx":              {"/usr/bin/partx", "/sbin/partx"},
	"pvcreate":           {"/usr/sbin/pvcreate"},
	"qemu-img":           {"/usr/bin/qemu-img"},
	"qemu-system-x86_64": {"/usr/bin/qemu-system-x86_64"},
	"rm":                 {"/bin/rm"},
	"rpm":                {"/usr/bin/rpm"},
	"run":                {"/usr/bin/run"},
	"sed":                {"/usr/bin/sed", "/bin/sed"},
	"sfdisk":             {"/usr/sbin/sfdisk"},
	"sgdisk":             {"/usr/bin/sgdisk"},
	"sha256sum":          {"/usr/bin/sha256sum"},
	"sh":                 {"/bin/sh"},
	"sleep":              {"/usr/bin/sleep"},
	"snap":               {"/usr/bin/snap"},
	"sudo":               {"/usr/bin/sudo"},
	"swapon":             {"/usr/sbin/swapon"},
	"swapoff":            {"/usr/sbin/swapoff"},
	"sync":               {"/usr/bin/sync"},
	"tail":               {"/usr/bin/tail"},
	"tar":                {"/usr/bin/tar"},
	"tdnf":               {"/usr/bin/tdnf"},
	"tee":                {"/usr/bin/tee"},
	"touch":              {"/usr/bin/touch"},
	"truncate":           {"/usr/bin/truncate"},
	"tune2fs":            {"/usr/sbin/tune2fs"},
	"ukify":              {"/usr/bin/ukify", "/usr/local/bin/ukify"},
	"umount":             {"/usr/bin/umount"},
	"uname":              {"/usr/bin/uname"},
	"uniq":               {"/usr/bin/uniq"},
	"veritysetup":        {"/usr/sbin/veritysetup"},
	"vgcreate":           {"/usr/sbin/vgcreate"},
	"wget":               {"/usr/bin/wget"},
	"wipefs":             {"/usr/sbin/wipefs"},
	"xorriso":            {"/usr/bin/xorriso"},
	"xz":                 {"/usr/bin/xz"},
	"yum":                {"/usr/bin/yum"},
	"zstd":               {"/usr/bin/zstd"},
	"dracut":             {"/usr/bin/dracut"},
	"useradd":            {"/usr/sbin/useradd"},
	"usermod":            {"/usr/sbin/usermod"},
	"groups":             {"/usr/bin/groups"},
	"passwd":             {"/usr/bin/passwd"},
	"mv":                 {"/bin/mv"},
	"grub-mkimage":       {"/usr/bin/grub-mkimage"},
	"grub-install":       {"/usr/sbin/grub-install"},
	"sbsign":             {"/usr/bin/sbsign"},
	"systemctl":          {"/usr/bin/systemctl"},
	"test":               {"/bin/test"},
	"awk":                {"/usr/bin/awk"},
	"update-initramfs":   {"/usr/sbin/update-initramfs", "/usr/bin/update-initramfs"},
	"update-grub":        {"/usr/sbin/update-grub", "/usr/bin/update-grub"},
	// Add more mappings as needed
}

type Executor interface {
	ExecCmd(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error)
	ExecCmdSilent(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error)
	ExecCmdWithStream(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error)
	ExecCmdWithInput(inputStr string, cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error)
}

type DefaultExecutor struct{}

var Default Executor = &DefaultExecutor{}

// GetOSEnvirons returns the system environment variables
func GetOSEnvirons() map[string]string {
	// Convert os.Environ() to a map
	environ := make(map[string]string)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			environ[parts[0]] = parts[1]
		}
	}
	return environ
}

// GetOSProxyEnvirons retrieves HTTP and HTTPS proxy environment variables
func GetOSProxyEnvirons() map[string]string {
	osEnv := GetOSEnvirons()
	proxyEnv := make(map[string]string)

	// Extract http_proxy and https_proxy variables
	for key, value := range osEnv {
		if strings.Contains(strings.ToLower(key), "http_proxy") ||
			strings.Contains(strings.ToLower(key), "https_proxy") {
			proxyEnv[key] = value
		}
	}

	return proxyEnv
}

// IsBashAvailable checks if bash is available in the given chroot environment
func IsBashAvailable(chrootPath string) bool {
	if bashPathList, ok := commandMap["bash"]; ok {
		for _, bashPath := range bashPathList {
			if _, err := os.Stat(filepath.Join(chrootPath, bashPath)); err == nil {
				return true
			}
		}
		log.Debugf("bash not found in chroot path %s", chrootPath)
	} else {
		log.Debugf("bash path not found in commandMap")
	}
	return false
}

// IsCommandExist checks if a command exists in the system or in a chroot environment
func IsCommandExist(cmd string, chrootPath string) (bool, error) {
	var cmdStr string
	if chrootPath == HostPath {
		cmdStr = "command -v " + cmd
	} else {
		cmdStr = "bash -c 'command -v " + cmd + "'"
	}

	output, err := ExecCmdSilent(cmdStr, false, chrootPath, nil)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		trimmed := strings.TrimSpace(output)
		if len(trimmed) == 0 {
			return false, nil
		}
		return false, fmt.Errorf("failed to execute command %s: output %s, err %w", cmdStr, trimmed, err)
	}

	return strings.TrimSpace(output) != "", nil
}

func extractSedPattern(command string) (string, error) {
	// This regex handles common sed patterns:
	// - sed -i 's/pattern/replacement/'
	// - sed -i 's/pattern/replacement/g'
	// - sed -i '/pattern/d'
	// - sed -i '/pattern/c\replacement'
	// - sed -i '1,10 d'
	// - sed -i '10 i\text to insert'
	// - sed -E -i 's|pattern|replacement|g' file
	// - sed -E -i "s|pattern|replacement|g" file

	// First try single quotes
	singleRe := regexp.MustCompile(`(?s)sed\s+(?:-[^\s'"]*\s*)*'(.*?)'`)
	matches := singleRe.FindStringSubmatch(command)

	if len(matches) >= 2 {
		return matches[1], nil
	}

	// Then try double quotes
	doubleRe := regexp.MustCompile(`(?s)sed\s+(?:-[^\s'"]*\s*)*"(.*?)"`)
	matches = doubleRe.FindStringSubmatch(command)
	if len(matches) >= 2 {
		return matches[1], nil
	}
	return "", fmt.Errorf("no quoted string found in sed command")
}

func extractEchoString(command string) (string, error) {
	// First try single quotes
	singleRe := regexp.MustCompile(`(?s)echo\s+(?:-e\s+)?'(.*?)'`)
	matches := singleRe.FindStringSubmatch(command)

	if len(matches) >= 2 {
		return matches[1], nil
	}

	// Then try double quotes
	doubleRe := regexp.MustCompile(`echo\s+(?:-e\s+)?"(.*?)"`)
	matches = doubleRe.FindStringSubmatch(command)
	if len(matches) >= 2 {
		return matches[1], nil
	}

	// Check with need manual parsing
	// This handles cases like: echo 'hello "world" good morning'
	return extractEchoStringManual(command)
}

// extractEchoStringManual uses manual parsing to ensure opening and closing quotes match
// This handles cases like: echo 'hello "world" good morning'
func extractEchoStringManual(command string) (string, error) {
	// Find the echo command and optional -e flag
	echoRe := regexp.MustCompile(`echo\s+(?:-e\s+)?`)
	loc := echoRe.FindStringIndex(command)
	if loc == nil {
		return "", fmt.Errorf("no echo command found")
	}

	// Get the rest of the string after 'echo' and optional '-e'
	rest := command[loc[1]:]
	rest = strings.TrimSpace(rest)

	if len(rest) == 0 {
		return "", fmt.Errorf("no quoted string found in echo command")
	}

	// Check if it starts with a quote
	if rest[0] != '\'' && rest[0] != '"' {
		return "", fmt.Errorf("echo string must start with a quote")
	}

	quoteChar := rest[0]

	// Find the matching closing quote (same type as opening)
	escaped := false
	for i := 1; i < len(rest); i++ {
		if escaped {
			escaped = false
			continue
		}
		if rest[i] == '\\' {
			escaped = true
			continue
		}
		if rest[i] == quoteChar {
			// Found the matching closing quote - return WITH quotes
			return rest[0 : i+1], nil
		}
	}

	return "", fmt.Errorf("no matching closing quote found")
}

// findSeparatorOutsideQuotes finds the index of a separator that is not within quotes
func findSeparatorOutsideQuotes(cmd string, sep string) int {
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false

	for i := 0; i < len(cmd); i++ {
		if escaped {
			escaped = false
			continue
		}

		if cmd[i] == '\\' {
			escaped = true
			continue
		}

		// Toggle quote states
		if cmd[i] == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
		} else if cmd[i] == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
		}

		// Check if we found the separator outside quotes
		if !inSingleQuote && !inDoubleQuote {
			if i+len(sep) <= len(cmd) && cmd[i:i+len(sep)] == sep {
				return i
			}
		}
	}

	return -1
}

func verifyCmdWithFullPath(cmd, chrootPath string) (string, error) {
	var ignoreStr string
	var err error
	separators := []string{"&&", ";", "|", "||"}

	// If the command is 'sed' or 'echo', we need to ignore the string content
	if strings.HasPrefix(cmd, "sed ") {
		ignoreStr, err = extractSedPattern(cmd)
		if err != nil {
			return "", fmt.Errorf("failed to extract sed pattern: %w", err)
		}
	} else if strings.HasPrefix(cmd, "echo ") {
		ignoreStr, err = extractEchoString(cmd)
		if err != nil {
			return "", fmt.Errorf("failed to extract echo string: %w", err)
		}
	}

	if ignoreStr != "" {
		// Remove the ignore string from the command
		cmd = strings.ReplaceAll(cmd, ignoreStr, "<ignored>")
	}

	sepIdx := -1
	sep := ""
	for _, s := range separators {
		if idx := findSeparatorOutsideQuotes(cmd, s); idx != -1 && (sepIdx == -1 || idx < sepIdx) {
			sepIdx = idx
			sep = s
		}
	}
	if sepIdx != -1 {
		left := strings.TrimSpace(cmd[:sepIdx])
		right := strings.TrimSpace(cmd[sepIdx+len(sep):])
		leftCmdStr, err := verifyCmdWithFullPath(left, chrootPath)
		if err != nil {
			return "", fmt.Errorf("failed to verify command: %w", err)
		}
		rightCmdStr, err := verifyCmdWithFullPath(right, chrootPath)
		if err != nil {
			return "", fmt.Errorf("failed to verify command: %w", err)
		}
		updatedCmdStr := leftCmdStr + " " + sep + " " + rightCmdStr
		if ignoreStr != "" && ignoreStr != "<ignored>" {
			updatedCmdStr = strings.ReplaceAll(updatedCmdStr, "<ignored>", ignoreStr)
		}
		return updatedCmdStr, nil
	}

	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return cmd, nil
	}
	bin := fields[0]
	fullPathList, ok := commandMap[bin]
	if ok {
		var foundPath bool = false
		for _, fullPath := range fullPathList {
			if fullPath == bin {
				// This handles shell built-in commands like 'cd' and 'command'
				fields[0] = bin
				foundPath = true
				break
			}
			fullHostPath := filepath.Join(chrootPath, fullPath)
			if _, err := os.Stat(fullHostPath); err == nil {
				fields[0] = fullPath
				foundPath = true
				break
			}
		}
		if !foundPath {
			return "", fmt.Errorf("command %s full path recorded in [%s] not exist in %s",
				bin, strings.Join(fullPathList, ", "), chrootPath)
		}
	} else {
		return "", fmt.Errorf("command %s not found in commandMap", bin)
	}

	updatedCmdStr := strings.Join(fields, " ")
	if ignoreStr != "" && ignoreStr != "<ignored>" {
		updatedCmdStr = strings.ReplaceAll(updatedCmdStr, "<ignored>", ignoreStr)
	}
	return updatedCmdStr, nil
}

// GetFullCmdStr prepares a command string with necessary prefixes
func GetFullCmdStr(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	var fullCmdStr string
	envValStr := ""
	for _, env := range envVal {
		envValStr += env + " "
	}

	fullPathCmdStr, err := verifyCmdWithFullPath(cmdStr, chrootPath)
	if err != nil {
		return fullPathCmdStr, fmt.Errorf("failed to verify command with full path: %w", err)
	}

	if chrootPath != HostPath {
		if _, err := os.Stat(chrootPath); os.IsNotExist(err) {
			return fullPathCmdStr, fmt.Errorf("chroot path %s does not exist", chrootPath)
		}

		proxyEnv := GetOSProxyEnvirons()

		for key, value := range proxyEnv {
			envValStr += key + "=" + value + " "
		}

		fullCmdStr = "sudo " + envValStr + "chroot " + chrootPath + " " + fullPathCmdStr
		chrootDir := filepath.Base(chrootPath)
		// log.Debugf("Chroot " + chrootDir + " Exec: [" + fullPathCmdStr + "]")
		// Avoid logging full command string to prevent leaking sensitive data.
		log.Debugf("Chroot %s Exec: [command executed]", chrootDir)

	} else {
		if sudo {
			proxyEnv := GetOSProxyEnvirons()

			for key, value := range proxyEnv {
				envValStr += key + "=" + value + " "
			}

			fullCmdStr = "sudo " + envValStr + fullPathCmdStr
			// log.Debugf("Exec: [sudo " + fullPathCmdStr + "]")
			// Avoid logging full command string to prevent leaking sensitive data.
			log.Debugf("Exec with sudo: [command executed]")
		} else {
			fullCmdStr = fullPathCmdStr
			// log.Debugf("Exec: [" + fullPathCmdStr + "]")
			// Avoid logging full command string to prevent leaking sensitive data.
			log.Debugf("Exec without sudo: [command executed]")
		}
	}

	return fullCmdStr, nil
}

// ExecCmd executes a command and returns its output
func (d *DefaultExecutor) ExecCmd(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fullCmdStr, err := GetFullCmdStr(cmdStr, sudo, chrootPath, envVal)
	if err != nil {
		return "", fmt.Errorf("failed to get full command string: %w", err)
	}

	cmd := exec.Command("bash", "-c", fullCmdStr)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		if outputStr != "" {
			// return outputStr, fmt.Errorf("failed to exec %s: output %s, err %w", fullCmdStr, outputStr, err)
			// Do not include the full command string in the error to avoid leaking sensitive data.
			return outputStr, fmt.Errorf("failed to execute command with output: %w", err)
		} else {
			// return outputStr, fmt.Errorf("failed to exec %s: %w", fullCmdStr, err)
			// Do not include the full command string in the error to avoid leaking sensitive data.
			return outputStr, fmt.Errorf("failed to execute command: %w", err)
		}
	} else {
		if outputStr != "" {
			// log.Debugf(outputStr)
			// Avoid logging raw command output to prevent leaking sensitive data.
			log.Debugf("Command executed successfully with non-empty output")
		}
		return outputStr, nil
	}
}

// ExecCmdSilent executes a command without logging its output
func (d *DefaultExecutor) ExecCmdSilent(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fullCmdStr, err := GetFullCmdStr(cmdStr, sudo, chrootPath, envVal)
	if err != nil {
		return "", fmt.Errorf("failed to get full command string: %w", err)
	}

	cmd := exec.Command("bash", "-c", fullCmdStr)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// ExecCmdWithStream executes a command and streams its output
func (d *DefaultExecutor) ExecCmdWithStream(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fullCmdStr, err := GetFullCmdStr(cmdStr, sudo, chrootPath, envVal)
	if err != nil {
		return "", fmt.Errorf("failed to get full command string: %w", err)
	}
	cmd := exec.Command("bash", "-c", fullCmdStr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdout pipe for command %s: %w", fullCmdStr, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stderr pipe for command %s: %w", fullCmdStr, err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command %s: %w", fullCmdStr, err)
	}

	// Use channels to collect stdout and stderr in order to preserve full command output.
	outputChan := make(chan string, 128)
	var outputStr strings.Builder
	var collectWG sync.WaitGroup
	collectWG.Add(1)
	go func() {
		defer collectWG.Done()
		for output := range outputChan {
			outputStr.WriteString(output)
			outputStr.WriteString("\n") // Add newlines between lines
		}
	}()

	var streamWG sync.WaitGroup
	streamWG.Add(2)

	go func() {
		defer streamWG.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			str := scanner.Text()
			if str != "" {
				outputChan <- str
				log.Debugf(str)
			}
		}
	}()

	go func() {
		defer streamWG.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			str := scanner.Text()
			if str != "" {
				outputChan <- str
				log.Debugf(str)
			}
		}
	}()

	streamWG.Wait()
	close(outputChan)
	collectWG.Wait()

	if err := cmd.Wait(); err != nil {
		return outputStr.String(), fmt.Errorf("failed to wait for command %s: %w", fullCmdStr, err)
	}

	return outputStr.String(), nil
}

// ExecCmdWithInput executes a command with input string
func (d *DefaultExecutor) ExecCmdWithInput(inputStr string, cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fullCmdStr, err := GetFullCmdStr(cmdStr, sudo, chrootPath, envVal)
	if err != nil {
		return "", fmt.Errorf("failed to get full command string: %w", err)
	}

	cmd := exec.Command("bash", "-c", fullCmdStr)
	cmd.Stdin = strings.NewReader(inputStr)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		if outputStr != "" {
			log.Infof(outputStr)
		}
		// return outputStr, fmt.Errorf("failed to exec %s with input %s: %w", fullCmdStr, inputStr, err)
		return outputStr, fmt.Errorf("failed to exec command: %w", err)
	} else {
		if outputStr != "" {
			log.Debugf(outputStr)
		}
		return outputStr, nil
	}
}

// Convenience functions for backward compatibility
func ExecCmd(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return Default.ExecCmd(cmdStr, sudo, chrootPath, envVal)
}

func ExecCmdSilent(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return Default.ExecCmdSilent(cmdStr, sudo, chrootPath, envVal)
}

func ExecCmdWithStream(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return Default.ExecCmdWithStream(cmdStr, sudo, chrootPath, envVal)
}

func ExecCmdWithInput(inputStr string, cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	return Default.ExecCmdWithInput(inputStr, cmdStr, sudo, chrootPath, envVal)
}
