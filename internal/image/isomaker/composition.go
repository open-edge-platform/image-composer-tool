package isomaker

import (
	"fmt"
	"path/filepath"

	"github.com/open-edge-platform/image-composer-tool/internal/chroot/deb"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageos"
)

// writeTargetSBOM writes the SPDX SBOM of the system the ISO installs, from
// the package set resolved at compose time, so the ISO build retains an SBOM
// next to the ISO. (The installed system additionally gets its own SBOM from
// the installer.) The file lands in the temp dir under the name
// CopySBOMToImageBuildDir copies.
func (isoMaker *IsoMaker) writeTargetSBOM() (string, error) {
	template := isoMaker.template
	name := imageos.SBOMFileName(isoMaker.ChrootEnv.GetTargetOsPkgType(), template.GetImageName())
	manifest.DefaultSPDXFile = name
	if err := manifest.WriteSPDXToFile(template.FullPkgListBom, filepath.Join(config.TempDir(), name)); err != nil {
		return "", fmt.Errorf("failed to write target system SBOM: %w", err)
	}
	return name, nil
}

// buildCompositionManifest captures the composition inputs. It must run
// before copyConfigFilesToIso, which rewrites additionalFiles to their
// on-ISO locations.
func (isoMaker *IsoMaker) buildCompositionManifest(artifact, sbom string) (manifest.CompositionManifest, error) {
	template := isoMaker.template
	// Deb provider repository configs are keyed by the Debian architecture.
	arch := template.Target.Arch
	if isoMaker.ChrootEnv.GetTargetOsPkgType() == "deb" {
		if mapped, err := deb.NormalizeDebArch(arch); err == nil {
			arch = mapped
		}
	}
	baseRepos, err := config.LoadProviderRepoConfig(template.Target.OS, template.Target.Dist, arch)
	if err != nil {
		log.Warnf("Composition manifest will not list base repositories: %v", err)
	}
	return manifest.BuildCompositionManifest(template, baseRepos, artifact, sbom)
}
