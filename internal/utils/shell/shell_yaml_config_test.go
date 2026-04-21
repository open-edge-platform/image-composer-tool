package shell_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// TestEchoWithNestedQuotes specifically tests the fix for echo commands with nested quotes
func TestEchoWithNestedQuotes(t *testing.T) {
	tests := []struct {
		name        string
		cmd         string
		wantErr     bool
		description string
	}{
		{
			name:        "single quotes with double quotes inside",
			cmd:         `echo 'Acquire::ftp::Proxy "http://proxy-dmz.intel.com:911";' > /etc/apt/apt.conf.d/99proxy.conf`,
			wantErr:     false,
			description: "This was the failing case - single quotes containing double quotes",
		},
		{
			name:        "single quotes with multiple double quotes",
			cmd:         `echo 'key1="value1" key2="value2"' > /etc/config`,
			wantErr:     false,
			description: "Multiple key-value pairs with double quotes inside single quotes",
		},
		{
			name:        "double quotes with escaped double quotes inside",
			cmd:         `echo "She said \"hello\" to me" > /etc/greeting`,
			wantErr:     false,
			description: "Double quotes with escaped double quotes inside",
		},
		{
			name:        "single quotes with special characters and double quotes",
			cmd:         `echo 'Pattern: "*.txt" Count: 42' > /etc/pattern.conf`,
			wantErr:     false,
			description: "Mix of special characters and double quotes in single quotes",
		},
		{
			name:        "empty string in single quotes",
			cmd:         `echo '' > /etc/empty`,
			wantErr:     false,
			description: "Empty string should work",
		},
		{
			name:        "empty string in double quotes",
			cmd:         `echo "" > /etc/empty`,
			wantErr:     false,
			description: "Empty double quoted string should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullCmd, err := shell.GetFullCmdStr(tt.cmd, false, shell.HostPath, nil)

			if tt.wantErr && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}

			if !tt.wantErr && err != nil {
				t.Errorf("%s: unexpected error = %v\nCommand: %s", tt.description, err, tt.cmd)
			}

			if err == nil && !strings.Contains(fullCmd, "echo") {
				t.Errorf("%s: processed command doesn't contain 'echo': %s", tt.description, fullCmd)
			}
		})
	}
}

// TestChrootCommandVerification simulates the EXACT flow used in imageos.go
// where commands are wrapped with chroot and quoted with strconv.Quote
func TestChrootCommandVerification(t *testing.T) {
	tests := []struct {
		name        string
		cmd         string
		installRoot string
		wantErr     bool
	}{
		{
			name:        "simple echo in chroot",
			cmd:         `echo 'yockgn01 dlstreamer x86_64 ubuntu24 image' > /etc/yockgn01.txt`,
			installRoot: "/data/image-composer-tool/workspace/ubuntu-ubuntu24-x86_64/chrootenv/workspace/imagebuild/minimal",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the EXACT process from imageos.go:823
			chrootCmd := fmt.Sprintf("chroot %s /bin/bash -c %s", tt.installRoot, strconv.Quote(tt.cmd))

			// This is what gets called in shell.ExecCmd -> GetFullCmdStr
			_, err := shell.GetFullCmdStr(chrootCmd, true, shell.HostPath, nil)

			if tt.wantErr && err == nil {
				t.Errorf("expected error but got none")
				t.Logf("Command: %s", tt.cmd)
				t.Logf("ChrootCmd: %s", chrootCmd)
			}

			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error = %v", err)
				t.Logf("Command: %s", tt.cmd)
				t.Logf("ChrootCmd: %s", chrootCmd)
			}
		})
	}
}
