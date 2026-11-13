// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/oepinstaller"
)

// oepTemplate is a schema-valid template with a systemConfig.oepInstaller
// section. Nothing is downloaded: the build is a fake.
const oepTemplate = `image:
  name: oep-image
  version: "1.0"
target:
  os: ubuntu
  dist: ubuntu24
  arch: x86_64
  imageType: iso
systemConfig:
  name: oep
  users:
    - name: edge
      sshAuthorizedKeys:
        - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIENm2CSXV8QTuKy2yC7Mr8b8oy9S98WGVPszP5qfF32G test
  oepInstaller:
    url: https://raw.githubusercontent.com/example/oep-installer-cli/0123abc/rendered/openedge-cli
    sha256: ` + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + `
    sbomUrl: https://raw.githubusercontent.com/example/oep-installer-cli/0123abc/rendered/sbom.yaml
    sbomSha256: ` + "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210" + `
    modules:
      - handheld_blueprint
    user: edge
`

// recordingFakeICT writes its arguments to %[1]s and, when it is given a token
// file, copies the file's contents to %[2]s, so a test can check what the build
// received while the file still exists.
const recordingFakeICT = `#!/bin/sh
printf '%%s\n' "$@" > "%[1]s"
while [ $# -gt 0 ]; do
  if [ "$1" = "` + oepinstaller.TokenFileFlag + `" ]; then cat "$2" > "%[2]s"; fi
  shift
done
exit 0
`

// newOEPTokenTestService returns a service whose robotics.yml template uses the
// OEP Installer (retail.yml does not), building with a fake that records what it
// was given in the returned directory.
func newOEPTokenTestService(t *testing.T) (s *Service, record string) {
	t.Helper()
	s = newTestService(t)
	if err := os.WriteFile(filepath.Join(s.cfg.TemplatesDir, "robotics.yml"), []byte(oepTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	record = t.TempDir()
	bin := filepath.Join(t.TempDir(), "fake-ict")
	script := fmt.Sprintf(recordingFakeICT, filepath.Join(record, "args"), filepath.Join(record, "token"))
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s.cfg.ICTBinary = bin
	return s, record
}

var oepSelection = Selection{Vertical: "robotics", SKU: "amr", Platform: "wcl", OS: "ubuntu24", ImageType: "iso"}

// runToEnd starts req, waits for the build to finish, and frees its build slot.
func runToEnd(t *testing.T, s *Service, req BuildRequest) *build {
	t.Helper()
	acc, err := s.StartBuild(req)
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	b, ok := s.getBuild(acc.BuildID)
	if !ok {
		t.Fatalf("build %s not tracked", acc.BuildID)
	}
	// done closes before runBuild's deferred releaseBuildSlot runs, so a build
	// started straight after could still see the slot taken; free it here, as
	// retry_test.go does.
	<-b.done
	s.releaseBuildSlot(acc.BuildID)
	return b
}

func readOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func TestStartBuildPassesOEPTokenAsFile(t *testing.T) {
	s, record := newOEPTokenTestService(t)
	const token = "github_pat_test-token-value"
	sel := oepSelection
	b := runToEnd(t, s, BuildRequest{Compose: &sel, OEPInstallerToken: token})

	args := readOrEmpty(t, filepath.Join(record, "args"))
	if !strings.Contains(args, oepinstaller.TokenFileFlag+"\n") {
		t.Fatalf("build args lack %s:\n%s", oepinstaller.TokenFileFlag, args)
	}
	if got := readOrEmpty(t, filepath.Join(record, "token")); got != token {
		t.Errorf("token file held %q, want %q", got, token)
	}
	if strings.Contains(args, token) || strings.Contains(b.Command, token) {
		t.Error("the token is on the command line")
	}
	if !strings.HasPrefix(b.tokenFile, filepath.Join(s.cfg.WorkDir, oepTokenDirName)+string(os.PathSeparator)) {
		t.Errorf("token file %q is not in the token directory", b.tokenFile)
	}
	if _, err := os.Stat(b.tokenFile); !os.IsNotExist(err) {
		t.Errorf("token file still exists after the build: %v", err)
	}
	for _, name := range []string{"meta.json", "compose.log", "template.yml"} {
		if strings.Contains(readOrEmpty(t, filepath.Join(b.RootDir, name)), token) {
			t.Errorf("%s contains the token", name)
		}
	}

	// A retry resubmits the stored selection, which has no token.
	acc, err := s.RetryBuild(b.ID)
	if err != nil {
		t.Fatalf("RetryBuild: %v", err)
	}
	retried, _ := s.getBuild(acc.BuildID)
	<-retried.done
	if args := readOrEmpty(t, filepath.Join(record, "args")); strings.Contains(args, oepinstaller.TokenFileFlag) {
		t.Errorf("the retry got a token file:\n%s", args)
	}
}

func TestStartBuildIgnoresOEPTokenWithoutOEPInstaller(t *testing.T) {
	s, record := newOEPTokenTestService(t)
	sel := Selection{Vertical: "retail", SKU: "dv", Platform: "arl", OS: "debian13", ImageType: "iso"}
	b := runToEnd(t, s, BuildRequest{Compose: &sel, OEPInstallerToken: "unused"})
	if b.tokenFile != "" {
		t.Errorf("a template without oepInstaller got token file %q", b.tokenFile)
	}
	if args := readOrEmpty(t, filepath.Join(record, "args")); strings.Contains(args, oepinstaller.TokenFileFlag) {
		t.Errorf("build args carry %s:\n%s", oepinstaller.TokenFileFlag, args)
	}
}

func TestComposeReportsOEPInstaller(t *testing.T) {
	s, _ := newOEPTokenTestService(t)
	res, err := s.Compose(oepSelection)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if !res.OEPInstaller.Uses {
		t.Error("OEPInstaller.Uses = false for a template with oepInstaller")
	}
	res, err = s.Compose(Selection{Vertical: "retail", SKU: "dv", Platform: "arl", OS: "debian13", ImageType: "iso"})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if res.OEPInstaller.Uses {
		t.Error("OEPInstaller.Uses = true for a template without oepInstaller")
	}
}

// TestOEPInstallerServerToken is not parallel: it sets environment variables
// and the global configuration.
func TestOEPInstallerServerToken(t *testing.T) {
	uses := &config.ImageTemplate{}
	uses.SystemConfig.OEPInstaller.Modules = []string{"m"}
	tests := []struct {
		name, env, tokenFile string
		sudo, want           bool
	}{
		{"nothing configured", "", "", false, false},
		{"configured token file", "", "/etc/token", true, true},
		{"environment without sudo", "token", "", false, true},
		{"environment under sudo does not reach the build", "token", "", true, false},
	}
	orig := *config.Global()
	t.Cleanup(func() { config.SetGlobal(&orig) })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(oepinstaller.TokenEnv, tt.env)
			t.Setenv(oepinstaller.TokenFileEnv, "")
			cfg := orig
			cfg.OEPInstaller.TokenFile = tt.tokenFile
			config.SetGlobal(&cfg)
			s := &Service{cfg: Config{Sudo: tt.sudo}}
			if got := s.oepInstallerInfo(uses); !got.Uses || got.ServerToken != tt.want {
				t.Errorf("oepInstallerInfo = %+v, want Uses and ServerToken %v", got, tt.want)
			}
		})
	}
}

func TestRemoveStaleOEPTokens(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	s := &Service{cfg: Config{WorkDir: workDir}}
	own, err := s.writeOEPToken("left-behind")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(own)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %04o, want 0600", perm)
	}
	if got, want := filepath.Dir(own), filepath.Join(workDir, oepTokenDirName, strconv.Itoa(os.Getpid())); got != want {
		t.Errorf("token file in %s, want the server's own directory %s", got, want)
	}

	// A process that has exited: run one and wait for it, so its PID is free.
	exited := exec.Command("true")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	tokenIn := func(pid int) string {
		dir := filepath.Join(workDir, oepTokenDirName, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "token-1")
		if err := os.WriteFile(path, []byte("t"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	dead := tokenIn(exited.Process.Pid)
	live := tokenIn(os.Getppid()) // another running server's

	removeStaleOEPTokens(workDir)
	for name, path := range map[string]string{"this process's own": own, "an exited server's": dead} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s stale token file survived: %v", name, err)
		}
	}
	if _, err := os.Stat(live); err != nil {
		t.Errorf("a running server's token file was removed: %v", err)
	}
}

// A token longer than the OpenAPI maxLength is rejected before a build starts;
// one at the limit is accepted.
func TestStartBuildRejectsOversizedOEPToken(t *testing.T) {
	s, record := newOEPTokenTestService(t)
	sel := oepSelection
	_, err := s.StartBuild(BuildRequest{Compose: &sel, OEPInstallerToken: strings.Repeat("a", maxOEPTokenLen+1)})
	var se *Error
	if !errors.As(err, &se) || se.Status != http.StatusBadRequest {
		t.Fatalf("StartBuild with an oversized token: err = %v, want 400", err)
	}
	if args := readOrEmpty(t, filepath.Join(record, "args")); args != "" {
		t.Errorf("a build ran with an oversized token:\n%s", args)
	}
	token := strings.Repeat("a", maxOEPTokenLen)
	runToEnd(t, s, BuildRequest{Compose: &sel, OEPInstallerToken: token})
	if got := readOrEmpty(t, filepath.Join(record, "token")); got != token {
		t.Errorf("token at the limit: build got %d characters, want %d", len(got), len(token))
	}
}
