package imageos

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageboot"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// buildDkmsModules builds, installs, and verifies the vendor DKMS kernel
// modules requested by systemConfig.dkms against the image's installed
// target kernel(s). It always builds with an explicit -k <kernel-version>;
// a package's own postinst/dkms.conf trigger would otherwise key off
// `uname -r`, which inside a chroot reports the build host's running
// kernel, not the target kernel just installed.
func buildDkmsModules(installRoot string, template *config.ImageTemplate) error {
	dkms := template.GetDkms()
	if !dkms.Enabled {
		return nil
	}

	kernelVersions, err := imageboot.ListInstalledKernelVersions(installRoot)
	if err != nil {
		return fmt.Errorf("failed to list installed kernel versions: %w", err)
	}
	if len(kernelVersions) == 0 {
		return fmt.Errorf("no installed kernel found under %s/boot for dkms build", installRoot)
	}

	for _, kernelVersion := range kernelVersions {
		if err := buildDkmsModulesForKernel(installRoot, kernelVersion, dkms); err != nil {
			return err
		}
	}
	return nil
}

func buildDkmsModulesForKernel(installRoot, kernelVersion string, dkms config.Dkms) error {
	log.Infof("Building DKMS modules for kernel %s", kernelVersion)

	if len(dkms.Modules) == 0 {
		cmd := fmt.Sprintf("dkms autoinstall -k %s", shell.QuoteArg(kernelVersion))
		if _, err := shell.ExecCmd(cmd, true, installRoot, nil); err != nil {
			return fmt.Errorf("dkms autoinstall for kernel %s: %w", kernelVersion, err)
		}
	} else {
		for _, module := range dkms.Modules {
			name, version, err := splitDkmsModule(module)
			if err != nil {
				return err
			}
			buildCmd := fmt.Sprintf("dkms build -k %s -m %s -v %s",
				shell.QuoteArg(kernelVersion), shell.QuoteArg(name), shell.QuoteArg(version))
			if _, err := shell.ExecCmd(buildCmd, true, installRoot, nil); err != nil {
				return fmt.Errorf("dkms build %s for kernel %s: %w", module, kernelVersion, err)
			}
			installCmd := fmt.Sprintf("dkms install -k %s -m %s -v %s",
				shell.QuoteArg(kernelVersion), shell.QuoteArg(name), shell.QuoteArg(version))
			if _, err := shell.ExecCmd(installCmd, true, installRoot, nil); err != nil {
				return fmt.Errorf("dkms install %s for kernel %s: %w", module, kernelVersion, err)
			}
		}
	}

	statusOutput, err := shell.ExecCmdWithStream(
		fmt.Sprintf("dkms status -k %s", shell.QuoteArg(kernelVersion)), true, installRoot, nil)
	if err != nil {
		return fmt.Errorf("dkms status for kernel %s: %w", kernelVersion, err)
	}
	if err := verifyDkmsModulesInstalled(statusOutput, dkms.Modules, kernelVersion); err != nil {
		return err
	}

	// verifyDkmsModulesInstalled only confirms each SOURCE reports installed;
	// this additionally confirms every module each built source declares
	// actually compiled, closing the gap where a source reports installed while
	// one of its modules silently failed to build (most relevant when
	// dkms.Modules is unset and the install check only requires "at least one").
	if err := verifyDkmsModulesBuilt(installRoot, kernelVersion); err != nil {
		return err
	}

	if dkms.SecureBoot.Enabled {
		if err := signDkmsModules(installRoot, kernelVersion, dkms); err != nil {
			return err
		}
	}

	return nil
}

func splitDkmsModule(module string) (name, version string, err error) {
	parts := strings.SplitN(module, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid dkms module %q: expected \"name/version\"", module)
	}
	return parts[0], parts[1], nil
}

// verifyDkmsModulesInstalled fails the build unless every module in expected
// (or, if expected is empty, at least one module) reports "installed" for
// kernelVersion in `dkms status` output.
func verifyDkmsModulesInstalled(statusOutput string, expected []string, kernelVersion string) error {
	installed := map[string]bool{}
	for _, line := range strings.Split(statusOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, kernelVersion) || !strings.Contains(line, ": installed") {
			continue
		}
		moduleField := strings.SplitN(line, ",", 2)[0]
		installed[strings.TrimSpace(moduleField)] = true
	}

	if len(expected) == 0 {
		if len(installed) == 0 {
			return fmt.Errorf("no dkms modules report installed for kernel %s", kernelVersion)
		}
		return nil
	}

	var missing []string
	for _, module := range expected {
		if !installed[module] {
			missing = append(missing, module)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("dkms module(s) %s did not report installed for kernel %s",
			strings.Join(missing, ", "), kernelVersion)
	}
	return nil
}

var builtModuleNameRe = regexp.MustCompile(`BUILT_MODULE_NAME(?:\[[0-9]+\])?="([^"]+)"`)

// verifyDkmsModulesBuilt fails the build if any module a DKMS source declares
// (BUILT_MODULE_NAME in its dkms.conf) did not actually compile for
// kernelVersion. verifyDkmsModulesInstalled only checks that each source
// reports "installed", but a source can report installed while one of its
// modules silently failed to build (dkms installs whatever built). This walks
// every source built for the kernel and confirms each declared module produced
// a .ko in the dkms build tree. A module that built but was not copied into
// /lib/modules (a dkms install choice, e.g. it also ships in-tree) still counts
// as built.
func verifyDkmsModulesBuilt(installRoot, kernelVersion string) error {
	// /var/lib/dkms/<source>/<version>/<kernelVersion> exists once a source has
	// been built for that kernel.
	kernelDirs, err := filepath.Glob(
		filepath.Join(installRoot, "var", "lib", "dkms", "*", "*", kernelVersion))
	if err != nil {
		return fmt.Errorf("failed to scan dkms build tree: %w", err)
	}
	for _, kernelDir := range kernelDirs {
		versionDir := filepath.Dir(kernelDir)
		version := filepath.Base(versionDir)
		source := filepath.Base(filepath.Dir(versionDir))

		conf := filepath.Join(installRoot, "usr", "src", source+"-"+version, "dkms.conf")
		content, err := os.ReadFile(conf)
		if err != nil {
			return fmt.Errorf("failed to read dkms.conf for %s/%s: %w", source, version, err)
		}
		declared := dkmsBuiltModuleNames(string(content))
		if len(declared) == 0 {
			continue
		}

		built := builtKoModuleNames(kernelDir)
		if missing := missingBuiltModules(declared, built); len(missing) > 0 {
			return fmt.Errorf("dkms source %s/%s: module(s) %s did not build for kernel %s",
				source, version, strings.Join(missing, ", "), kernelVersion)
		}
		log.Infof("DKMS source %s/%s: all %d declared module(s) built for kernel %s",
			source, version, len(declared), kernelVersion)
	}
	return nil
}

// dkmsBuiltModuleNames extracts the BUILT_MODULE_NAME values from a dkms.conf,
// handling both the indexed (BUILT_MODULE_NAME[0]="x") and plain
// (BUILT_MODULE_NAME="x") forms.
func dkmsBuiltModuleNames(dkmsConf string) []string {
	var names []string
	for _, m := range builtModuleNameRe.FindAllStringSubmatch(dkmsConf, -1) {
		if name := strings.TrimSpace(m[1]); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// builtKoModuleNames returns the module name of every compiled object anywhere
// under a source's per-kernel dkms build tree (the .ko may sit under an arch
// subdirectory), with the .ko and any compression suffix stripped.
func builtKoModuleNames(kernelDir string) []string {
	var names []string
	_ = filepath.WalkDir(kernelDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.Contains(d.Name(), ".ko") {
			names = append(names, koModuleName(d.Name()))
		}
		return nil
	})
	return names
}

// koModuleName strips a kernel object filename down to its module name,
// dropping the .ko and any compression suffix (.ko, .ko.zst, .ko.xz, .ko.gz).
func koModuleName(fileName string) string {
	for _, suffix := range []string{".zst", ".xz", ".gz"} {
		fileName = strings.TrimSuffix(fileName, suffix)
	}
	return strings.TrimSuffix(fileName, ".ko")
}

// missingBuiltModules returns the declared modules with no matching compiled
// object. dkms and the kernel spell '-' and '_' inconsistently between a
// BUILT_MODULE_NAME and the produced .ko, so matching compares both on a
// '_'-normalized form.
func missingBuiltModules(declared, builtKoNames []string) []string {
	built := map[string]bool{}
	for _, name := range builtKoNames {
		built[normalizeModuleName(name)] = true
	}
	var missing []string
	for _, d := range declared {
		if !built[normalizeModuleName(d)] {
			missing = append(missing, d)
		}
	}
	return missing
}

func normalizeModuleName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// signDkmsModules signs every built .ko for kernelVersion with the manifest's
// configured signing key/cert, verifies each is signed, and - if requested -
// retains the signing identity in the image for future on-target rebuilds.
func signDkmsModules(installRoot, kernelVersion string, dkms config.Dkms) error {
	sb := dkms.SecureBoot

	signFileHostPath, err := locateSignFile(installRoot, kernelVersion)
	if err != nil {
		return err
	}
	chrootSignFile, err := chrootRelative(signFileHostPath, installRoot)
	if err != nil {
		return err
	}

	modulesDir := filepath.Join(installRoot, "lib", "modules", kernelVersion, "updates", "dkms")
	kos, err := filepath.Glob(filepath.Join(modulesDir, "*.ko"))
	if err != nil {
		return fmt.Errorf("failed to list built dkms modules: %w", err)
	}
	if len(kos) == 0 {
		return fmt.Errorf("no built .ko files found under %s to sign", modulesDir)
	}

	// Stage the signing key/cert inside the chroot only for the duration of
	// signing; removed unconditionally afterward. RetainSigningIdentity below
	// governs a separate, deliberate copy to the manifest's target path - it
	// does not extend the lifetime of this transient staging copy.
	stagingDir, err := os.MkdirTemp(filepath.Join(installRoot, "tmp"), "ict-dkms-sign-")
	if err != nil {
		return fmt.Errorf("failed to create dkms signing staging dir: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(stagingDir); rmErr != nil {
			log.Warnf("Failed to remove dkms signing staging dir %s: %v", stagingDir, rmErr)
		}
	}()

	stagedKey := filepath.Join(stagingDir, "signing.key")
	stagedCert := filepath.Join(stagingDir, "signing.crt")
	if err := file.CopyFile(sb.SigningKeyPath, stagedKey, "", true); err != nil {
		return fmt.Errorf("failed to stage dkms signing key: %w", err)
	}
	if err := file.CopyFile(sb.SigningCertPath, stagedCert, "", true); err != nil {
		return fmt.Errorf("failed to stage dkms signing cert: %w", err)
	}
	if _, err := shell.ExecCmd(fmt.Sprintf("chmod 600 %s", shell.QuoteArg(stagedKey)), true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to lock down staged dkms signing key: %w", err)
	}

	chrootKey, err := chrootRelative(stagedKey, installRoot)
	if err != nil {
		return err
	}
	chrootCert, err := chrootRelative(stagedCert, installRoot)
	if err != nil {
		return err
	}

	for _, ko := range kos {
		chrootKo, err := chrootRelative(ko, installRoot)
		if err != nil {
			return err
		}

		// Invoked as `chroot <root> <sign-file> ...` directly (no /bin/bash -c
		// needed): sign-file's path is per-kernel-version and so can't be a
		// static shell allowlist entry, but only the leading "chroot" token is
		// checked - everything after it, including this dynamic path, passes
		// through untouched, same as addImageConfigs's existing cmd hook.
		signCmd := fmt.Sprintf("chroot %s %s sha512 %s %s %s",
			shell.QuoteArg(installRoot), shell.QuoteArg(chrootSignFile),
			shell.QuoteArg(chrootKey), shell.QuoteArg(chrootCert), shell.QuoteArg(chrootKo))
		if _, err := shell.ExecCmd(signCmd, true, shell.HostPath, nil); err != nil {
			return fmt.Errorf("failed to sign dkms module %s: %w", filepath.Base(ko), err)
		}

		// MVP verification: confirm a signer identity is present at all.
		// Asserting it matches the manifest-selected certificate's subject
		// would need parsing the cert (e.g. via openssl) - left as a
		// follow-up rather than silently skipped.
		signerOutput, err := shell.ExecCmdWithStream(
			fmt.Sprintf("modinfo -F signer %s", shell.QuoteArg(chrootKo)), true, installRoot, nil)
		if err != nil || strings.TrimSpace(signerOutput) == "" {
			return fmt.Errorf("dkms module %s is unsigned after signing attempt", filepath.Base(ko))
		}
	}

	if sb.RetainSigningIdentity {
		if err := retainSigningIdentity(installRoot, sb); err != nil {
			return err
		}
	}

	return nil
}

// retainSigningIdentity copies the signing key/cert into the image so a
// future on-target DKMS rebuild (e.g. after an HWE kernel update) can still
// produce Secure-Boot-trusted modules. Per review discussion on the EdgePack
// ADR, ICT does not attempt per-device key uniqueness here - a shared
// signing key's fleet-wide blast radius is treated as an OS-level file
// permission/authorization concern, not something this schema can solve.
func retainSigningIdentity(installRoot string, sb config.DkmsSecureBoot) error {
	targetKey := filepath.Join(installRoot, sb.TargetKeyPath)
	targetCert := filepath.Join(installRoot, sb.TargetCertPath)
	if err := file.CopyFile(sb.SigningKeyPath, targetKey, "", true); err != nil {
		return fmt.Errorf("failed to retain dkms signing key in image: %w", err)
	}
	if err := file.CopyFile(sb.SigningCertPath, targetCert, "", true); err != nil {
		return fmt.Errorf("failed to retain dkms signing cert in image: %w", err)
	}
	// Private key: stricter than the repo's general 0644 data convention,
	// intentionally - this is the material that lets a future on-target DKMS
	// rebuild sign modules trusted by Secure Boot, so it must not be
	// world/group readable.
	if _, err := shell.ExecCmd(fmt.Sprintf("chmod 600 %s", shell.QuoteArg(targetKey)), true, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to lock down retained dkms signing key: %w", err)
	}
	return nil
}

// locateSignFile finds the kernel's scripts/sign-file tool inside the
// chroot. Its path is version-specific (ships inside linux-headers-<version>,
// or on Debian/Ubuntu sometimes inside a linux-kbuild-<abi> package keyed by
// ABI rather than the full kernel version string), so it can't be a static
// shell allowlist entry.
func locateSignFile(installRoot, kernelVersion string) (string, error) {
	candidates := []string{
		filepath.Join(installRoot, "usr", "src", "linux-headers-"+kernelVersion, "scripts", "sign-file"),
	}
	if abi := kernelAbiFromVersion(kernelVersion); abi != "" {
		if matches, err := filepath.Glob(filepath.Join(installRoot, "usr", "lib", "linux-kbuild-"+abi+"*", "scripts", "sign-file")); err == nil {
			candidates = append(candidates, matches...)
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf(
		"scripts/sign-file not found for kernel %s under %s (install a matching linux-headers or linux-kbuild package)",
		kernelVersion, installRoot)
}

// kernelAbiFromVersion drops the flavour suffix from a kernel version string,
// e.g. "6.14.0-29-generic" -> "6.14.0-29".
func kernelAbiFromVersion(kernelVersion string) string {
	parts := strings.Split(kernelVersion, "-")
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "-")
}

// chrootRelative converts a host path (already known to be under installRoot)
// into the equivalent absolute path as seen from inside the chroot.
func chrootRelative(hostPath, installRoot string) (string, error) {
	rel, err := filepath.Rel(installRoot, hostPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute chroot-relative path for %s: %w", hostPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path %s escapes chroot %s", hostPath, installRoot)
	}
	return "/" + rel, nil
}
