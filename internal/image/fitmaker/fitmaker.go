// Package fitmaker builds U-Boot FIT (Flattened Image Tree) images: a single
// file bundling a kernel, one or more device trees, and a ramdisk, used by
// externally-booted embedded targets that load their boot chain over a
// network or management interface rather than from a local disk.
package fitmaker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/file"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/mount"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

type FitMakerInterface interface {
	Init() error
	BuildFitImage() error
	GetFitVersion() string
	GetFitFilePath() string
	GetFitRootfsPath() string
	CleanFitRootfs() error
}

// FitMaker builds a FIT image from an installed rootfs. The rootfs is
// installed exactly like an initrd/img target (no disk, no partitions, no
// bootloader) -- the only difference is the final packaging step, which
// extracts the kernel and device trees out of the tree instead of leaving
// them in the ramdisk, and wraps everything in a U-Boot FIT container
// instead of a bare gzipped cpio archive.
type FitMaker struct {
	template      *config.ImageTemplate
	ImageBuildDir string
	FitRootfsPath string
	FitFilePath   string
	VersionInfo   string
	ChrootEnv     chroot.ChrootEnvInterface
	ImageOs       imageos.ImageOsInterface
}

var log = logger.Logger()

func NewFitMaker(chrootEnv chroot.ChrootEnvInterface, template *config.ImageTemplate) (*FitMaker, error) {
	// nil checking is done once in the constructor only to avoid repetitive
	// checks in every method; schema validation already guarantees a valid
	// template by the time a provider reaches BuildImage.
	if template == nil {
		return nil, fmt.Errorf("image template cannot be nil")
	}
	if chrootEnv == nil {
		return nil, fmt.Errorf("chroot environment cannot be nil")
	}

	imageOs, err := imageos.NewImageOs(chrootEnv, template)
	if err != nil {
		return nil, fmt.Errorf("failed to create image OS: %w", err)
	}

	return &FitMaker{
		template:  template,
		ChrootEnv: chrootEnv,
		ImageOs:   imageOs,
	}, nil
}

func (fitMaker *FitMaker) Init() error {
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get global work directory: %w", err)
	}

	providerId := system.GetProviderId(
		fitMaker.template.Target.OS,
		fitMaker.template.Target.Dist,
		fitMaker.template.Target.Arch,
	)

	fitMaker.ImageBuildDir = filepath.Join(
		globalWorkDir,
		providerId,
		"imagebuild",
		fitMaker.template.GetSystemConfigName(),
	)

	return os.MkdirAll(fitMaker.ImageBuildDir, 0700)
}

func (fitMaker *FitMaker) GetFitVersion() string    { return fitMaker.VersionInfo }
func (fitMaker *FitMaker) GetFitFilePath() string   { return fitMaker.FitFilePath }
func (fitMaker *FitMaker) GetFitRootfsPath() string { return fitMaker.FitRootfsPath }

func (fitMaker *FitMaker) BuildFitImage() (err error) {
	log.Infof("Building FIT image for: %s", fitMaker.template.GetImageName())

	imageName := fitMaker.template.GetImageName()

	fitMaker.FitRootfsPath, fitMaker.VersionInfo, err = fitMaker.ImageOs.InstallInitrd()
	if err != nil {
		if cleanErr := fitMaker.CleanFitRootfs(); cleanErr != nil {
			log.Errorf("Failed to clean FIT rootfs after install failure: %v", cleanErr)
		}
		return fmt.Errorf("failed to install FIT rootfs: %w", err)
	}

	fitMaker.FitFilePath = filepath.Join(fitMaker.ImageBuildDir, fmt.Sprintf("%s-%s.itb",
		imageName, fitMaker.VersionInfo))

	// Copy SBOM into the rootfs (inside the image) before it's packed.
	if err := manifest.CopySBOMToChroot(fitMaker.FitRootfsPath); err != nil {
		log.Warnf("Failed to copy SBOM into FIT rootfs: %v", err)
		// Don't fail the build if SBOM copy fails, just log warning.
	}

	if err := fitMaker.createFitImage(); err != nil {
		return fmt.Errorf("failed to create FIT image: %w", err)
	}

	// Copy SBOM to image build directory
	if err := manifest.CopySBOMToImageBuildDir(fitMaker.ImageBuildDir); err != nil {
		log.Warnf("Failed to copy SBOM to image build directory: %v", err)
	}

	fitMaker.template.FinishPureImageBuildTimer()
	pureImageBuildDuration := fitMaker.template.GetPureImageBuildDuration()
	if pureImageBuildDuration > 0 {
		log.Infof("Pure FIT image build time: %s", pureImageBuildDuration.Round(time.Millisecond))
		log.Infof("FIT image build completed successfully: %s", fitMaker.FitFilePath)
	}

	return nil
}

// findKernel returns the rootfs-relative path of the single vmlinuz-* kernel
// under /boot, matching the same /boot/vmlinuz-* convention the existing img
// (initrd) target already relies on. This lists the rootfs as a plain host
// directory rather than chrooting into it: listing files doesn't need to
// execute anything built for the target architecture.
func (fitMaker *FitMaker) findKernel() (string, error) {
	cmdStr := fmt.Sprintf("ls %s | grep '^vmlinuz-'", shell.QuoteArg(filepath.Join(fitMaker.FitRootfsPath, "boot")))
	output, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
	if err != nil {
		return "", fmt.Errorf("failed to list vmlinuz files in /boot: %w", err)
	}

	var names []string
	for _, line := range strings.Split(output, "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no vmlinuz-* kernel found in /boot")
	}
	if len(names) > 1 {
		return "", fmt.Errorf("expected exactly one kernel in /boot, found %d: %v", len(names), names)
	}
	return filepath.Join("boot", names[0]), nil
}

// findDTBs returns the rootfs-relative paths of every *.dtb file anywhere
// under /boot (kernel packages commonly nest them under a per-version
// subdirectory, e.g. /boot/dtb-<version>/<vendor>/*.dtb), sorted for a
// deterministic default/configuration order.
func (fitMaker *FitMaker) findDTBs() ([]string, error) {
	bootDir := filepath.Join(fitMaker.FitRootfsPath, "boot")
	cmdStr := fmt.Sprintf("find %s -name '*.dtb' -printf '%%P\\n'", shell.QuoteArg(bootDir))
	output, err := shell.ExecCmd(cmdStr, true, shell.HostPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to find device trees under /boot: %w", err)
	}

	var dtbs []string
	for _, line := range strings.Split(output, "\n") {
		rel := strings.TrimSpace(line)
		if rel != "" {
			dtbs = append(dtbs, filepath.Join("boot", rel))
		}
	}
	if len(dtbs) == 0 {
		return nil, fmt.Errorf("no *.dtb files found under /boot")
	}
	sort.Strings(dtbs)
	return dtbs, nil
}

// confName derives a FIT configuration name from a DTB's basename.
func confName(dtbPath string) string {
	base := filepath.Base(dtbPath)
	return "conf-" + strings.TrimSuffix(base, ".dtb")
}

// writeFitITS writes the .its (Image Tree Source) file mkimage consumes to
// build the FIT container: one kernel, one ramdisk, a configuration per DTB
// (the first is the default), and a small version-trailer data node some
// bootloaders expect alongside the image.
func writeFitITS(itsPath string, dtbNames []string) error {
	var sb strings.Builder

	sb.WriteString(`/dts-v1/;

/ {
	description = "Image Composer Tool FIT image";
	#address-cells = <2>;

	images {
		kernel {
			description = "Kernel";
			data = /incbin/("./Image");
			type = "kernel";
			arch = "arm64";
			os = "linux";
			compression = "none";
			hash {
				algo = "sha1";
			};
		};
`)

	for i, name := range dtbNames {
		label := strings.TrimSuffix(name, ".dtb")
		fmt.Fprintf(&sb, `		fdt%d {
			description = "%s";
			data = /incbin/("%s");
			type = "flat_dt";
			arch = "arm64";
			compression = "none";
			hash {
				algo = "sha1";
			};
		};
`, i+1, label, name)
	}

	sb.WriteString(`		ramdisk {
			description = "ramdisk";
			data = /incbin/("rootfs.img");
			type = "ramdisk";
			arch = "arm64";
			os = "linux";
			compression = "none";
			hash {
				algo = "sha1";
			};
		};
		versions {
			description = "image versions";
			data = /incbin/("version-trailer.bin");
			type = "invalid";
			compression = "none";
		};
	};

	configurations {
`)

	fmt.Fprintf(&sb, "\t\tdefault = %q;\n", confName(dtbNames[0]))

	for i, name := range dtbNames {
		cname := confName(name)
		fmt.Fprintf(&sb, `		%s {
			description = "Boot with %s";
			kernel = "kernel";
			fdt = "fdt%d";
			ramdisk = "ramdisk";
		};
`, cname, strings.TrimSuffix(name, ".dtb"), i+1)
	}

	sb.WriteString(`	};
};
`)

	return os.WriteFile(itsPath, []byte(sb.String()), 0644)
}

// createVersionTrailer writes the 4KB placeholder data node some bootloaders
// expect alongside a FIT image; it carries no content of its own today.
func createVersionTrailer(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(4096)
}

func (fitMaker *FitMaker) createFitImage() error {
	if exists, err := shell.IsCommandExist("mkimage", shell.HostPath); err != nil {
		return fmt.Errorf("failed to check for required host dependency 'mkimage': %w", err)
	} else if !exists {
		return fmt.Errorf("required host dependency 'mkimage' not found")
	}

	kernelRel, err := fitMaker.findKernel()
	if err != nil {
		return err
	}
	dtbsRel, err := fitMaker.findDTBs()
	if err != nil {
		return err
	}

	// A duplicate DTB basename under different directories would silently
	// overwrite the first copy when staged flat into the work directory;
	// reject it instead of producing a FIT image missing a configuration.
	dtbNames := make([]string, 0, len(dtbsRel))
	seen := make(map[string]string, len(dtbsRel))
	for _, rel := range dtbsRel {
		name := filepath.Base(rel)
		if prev, dup := seen[name]; dup {
			return fmt.Errorf("duplicate device tree basename %q: %s and %s", name, prev, rel)
		}
		seen[name] = rel
		dtbNames = append(dtbNames, name)
	}

	workDir, err := os.MkdirTemp("", "fit")
	if err != nil {
		return fmt.Errorf("failed to create FIT work directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	if err := file.CopyFile(filepath.Join(fitMaker.FitRootfsPath, kernelRel), filepath.Join(workDir, "Image"), "--preserve=mode", true); err != nil {
		return fmt.Errorf("failed to stage kernel for FIT image: %w", err)
	}
	for _, rel := range dtbsRel {
		if err := file.CopyFile(filepath.Join(fitMaker.FitRootfsPath, rel), filepath.Join(workDir, filepath.Base(rel)), "--preserve=mode", true); err != nil {
			return fmt.Errorf("failed to stage device tree %s for FIT image: %w", rel, err)
		}
	}

	if err := createVersionTrailer(filepath.Join(workDir, "version-trailer.bin")); err != nil {
		return fmt.Errorf("failed to create version trailer: %w", err)
	}
	if err := writeFitITS(filepath.Join(workDir, "fit.its"), dtbNames); err != nil {
		return fmt.Errorf("failed to write FIT image tree source: %w", err)
	}

	// The local package cache bind mount must not end up inside the ramdisk.
	if err := mount.UmountPath(fitMaker.FitRootfsPath + "/cdrom/cache-repo"); err != nil {
		return fmt.Errorf("failed to unmount cache-repo %s: %w",
			fitMaker.FitRootfsPath+"/cdrom/cache-repo", err)
	}

	// Pack everything except the kernel and device trees (already extracted
	// above, and not meant to also ride along inside the ramdisk) into the
	// gzipped cpio archive mkimage embeds as the FIT's ramdisk component.
	rootfsImgPath := filepath.Join(workDir, "rootfs.img")
	packCmd := fmt.Sprintf(
		`cd %s && sudo find . -not \( -path './%s' -o -name '*.dtb' \) | sudo cpio -o -H newc | sudo gzip > %s`,
		fitMaker.FitRootfsPath, kernelRel, rootfsImgPath)
	if _, err := shell.ExecCmdWithStream(packCmd, false, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to create FIT ramdisk: %w", err)
	}

	mkimageCmd := fmt.Sprintf("cd %s && mkimage -f fit.its %s", workDir, fitMaker.FitFilePath)
	if _, err := shell.ExecCmdWithStream(mkimageCmd, false, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to build FIT image with mkimage: %w", err)
	}

	if _, err := os.Stat(fitMaker.FitFilePath); os.IsNotExist(err) {
		return fmt.Errorf("FIT image file does not exist: %s", fitMaker.FitFilePath)
	}
	return nil
}

func (fitMaker *FitMaker) CleanFitRootfs() error {
	log.Infof("Cleaning up FIT rootfs: %s", fitMaker.FitRootfsPath)

	if fitMaker.FitRootfsPath == "" {
		log.Debugf("FIT rootfs path is empty, nothing to clean")
		return nil
	}

	if _, err := os.Stat(fitMaker.FitRootfsPath); os.IsNotExist(err) {
		log.Debugf("FIT rootfs path does not exist: %s", fitMaker.FitRootfsPath)
		return nil
	}

	if err := mount.UmountPath(fitMaker.FitRootfsPath + "/cdrom/cache-repo"); err != nil {
		log.Errorf("Failed to unmount cache-repo %s: %v",
			fitMaker.FitRootfsPath+"/cdrom/cache-repo", err)
		return fmt.Errorf("failed to unmount cache-repo %s: %w",
			fitMaker.FitRootfsPath+"/cdrom/cache-repo", err)
	}

	if _, err := shell.ExecCmd("rm -rf "+fitMaker.FitRootfsPath, true, shell.HostPath, nil); err != nil {
		log.Errorf("Failed to remove FIT rootfs directory %s: %v", fitMaker.FitRootfsPath, err)
		return fmt.Errorf("failed to remove FIT rootfs directory: %w", err)
	}
	return nil
}
