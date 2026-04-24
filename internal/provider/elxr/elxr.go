package elxr

import (
	"fmt"
	"path/filepath"

	"github.com/open-edge-platform/os-image-composer/internal/chroot"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/image/initrdmaker"
	"github.com/open-edge-platform/os-image-composer/internal/image/isomaker"
	"github.com/open-edge-platform/os-image-composer/internal/image/rawmaker"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/debutils"
	"github.com/open-edge-platform/os-image-composer/internal/provider"
	"github.com/open-edge-platform/os-image-composer/internal/utils/display"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
	"github.com/open-edge-platform/os-image-composer/internal/utils/system"
)

// DEB: https://deb.debian.org/debian/dists/bookworm/main/binary-amd64/Packages.gz
// DEB Download Path: https://deb.debian.org/debian/pool/main/0/0ad/0ad_0.0.26-3_amd64.deb
// eLxr: https://mirror.elxr.dev/elxr/dists/aria/main/binary-amd64/Packages.gz
// eLxr Download Path: https://mirror.elxr.dev/elxr/pool/main/p/python3-defaults/2to3_3.11.2-1_all.deb
const (
	OsName = "wind-river-elxr"
)

var log = logger.Logger()

// eLxr implements provider.Provider
type eLxr struct {
	repoCfgs  []debutils.RepoConfig // Add this field
	chrootEnv chroot.ChrootEnvInterface
}

func Register(targetOs, targetDist, targetArch string) error {
	chrootEnv, err := chroot.NewChrootEnv(targetOs, targetDist, targetArch)
	if err != nil {
		return fmt.Errorf("failed to inject chroot dependency: %w", err)
	}
	provider.Register(&eLxr{
		chrootEnv: chrootEnv,
	}, targetDist, targetArch)

	return nil
}

// Name returns the unique name of the provider
func (p *eLxr) Name(dist, arch string) string {
	return system.GetProviderId(OsName, dist, arch)
}

// Init will initialize the provider, fetching repo configuration
func (p *eLxr) Init(dist, arch string) error {

	// Architecture mapping if needed
	switch arch {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	}

	cfgs, err := loadRepoConfig("", arch)
	if err != nil {
		log.Errorf("Parsing repo config failed: %v", err)
		return err
	}
	p.repoCfgs = cfgs

	log.Infof("Initialized elxr provider with %d repositories", len(cfgs))
	for i, cfg := range cfgs {
		log.Infof("Repository %d: name=%s, package list url=%s, package download url=%s",
			i+1, cfg.Name, cfg.PkgList, cfg.PkgPrefix)
	}
	return nil
}

func (p *eLxr) PreProcess(template *config.ImageTemplate) error {
	// Generate apt sources file from packageRepositories
	if err := template.GenerateAptSourcesFromRepositories(); err != nil {
		return fmt.Errorf("failed to generate apt sources from repositories: %w", err)
	}

	if err := p.installHostDependency(); err != nil {
		return fmt.Errorf("failed to install host dependencies: %w", err)
	}

	template.StartDownloadImagePkgsTimer()
	if err := p.downloadImagePkgs(template); err != nil {
		template.FinishDownloadImagePkgsTimer()
		return fmt.Errorf("failed to download image packages: %w", err)
	}
	template.FinishDownloadImagePkgsTimer()
	if templateAwareChrootEnv, ok := p.chrootEnv.(interface{ SetBuildTemplate(*config.ImageTemplate) }); ok {
		templateAwareChrootEnv.SetBuildTemplate(template)
	}

	if err := p.chrootEnv.InitChrootEnv(template.Target.OS,
		template.Target.Dist, template.Target.Arch); err != nil {
		return fmt.Errorf("failed to initialize chroot environment: %w", err)
	}
	return nil
}

func (p *eLxr) BuildImage(template *config.ImageTemplate) error {
	if template == nil {
		return fmt.Errorf("template cannot be nil")
	}

	log.Infof("Building image: %s", template.GetImageName())

	// Create makers with template when needed
	switch template.Target.ImageType {
	case "raw":
		return p.buildRawImage(template)
	case "img":
		return p.buildInitrdImage(template)
	case "iso":
		return p.buildIsoImage(template)
	default:
		return fmt.Errorf("unsupported image type: %s", template.Target.ImageType)
	}
}

func (p *eLxr) buildRawImage(template *config.ImageTemplate) error {
	// Create RawMaker with template (dependency injection)
	rawMaker, err := rawmaker.NewRawMaker(p.chrootEnv, template)
	if err != nil {
		return fmt.Errorf("failed to create raw maker: %w", err)
	}

	// Use the maker
	if err := rawMaker.Init(); err != nil {
		return fmt.Errorf("failed to initialize raw maker: %w", err)
	}

	if err := rawMaker.BuildRawImage(); err != nil {
		return err
	}

	// Display summary after build completes (loop device detached, files accessible)
	// Construct the actual image build directory path (on host, not in chroot)
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get work directory: %w", err)
	}
	providerId := system.GetProviderId(template.Target.OS, template.Target.Dist, template.Target.Arch)
	imageBuildDir := filepath.Join(globalWorkDir, providerId, "imagebuild", template.GetSystemConfigName())

	displayImageArtifacts(imageBuildDir, "RAW")

	return nil
}

func (p *eLxr) buildInitrdImage(template *config.ImageTemplate) error {
	// Create InitrdMaker with template (dependency injection)
	initrdMaker, err := initrdmaker.NewInitrdMaker(p.chrootEnv, template)
	if err != nil {
		return fmt.Errorf("failed to create initrd maker: %w", err)
	}

	// Use the maker
	if err := initrdMaker.Init(); err != nil {
		return fmt.Errorf("failed to initialize initrd image maker: %w", err)
	}
	if err := initrdMaker.BuildInitrdImage(); err != nil {
		return fmt.Errorf("failed to build initrd image: %w", err)
	}
	if err := initrdMaker.CleanInitrdRootfs(); err != nil {
		return fmt.Errorf("failed to clean initrd rootfs: %w", err)
	}

	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get work directory: %w", err)
	}
	providerId := system.GetProviderId(template.Target.OS, template.Target.Dist, template.Target.Arch)
	imageBuildDir := filepath.Join(globalWorkDir, providerId, "imagebuild", template.GetSystemConfigName())

	displayImageArtifacts(imageBuildDir, "IMG")

	return nil
}

func (p *eLxr) buildIsoImage(template *config.ImageTemplate) error {
	// Create IsoMaker with template (dependency injection)
	isoMaker, err := isomaker.NewIsoMaker(p.chrootEnv, template)
	if err != nil {
		return fmt.Errorf("failed to create iso maker: %w", err)
	}

	// Use the maker
	if err := isoMaker.Init(); err != nil {
		return fmt.Errorf("failed to initialize iso maker: %w", err)
	}

	if err := isoMaker.BuildIsoImage(); err != nil {
		return err
	}

	// Display summary after build completes
	// Construct the actual image build directory path (on host, not in chroot)
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return fmt.Errorf("failed to get work directory: %w", err)
	}
	providerId := system.GetProviderId(template.Target.OS, template.Target.Dist, template.Target.Arch)
	imageBuildDir := filepath.Join(globalWorkDir, providerId, "imagebuild", template.GetSystemConfigName())

	displayImageArtifacts(imageBuildDir, "ISO")

	return nil
}

func (p *eLxr) PostProcess(template *config.ImageTemplate, err error) error {
	if err := p.chrootEnv.CleanupChrootEnv(template.Target.OS,
		template.Target.Dist, template.Target.Arch); err != nil {
		return fmt.Errorf("failed to cleanup chroot environment: %w", err)
	}
	return nil
}

func (p *eLxr) installHostDependency() error {
	var dependencyInfo = map[string]string{
		"mmdebstrap":        "mmdebstrap",       // For the chroot env build
		"mkfs.fat":          "dosfstools",       // For the FAT32 boot partition creation
		"mformat":           "mtools",           // For writing files to FAT32 partition
		"xorriso":           "xorriso",          // For ISO image creation
		"qemu-img":          "qemu-utils",       // For image file format conversion
		"ukify":             "systemd-ukify",    // For the UKI image creation
		"grub-mkimage":      "grub-common",      // For ISO image UEFI Grub binary creation
		"veritysetup":       "cryptsetup",       // For the veritysetup command
		"sbsign":            "sbsigntool",       // For the UKI image creation
		"dpkg-scanpackages": "dpkg-dev",         // For DEB repository metadata creation
		"arch-test":         "arch-test",        // Required by mmdebstrap for foreign-architecture bootstrap
		"qemu-user-static":  "qemu-user-static", // For cross-architecture binary execution support
	}
	hostPkgManager, err := system.GetHostOsPkgManager()
	if err != nil {
		return fmt.Errorf("failed to get host package manager: %w", err)
	}

	for cmd, pkg := range dependencyInfo {
		cmdExist, err := shell.IsCommandExist(cmd, shell.HostPath)
		if err != nil {
			return fmt.Errorf("failed to check command %s existence: %w", cmd, err)
		}
		if !cmdExist {
			cmdStr := fmt.Sprintf("%s install -y %s", hostPkgManager, pkg)
			if _, err := shell.ExecCmdWithStream(cmdStr, true, shell.HostPath, nil); err != nil {
				return fmt.Errorf("failed to install host dependency %s: %w", pkg, err)
			}
			log.Debugf("Installed host dependency: %s", pkg)
		} else {
			log.Debugf("Host dependency %s is already installed", pkg)
		}
	}
	return nil
}

func (p *eLxr) downloadImagePkgs(template *config.ImageTemplate) error {
	if err := p.chrootEnv.UpdateSystemPkgs(template); err != nil {
		return fmt.Errorf("failed to update system packages: %w", err)
	}
	pkgList := template.GetPackages()
	pkgSources := template.GetPackageSourceMap()
	providerId := p.Name(template.Target.Dist, template.Target.Arch)
	globalCache, err := config.CacheDir()
	if err != nil {
		return fmt.Errorf("failed to get global cache dir: %w", err)
	}
	pkgCacheDir := filepath.Join(globalCache, "pkgCache", providerId)

	// Configure multiple repositories
	if len(p.repoCfgs) == 0 {
		return fmt.Errorf("no repository configurations available")
	}

	// Set up all repositories for debutils
	debutils.RepoCfgs = p.repoCfgs

	// Set up primary repository for backward compatibility
	primaryRepo := p.repoCfgs[0]
	debutils.RepoCfg = primaryRepo
	debutils.GzHref = primaryRepo.PkgList
	debutils.Architecture = primaryRepo.Arch
	debutils.UserRepo = template.GetPackageRepositories()

	log.Infof("Configured %d repositories for package download", len(p.repoCfgs))
	for i, cfg := range p.repoCfgs {
		log.Infof("Repository %d: %s (%s)", i+1, cfg.Name, cfg.PkgList)
	}

	fullPkgList, fullPkgListBom, err := debutils.DownloadPackagesComplete(pkgList, pkgCacheDir, template.DotFilePath, pkgSources, template.DotSystemOnly)
	if err != nil {
		return fmt.Errorf("failed to download packages: %w", err)
	}
	template.FullPkgList = fullPkgList
	template.FullPkgListBom = fullPkgListBom

	return nil
}

func loadRepoConfig(repoUrl string, arch string) ([]debutils.RepoConfig, error) {
	var repoConfigs []debutils.RepoConfig

	// Load provider repo config for elxr - use correct OS name
	providerConfigs, err := config.LoadProviderRepoConfig(OsName, "elxr12", arch) // Use "wind-river-elxr" and "aria" dist
	if err != nil {
		return repoConfigs, fmt.Errorf("failed to load provider repo config: %w", err)
	}

	repoList := make([]debutils.Repository, len(providerConfigs))
	repoGroup := "elxr"

	// Convert each ProviderRepoConfig to debutils.RepoConfig
	for i, providerConfig := range providerConfigs {
		repoType, name, _, gpgKey, component, _, _, _, _, baseURL, _, _, _ := providerConfig.ToRepoConfigData(arch)

		// Verify this is a DEB repository
		if repoType != "deb" {
			log.Warnf("Skipping non-DEB repository: %s (type: %s)", name, repoType)
			continue
		}

		repoList[i] = debutils.Repository{
			ID:        fmt.Sprintf("%s%d", repoGroup, i+1),
			Codename:  name,
			URL:       baseURL,
			PKey:      gpgKey,
			Component: component,
			Priority:  0, // Default priority
		}
	}

	repoConfigs, err = debutils.BuildRepoConfigs(repoList, arch)
	if err != nil {
		return nil, fmt.Errorf("building user repo configs failed: %w", err)
	}

	if len(repoConfigs) == 0 {
		return repoConfigs, fmt.Errorf("no valid DEB repositories found")
	}

	return repoConfigs, nil
}

// displayImageArtifacts displays all image artifacts in the build directory
func displayImageArtifacts(imageBuildDir, imageType string) {
	display.PrintImageDirectorySummary(
		imageBuildDir,
		imageType,
	)
}
