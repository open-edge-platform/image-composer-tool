package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/spf13/cobra"
)

// cliTestKey is a public key (no private half exists) used as a valid key line.
const cliTestKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGgL6aAgYAGnMFok5OwHz7Q3u1QmAEkYOi+X1am2kd8g"

// resetCompositionFlags clears the package-level flag variables after a test.
func resetCompositionFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		diskStrategy, hostname, httpProxy, httpsProxy, ftpProxy, noProxy = "", "", "", "", "", ""
		sshKeyFlags = nil
	})
}

// setProxyFlag sets a proxy flag the way the command line does, so the flag
// counts as changed even when the value is empty.
func setProxyFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("setting --%s: %v", name, err)
	}
}

func TestApplyCompositionFlagOverrides(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "k.pub")
	if err := os.WriteFile(keyFile, []byte(cliTestKey+" cli\n"), 0644); err != nil {
		t.Fatal(err)
	}
	resetCompositionFlags(t)
	cmd := createBuildCommand()
	diskStrategy, hostname = "first", "edge-7"
	setProxyFlag(t, cmd, "http-proxy", "http://proxy:3128")
	sshKeyFlags = []string{"admin=" + keyFile}

	tmpl := &config.ImageTemplate{SystemConfig: config.SystemConfig{Users: []config.UserConfig{{Name: "admin"}}}}
	if err := applyCompositionFlagOverrides(cmd, tmpl); err != nil {
		t.Fatalf("applyCompositionFlagOverrides: %v", err)
	}
	if tmpl.Disk.SelectionPolicy.Strategy != "first" ||
		tmpl.SystemConfig.HostName != "edge-7" || tmpl.SystemConfig.Proxy.HTTPProxy != "http://proxy:3128" {
		t.Errorf("overrides not applied: %+v", tmpl)
	}
	if keys := tmpl.SystemConfig.Users[0].SSHAuthorizedKeys; len(keys) != 1 {
		t.Errorf("SSH key not added: %v", keys)
	}
}

func TestApplyCompositionFlagOverridesNoFlagsIsNoOp(t *testing.T) {
	resetCompositionFlags(t)
	cmd := createBuildCommand()
	tmpl := &config.ImageTemplate{Disk: config.DiskConfig{Path: "/dev/keep"}}
	if err := applyCompositionFlagOverrides(cmd, tmpl); err != nil {
		t.Fatalf("applyCompositionFlagOverrides: %v", err)
	}
	if tmpl.Disk.Path != "/dev/keep" {
		t.Errorf("template changed without flags: %+v", tmpl.Disk)
	}
}

func TestApplyCompositionFlagOverridesErrors(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		keys     []string
		want     string
	}{
		{"malformed key flag", "", []string{"admin"}, "USER=FILE"},
		{"empty file", "", []string{"admin="}, "USER=FILE"},
		{"bad strategy", "biggest", nil, "strategy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCompositionFlags(t)
			cmd := createBuildCommand()
			diskStrategy, sshKeyFlags = tt.strategy, tt.keys
			err := applyCompositionFlagOverrides(cmd, &config.ImageTemplate{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestApplyCompositionFlagOverridesEmptyProxyClearsTemplateValue(t *testing.T) {
	resetCompositionFlags(t)
	cmd := createBuildCommand()
	setProxyFlag(t, cmd, "no-proxy", "")
	tmpl := &config.ImageTemplate{SystemConfig: config.SystemConfig{Proxy: config.ProxyConfig{
		HTTPProxy: "http://proxy:3128", NoProxy: "localhost",
	}}}
	if err := applyCompositionFlagOverrides(cmd, tmpl); err != nil {
		t.Fatalf("applyCompositionFlagOverrides: %v", err)
	}
	if got := tmpl.SystemConfig.Proxy; got.NoProxy != "" || got.HTTPProxy != "http://proxy:3128" {
		t.Errorf("--no-proxy '' must clear only noProxy, got %+v", got)
	}
}
