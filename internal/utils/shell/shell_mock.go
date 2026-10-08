package shell

import (
	"fmt"
	"path/filepath"
	"regexp"
)

type MockCommand struct {
	Pattern string
	Output  string
	Error   error
}

type MockExecutor struct {
	// mockExpectedOutput contains the mapping of command strings to their expected outputs and errors
	mockExpectedOutput []MockCommand
}

func NewMockExecutor(mockExpectedOutput []MockCommand) *MockExecutor {
	return &MockExecutor{
		mockExpectedOutput: mockExpectedOutput}
}

func getFullCmdStr(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	var fullCmdStr string
	envValStr := ""
	for _, env := range envVal {
		envValStr += env + " "
	}
	if chrootPath != HostPath {
		fullCmdStr = "sudo " + envValStr + "chroot " + chrootPath + " " + cmdStr
		chrootDir := filepath.Base(chrootPath)
		// log.Debugf("MockExecutor: Chroot " + chrootDir + " Exec: [" + cmdStr + "]")
		// Avoid logging full command string to prevent leaking sensitive data such as passwords.
		log.Debugf("MockExecutor: Chroot %s Exec", chrootDir)
	} else {
		if sudo {
			fullCmdStr = "sudo " + envValStr + cmdStr
			// log.Debugf("MockExecutor: Host Exec with sudo: [" + cmdStr + "]")
			// Avoid logging full command string to prevent leaking sensitive data such as passwords.
			log.Debugf("MockExecutor: Host Exec with sudo")
		} else {
			fullCmdStr = envValStr + cmdStr
			// log.Debugf("MockExecutor: Host Exec: [" + cmdStr + "]")
			// Avoid logging full command string to prevent leaking sensitive data such as passwords.
			log.Debugf("MockExecutor: Host Exec without sudo")
		}
	}
	return fullCmdStr, nil
}

func (m *MockExecutor) execCmdOverride(cmdStr string, sudo bool, chrootPath string, envVal []string) (bool, string, error) {
	var fallback2Default = true
	fullCmdStr, err := getFullCmdStr(cmdStr, sudo, chrootPath, envVal)
	if err != nil {
		return fallback2Default, "", fmt.Errorf("failed to get full command string: %w", err)
	}
	for _, mockCmd := range m.mockExpectedOutput {
		matched, err := regexp.MatchString(mockCmd.Pattern, fullCmdStr)
		if err != nil {
			log.Errorf("Invalid regex pattern %s: %v", mockCmd.Pattern, err)
			continue
		}
		if matched {
			fallback2Default = false
			if mockCmd.Error != nil {
				return fallback2Default, mockCmd.Output, mockCmd.Error
			} else {
				return fallback2Default, mockCmd.Output, nil
			}
		}
	}
	return fallback2Default, "", nil
}

func (m *MockExecutor) ExecCmd(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fallback2Default, output, err := m.execCmdOverride(cmdStr, sudo, chrootPath, envVal)
	if fallback2Default {
		return (&DefaultExecutor{}).ExecCmd(cmdStr, sudo, chrootPath, envVal)
	} else {
		log.Debugf("MockExecutor: output: %s", output)
		return output, err
	}
}

func (m *MockExecutor) ExecCmdSilent(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fallback2Default, output, err := m.execCmdOverride(cmdStr, sudo, chrootPath, envVal)
	if fallback2Default {
		return (&DefaultExecutor{}).ExecCmdSilent(cmdStr, sudo, chrootPath, envVal)
	} else {
		return output, err
	}
}

func (m *MockExecutor) ExecCmdWithStream(cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fallback2Default, output, err := m.execCmdOverride(cmdStr, sudo, chrootPath, envVal)
	if fallback2Default {
		return (&DefaultExecutor{}).ExecCmdWithStream(cmdStr, sudo, chrootPath, envVal)
	} else {
		return output, err
	}
}

func (m *MockExecutor) ExecCmdWithInput(inputStr string, cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fallback2Default, output, err := m.execCmdOverride(cmdStr, sudo, chrootPath, envVal)
	if fallback2Default {
		return (&DefaultExecutor{}).ExecCmdWithInput(inputStr, cmdStr, sudo, chrootPath, envVal)
	} else {
		return output, err
	}
}

func (m *MockExecutor) ExecCmdSilentWithInput(inputStr string, cmdStr string, sudo bool, chrootPath string, envVal []string) (string, error) {
	fallback2Default, output, err := m.execCmdOverride(cmdStr, sudo, chrootPath, envVal)
	if fallback2Default {
		return (&DefaultExecutor{}).ExecCmdSilentWithInput(inputStr, cmdStr, sudo, chrootPath, envVal)
	} else {
		return output, err
	}
}
