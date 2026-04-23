package rcd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-edge-platform/os-image-composer/internal/chroot"
	"github.com/open-edge-platform/os-image-composer/internal/config"
	"github.com/open-edge-platform/os-image-composer/internal/image/initrdmaker"
	"github.com/open-edge-platform/os-image-composer/internal/image/isomaker"
	"github.com/open-edge-platform/os-image-composer/internal/image/rawmaker"
	"github.com/open-edge-platform/os-image-composer/internal/ospackage/rpmutils"
	"github.com/open-edge-platform/os-image-composer/internal/provider"
	"github.com/open-edge-platform/os-image-composer/internal/utils/display"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
	"github.com/open-edge-platform/os-image-composer/internal/utils/shell"
	"github.com/open-edge-platform/os-image-composer/internal/utils/system"
)

const (
	OsName   = "redhat-compatible-distro"
	repodata = "repodata/repomd.xml"
)

var log = logger.Logger()

// RCD implements provider.Provider
type RCD struct {
	repoCfg   rpmutils.RepoConfig
	gzHref    string
	chrootEnv chroot.ChrootEnvInterface
}

func Register(targetOs, targetDist, targetArch string) error {
	chrootEnv, err := chroot.NewChrootEnv(targetOs, targetDist, targetArch)
	if err != nil {
		return fmt.Errorf("failed to inject chroot dependency: %w", err)
	}

	provider.Register(&RCD{
		chrootEnv: chrootEnv,
	}, targetDist, targetArch)

	return nil
}

// Name returns the unique name of the provider
func (p *RCD) Name(dist, arch string) string {
	return system.GetProviderId(OsName, dist, arch)
}

// Init will initialize the provider, using centralized config with secure HTTP
func (p *RCD) Init(dist, arch string) error {
	// Load centralized YAML configuration first
	cfg, err := loadRepoConfigFromYAML(dist, arch)
	if err != nil {
		log.Errorf("Failed to load centralized repo config: %v", err)
		return err
	}

	// Use secure HTTP to fetch repository metadata from the centralized config URL
	// Note: rpmutils.FetchPrimaryURL internally uses network.NewSecureHTTPClient() for secure HTTPS communication
	repoDataURL := cfg.URL + "/" + repodata
	href, err := rpmutils.FetchPrimaryURL(repoDataURL)
	if err != nil {
		log.Errorf("Fetch primary.xml.gz failed from %s: %v", repoDataURL, err)
		return err
	}

	p.repoCfg = cfg
	p.gzHref = href

	log.Infof("redhat-compatible-distro provider initialized for dist=%s, arch=%s", dist, arch)
	log.Infof("repo section=%s", cfg.Section)
	log.Infof("name=%s", cfg.Name)
	log.Infof("url=%s", cfg.URL)
	log.Infof("primary.xml.gz=%s", p.gzHref)
	log.Infof("using %d workers for downloads", config.Workers())
	if err := os.MkdirAll(config.TempDir(), 0700); err != nil {
		log.Errorf("Failed to create temp directory for RCD: %v", err)
		return fmt.Errorf("failed to create temp directory for RCD: %w", err)
	}
	return nil
}

func (p *RCD) PreProcess(template *config.ImageTemplate) error {
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

func (p *RCD) BuildImage(template *config.ImageTemplate) error {
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

func (p *RCD) buildRawImage(template *config.ImageTemplate) error {
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

func (p *RCD) buildInitrdImage(template *config.ImageTemplate) error {
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

func (p *RCD) buildIsoImage(template *config.ImageTemplate) error {
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

func (p *RCD) PostProcess(template *config.ImageTemplate, err error) error {
	if err := p.chrootEnv.CleanupChrootEnv(template.Target.OS,
		template.Target.Dist, template.Target.Arch); err != nil {
		return fmt.Errorf("failed to cleanup chroot environment: %w", err)
	}
	return err
}

func (p *RCD) installHostDependency() error {
	var dependencyInfo = map[string]string{
		"rpm":          "rpm",         // For the chroot env build RPM pkg installation
		"mkfs.fat":     "dosfstools",  // For the FAT32 boot partition creation
		"qemu-img":     "qemu-utils",  // For image file format conversion
		"mformat":      "mtools",      // For writing files to FAT32 partition
		"xorriso":      "xorriso",     // For ISO image creation
		"grub-mkimage": "grub-common", // For ISO image UEFI Grub binary creation
		"sbsign":       "sbsigntool",  // For the UKI image creation
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

func (p *RCD) downloadImagePkgs(template *config.ImageTemplate) error {
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
	rpmutils.RepoCfg = p.repoCfg
	rpmutils.GzHref = p.gzHref
	rpmutils.Dist = template.Target.Dist
	rpmutils.UserRepo = template.GetPackageRepositories()
	rpmutils.ConfigureKernelSelection(template.GetKernelPackages(), template.GetKernel().Version)
	defer rpmutils.ConfigureKernelSelection(nil, "")

	fullPkgList, fullPkgListBom, err := rpmutils.DownloadPackagesComplete(pkgList, pkgCacheDir, template.DotFilePath, pkgSources, template.DotSystemOnly)
	if err != nil {
		return fmt.Errorf("failed to download packages: %w", err)
	}
	template.FullPkgList = fullPkgList
	template.FullPkgListBom = fullPkgListBom

	return nil
}

// loadRepoConfigFromYAML loads repository configuration from centralized YAML config
func loadRepoConfigFromYAML(dist, arch string) (rpmutils.RepoConfig, error) {
	// Load the centralized provider config
	providerConfigs, err := config.LoadProviderRepoConfig(OsName, dist, arch)
	if err != nil {
		return rpmutils.RepoConfig{}, fmt.Errorf("failed to load provider repo config: %w", err)
	}

	// Use the first repository configuration for backward compatibility
	if len(providerConfigs) == 0 {
		return rpmutils.RepoConfig{}, fmt.Errorf("no repository configurations found")
	}

	providerConfig := providerConfigs[0]

	// Convert to rpmutils.RepoConfig using the unified method
	repoType, name, url, gpgKey, component, buildPath, pkgPrefix, releaseFile, releaseSign, _, gpgCheck, repoGPGCheck, enabled := providerConfig.ToRepoConfigData(arch)

	// Verify this is an RPM repository
	if repoType != "rpm" {
		return rpmutils.RepoConfig{}, fmt.Errorf("expected RPM repository type, got: %s", repoType)
	}

	cfg := rpmutils.RepoConfig{
		Name:         name,
		URL:          url,
		GPGKey:       gpgKey,
		Section:      component, // Map component to Section for RPM utils
		GPGCheck:     gpgCheck,
		RepoGPGCheck: repoGPGCheck,
		Enabled:      enabled,
	}

	// Log unused DEB-specific fields for debugging
	_ = pkgPrefix
	_ = releaseFile
	_ = releaseSign
	_ = buildPath

	log.Infof("Loaded repo config from YAML for %s: %+v", OsName, cfg)
	return cfg, nil
}

// displayImageArtifacts displays all image artifacts in the build directory
func displayImageArtifacts(imageBuildDir, imageType string) {
	display.PrintImageDirectorySummary(
		imageBuildDir,
		imageType,
	)
}
