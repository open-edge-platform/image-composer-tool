// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/oepinstaller"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// oepTokenDirName is the directory under WorkDir that holds the GitHub tokens
// entered for running builds, in one subdirectory per server process named by
// its PID. It is outside builds/, so a token never sits next to a build's
// history, logs, or archived template.
const oepTokenDirName = ".oep-installer-tokens"

// maxOEPTokenLen is a request-size guard, matching the OpenAPI maxLength of
// BuildRequest.oepInstallerToken. GitHub tokens are far shorter.
const maxOEPTokenLen = 1024

// OEPInstallerInfo tells a caller whether a resolved template downloads
// OEP Installer files, and so may need a GitHub token, and whether the server
// already gives builds one.
type OEPInstallerInfo struct {
	Uses        bool
	ServerToken bool
}

// oepInstallerInfo reports t's OEP Installer use and whether a build started now
// would get a token without one entered for it. Under sudo the token
// environment variables do not reach the build, but the configuration file
// (passed with --config) does.
func (s *Service) oepInstallerInfo(t *config.ImageTemplate) OEPInstallerInfo {
	info := OEPInstallerInfo{Uses: !t.SystemConfig.OEPInstaller.IsEmpty()}
	if !info.Uses {
		return info
	}
	info.ServerToken = config.Global().OEPInstaller.TokenFile != "" ||
		(!s.cfg.Sudo && (os.Getenv(oepinstaller.TokenEnv) != "" || os.Getenv(oepinstaller.TokenFileEnv) != ""))
	return info
}

// writeOEPToken writes token to a new file only the server user can read, for
// one build to read with --oep-installer-token-file (root reads it under sudo),
// and returns its path. The caller removes the file once the build exits.
func (s *Service) writeOEPToken(token string) (path string, err error) {
	dir := filepath.Join(s.cfg.WorkDir, oepTokenDirName, strconv.Itoa(os.Getpid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating the token directory: %w", err)
	}
	f, err := os.CreateTemp(dir, "token-*") // mode 0600
	if err != nil {
		return "", fmt.Errorf("creating the token file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("writing the token file: %w", cerr)
		}
		if err != nil {
			removeOEPToken(f.Name())
		}
	}()
	if _, err := f.WriteString(token); err != nil {
		return "", fmt.Errorf("writing the token file: %w", err)
	}
	return f.Name(), nil
}

// removeOEPToken deletes a token file written by writeOEPToken.
func removeOEPToken(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.Logger().Warnf("removing the OEP Installer token file %s: %v", path, err)
	}
}

// removeStaleOEPTokens deletes the token files of server processes that are no
// longer running, which stopped mid-build. Another server sharing the work
// directory keeps its files, so its running builds can still read them, even
// when this server goes on to fail to start. A directory named by this
// process's own PID is stale too: this server has written nothing yet, and in
// a container a restarted server often gets the PID of the previous one.
func removeStaleOEPTokens(workDir string) {
	dir := filepath.Join(workDir, oepTokenDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Logger().Warnf("reading the OEP Installer token directory %s: %v", dir, err)
		}
		return
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() || (pid != os.Getpid() && processRunning(pid)) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			logger.Logger().Warnf("removing stale OEP Installer token files in %s: %v", dir, err)
		}
	}
}

// processRunning reports whether a process with this PID exists. EPERM means
// it does, owned by another user.
func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
