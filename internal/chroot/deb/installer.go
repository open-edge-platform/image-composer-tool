package deb

import (
	"fmt"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var log = logger.Logger()

type DebInstallerInterface interface {
	UpdateLocalDebRepo(cacheDir, arch string, sudo bool) error
	InstallDebPkg(configDir, chrootPath, cacheDir string, packages []string) error
}

type DebInstaller struct {
	targetArch string
}

func NewDebInstaller() *DebInstaller {
	return &DebInstaller{}
}

func normalizeDebArch(targetArch string) (string, error) {
	switch targetArch {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", targetArch)
	}
}

func normalizeRuntimeArch(goArch string) (string, error) {
	switch goArch {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported host architecture: %s", goArch)
	}
}

func (debInstaller *DebInstaller) validateCrossArchDeps(targetArch string) error {
	hostArch, err := normalizeRuntimeArch(runtime.GOARCH)
	if err != nil {
		return err
	}

	if hostArch == targetArch {
		return nil
	}

	type crossArchDep struct {
		name string   // human-readable dependency name
		cmds []string // commands to check (any match passes)
		pkg  string   // apt package that provides it
	}

	qemuCmdByArch := map[string]string{
		"arm64": "qemu-aarch64-static",
		"amd64": "qemu-x86_64-static",
	}

	qemuCmd, ok := qemuCmdByArch[targetArch]
	if !ok {
		return fmt.Errorf("unsupported target architecture for cross-architecture dependency validation: %s", targetArch)
	}

	deps := []crossArchDep{
		{name: "arch-test", cmds: []string{"arch-test"}, pkg: "arch-test"},
		{name: qemuCmd, cmds: []string{qemuCmd}, pkg: "qemu-user-static"},
		{name: "binfmt-support", cmds: []string{"update-binfmts"}, pkg: "binfmt-support"},
	}

	for _, dep := range deps {
		hasAnyCommand := false
		for _, cmd := range dep.cmds {
			exists, err := shell.IsCommandExist(cmd, shell.HostPath)
			if err != nil {
				return fmt.Errorf("failed to check host dependency %q for cross-architecture build (host=%s target=%s): %w", cmd, hostArch, targetArch, err)
			}
			if exists {
				hasAnyCommand = true
				break
			}
		}

		if !hasAnyCommand {
			return fmt.Errorf("cross-architecture build requested (host=%s target=%s) but required host dependency %q is missing; install it with: sudo apt-get install -y %s", hostArch, targetArch, dep.name, dep.pkg)
		}
	}

	return nil
}

func (debInstaller *DebInstaller) cleanupOnSuccess(repoPath string, err *error) {
	if umountErr := mount.UmountPath(repoPath); umountErr != nil {
		log.Errorf("Failed to unmount debian local repository: %v", umountErr)
		*err = fmt.Errorf("failed to unmount debian local repository: %w", umountErr)
	}
}

func (debInstaller *DebInstaller) cleanupOnError(chrootEnvPath, repoPath string, err *error) {
	if umountErr := mount.UmountPath(repoPath); umountErr != nil {
		log.Errorf("Failed to unmount debian local repository: %v", umountErr)
		*err = fmt.Errorf("operation failed: %w, cleanup errors: %v", *err, umountErr)
		return
	}

	if _, RemoveErr := shell.ExecCmd("rm -rf "+chrootEnvPath, true, shell.HostPath, nil); RemoveErr != nil {
		log.Errorf("Failed to remove chroot environment build path: %v", RemoveErr)
		*err = fmt.Errorf("operation failed: %w, cleanup errors: %v", *err, RemoveErr)
	}
}

func (debInstaller *DebInstaller) UpdateLocalDebRepo(repoPath, targetArch string, sudo bool) error {
	if repoPath == "" {
		return fmt.Errorf("repository path cannot be empty")
	}

	normalizedArch, err := normalizeDebArch(targetArch)
	if err != nil {
		return err
	}
	targetArch = normalizedArch
	debInstaller.targetArch = normalizedArch

	metaDataPath := filepath.Join(repoPath,
		fmt.Sprintf("dists/stable/main/binary-%s", targetArch), "Packages.gz")
	if _, err := os.Stat(metaDataPath); err == nil {
		if _, err = shell.ExecCmd("rm -f "+metaDataPath, sudo, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to remove existing Packages.gz: %w", err)
		}
	}
	metaDataDir := filepath.Dir(metaDataPath)
	if _, err := os.Stat(metaDataDir); os.IsNotExist(err) {
		if _, err = shell.ExecCmd("mkdir -p "+metaDataDir, sudo, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to create metadata directory %s: %w", metaDataDir, err)
		}
	}

	// Escape any double quotes and dollar signs in the paths
	safeRepoPath := strings.ReplaceAll(repoPath, `"`, `\"`)
	safeRepoPath = strings.ReplaceAll(safeRepoPath, "$", `\$`)
	safeMetaDataPath := strings.ReplaceAll(metaDataPath, `"`, `\"`)
	safeMetaDataPath = strings.ReplaceAll(safeMetaDataPath, "$", `\$`)

	cmd := fmt.Sprintf("bash -c \"cd %s && dpkg-scanpackages . /dev/null | gzip -9c > %s\"", safeRepoPath, safeMetaDataPath)
	if _, err := shell.ExecCmd(cmd, sudo, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to create local debian cache repository: %w", err)
	}

	return nil
}

func (debInstaller *DebInstaller) InstallDebPkg(
	targetOsConfigDir, chrootEnvPath, chrootPkgCacheDir string, pkgsList []string,
) (err error) {
	return debInstaller.InstallDebPkgWithArch(
		targetOsConfigDir,
		chrootEnvPath,
		chrootPkgCacheDir,
		pkgsList,
		debInstaller.targetArch,
	)
}

func (debInstaller *DebInstaller) InstallDebPkgWithArch(
	targetOsConfigDir, chrootEnvPath, chrootPkgCacheDir string, pkgsList []string, targetArch string,
) (err error) {
	if chrootEnvPath == "" || chrootPkgCacheDir == "" || len(pkgsList) == 0 {
		return fmt.Errorf("invalid parameters: chrootEnvPath, chrootPkgCacheDir, and pkgsList cannot be empty")
	}
	if targetArch == "" {
		return fmt.Errorf("failed to install debian packages in chroot environment: target architecture is required")
	}

	debArch := targetArch

	// from local.list
	repoPath := "/cdrom/cache-repo"
	pkgListStr := strings.Join(pkgsList, ",")

	localRepoConfigPath := filepath.Join(targetOsConfigDir, "chrootenvconfigs", "local.list")
	if _, err := os.Stat(localRepoConfigPath); os.IsNotExist(err) {
		log.Errorf("Local repository config file does not exist: %s", localRepoConfigPath)
		return fmt.Errorf("local repository config file does not exist: %s", localRepoConfigPath)
	}
	suite := debutils.DetectDebSuiteFromSourcesList(localRepoConfigPath)

	if err := debInstaller.validateCrossArchDeps(debArch); err != nil {
		log.Errorf("Missing host dependencies for cross-architecture chroot build: %v", err)
		return err
	}

	if err := mount.MountPath(chrootPkgCacheDir, repoPath, "--bind"); err != nil {
		log.Errorf("Failed to mount debian local repository: %v", err)
		return fmt.Errorf("failed to mount debian local repository: %w", err)
	}

	defer func() {
		if err == nil {
			debInstaller.cleanupOnSuccess(repoPath, &err)
		} else {
			debInstaller.cleanupOnError(chrootEnvPath, repoPath, &err)
		}
	}()

	if _, err := os.Stat(chrootEnvPath); os.IsNotExist(err) {
		if err := os.MkdirAll(chrootEnvPath, 0700); err != nil {
			log.Errorf("Failed to create chroot environment directory: %v", err)
			return fmt.Errorf("failed to create chroot environment directory: %w", err)
		}
	}

	cmd := fmt.Sprintf("mmdebstrap "+
		"--variant=custom "+
		"--format=directory "+
		"--aptopt=APT::Authentication::Trusted=true "+
		"--aptopt=Dpkg::Options::=--force-confdef "+
		"--aptopt=Dpkg::Options::=--force-confold "+
		"--aptopt=APT::Get::Assume-Yes=true "+
		"--architectures=%s "+
		"--hook-dir=/usr/share/mmdebstrap/hooks/file-mirror-automount "+
		"--include=%s "+
		"--verbose --debug "+
		"-- %s %s %s",
		debArch, pkgListStr, suite, chrootEnvPath, localRepoConfigPath)

	// Set environment variables to ensure non-interactive installation.
	// PYTHONDONTWRITEBYTECODE skips py3compile during postinst scripts, which
	// is very slow under QEMU user-mode emulation in cross-arch builds. Python
	// will recompile bytecode on first execution on the target device.
	envVars := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBCONF_NOWARNINGS=yes",
		"PYTHONDONTWRITEBYTECODE=1",
	}

	if _, err = shell.ExecCmdWithStream(cmd, true, shell.HostPath, envVars); err != nil {
		log.Errorf("Failed to install debian packages in chroot environment: %v", err)
		return fmt.Errorf("failed to install debian packages in chroot environment: %w", err)
	}

	return nil
}
