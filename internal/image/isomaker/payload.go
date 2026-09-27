package isomaker

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/isopayload"
	"github.com/open-edge-platform/image-composer-tool/internal/image/rawmaker"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
)

// payloadDirName is the on-ISO directory holding the compressed installer
// payload, its manifest, and its SBOM.
const payloadDirName = "payload"

// payloadPassthroughConvert is a no-op ImageConvertInterface injected into the
// payload raw build. imageconvert.ConvertImageFile's disk.artifacts handling
// (format conversion, compression, or deleting the raw file) is for the raw
// image's own output artifacts and must never run against the intermediate
// payload raw image; isopayload.Compress applies
// systemConfig.installerPayload.compression separately once the raw build
// finishes.
type payloadPassthroughConvert struct{}

func (payloadPassthroughConvert) ConvertImageFile(filePath string, template *config.ImageTemplate) error {
	if artifacts := template.GetDiskConfig().Artifacts; len(artifacts) > 0 {
		log.Warnf("disk.artifacts is ignored for the installer payload raw image (%s); "+
			"the payload raw is retained as-is and compressed per systemConfig.installerPayload.compression", filePath)
	}
	return nil
}

// synthesizeRawTemplate returns a raw-flavored copy of the ISO template used to
// build the installer payload. It is a shallow copy so it starts from exactly
// the same package resolution (FullPkgList, FullPkgListBom, ResolvedKernelPackages)
// that the ISO template's own PreProcess already produced - no second
// download/resolution pass runs. Target.ImageType is flipped because several
// build-time code paths (notably first-boot partition auto-expand) gate on the
// literal "raw" value. ExtendLastPartitionToFillDisk is forced off: growth of
// the deployed disk is live-installer's job (its deploy path grows the last
// partition/filesystem to fill whatever target disk was selected, after the
// payload is written), so the flip must not also install the raw image's own
// first-boot auto-expand systemd service - the two would race over the same
// partition on the target's first boot.
func synthesizeRawTemplate(isoTemplate *config.ImageTemplate) *config.ImageTemplate {
	rawTemplate := *isoTemplate
	rawTemplate.Target.ImageType = "raw"
	rawTemplate.Disk.ExtendLastPartitionToFillDisk = false
	rawTemplate.SystemConfig.Users = clearInstallerEntrypointStartupScripts(isoTemplate.SystemConfig.Users)
	return &rawTemplate
}

// installerEntrypointStartupScripts are the repo's own installer entry
// points (config/general/isolinux/{attendedinstaller,unattendedinstaller}).
// A template built directly for imageType: iso commonly sets root's
// startupScript to one of these so the live ISO environment launches the
// installer, but the payload's deployed target never has installation media
// to mount - carrying either value over as the deployed root's login shell
// would break every subsequent root login on the target.
var installerEntrypointStartupScripts = map[string]bool{
	attendedInstallerStartupScript:   true,
	unattendedInstallerStartupScript: true,
}

// clearInstallerEntrypointStartupScripts returns a copy of users with root's
// startupScript cleared if it is one of the repo's own installer entry
// points; every other user, and every other startupScript value, is passed
// through unchanged, since it may be legitimate deployed-system config.
func clearInstallerEntrypointStartupScripts(users []config.UserConfig) []config.UserConfig {
	cleared := make([]config.UserConfig, len(users))
	copy(cleared, users)
	for i := range cleared {
		if cleared[i].Name == "root" && installerEntrypointStartupScripts[cleared[i].StartupScript] {
			cleared[i].StartupScript = ""
		}
	}
	return cleared
}

// buildPayloadRaw builds the installer payload raw image (via a raw-flavored
// copy of the ISO template) and stages its compressed form, manifest, and SBOM
// into ImageBuildDir/payload/ - a sibling of the ISO staging tree, not inside
// it, so the payload survives cleanIsoInstallRoot's rm -rf of the staging tree
// and is retained as a build artifact alongside the .raw and .iso. createIso
// grafts these files onto the ISO by pathspec rather than copying them into
// the staging tree. It must run before buildInitrd so the loop device used to
// build the payload is fully detached before ISO staging begins.
func (isoMaker *IsoMaker) buildPayloadRaw() error {
	if isoMaker.RawMaker == nil {
		rawTemplate := synthesizeRawTemplate(isoMaker.template)
		rawMaker, err := rawmaker.NewRawMaker(isoMaker.ChrootEnv, rawTemplate)
		if err != nil {
			return fmt.Errorf("failed to create payload raw maker: %w", err)
		}
		rawMaker.ImageConvert = payloadPassthroughConvert{}
		isoMaker.RawMaker = rawMaker
	}

	if err := isoMaker.RawMaker.Init(); err != nil {
		return fmt.Errorf("failed to initialize payload raw maker: %w", err)
	}
	if err := isoMaker.RawMaker.BuildRawImage(); err != nil {
		return fmt.Errorf("failed to build installer payload raw image: %w", err)
	}

	rawImagePath := isoMaker.RawMaker.GetRawImageFilePath()
	if rawImagePath == "" {
		return fmt.Errorf("payload raw maker did not report a built image path")
	}

	payloadDir := filepath.Join(isoMaker.ImageBuildDir, payloadDirName)
	if err := os.MkdirAll(payloadDir, 0755); err != nil {
		return fmt.Errorf("failed to create payload directory %s: %w", payloadDir, err)
	}

	// Snapshot the payload's own SBOM before buildInitrd's package resolution
	// overwrites the process-global manifest.DefaultSPDXFile in config.TempDir().
	if sbomPath, err := snapshotPayloadSBOM(payloadDir); err != nil {
		log.Warnf("Failed to snapshot installer payload SBOM: %v", err)
	} else {
		isoMaker.payloadSBOMPath = sbomPath
	}

	m, compressedPath, err := isopayload.Compress(rawImagePath, payloadDir, isoMaker.template.PayloadCompression())
	if err != nil {
		return fmt.Errorf("failed to compress installer payload: %w", err)
	}
	isoMaker.payloadCompressedPath = compressedPath

	manifestPath := filepath.Join(payloadDir, "payload.manifest.yaml")
	if err := isopayload.Write(m, manifestPath); err != nil {
		return fmt.Errorf("failed to write installer payload manifest: %w", err)
	}
	isoMaker.payloadManifestPath = manifestPath

	log.Infof("Installer payload staged: raw=%d bytes, compressed=%d bytes, compression=%s",
		m.RawBytes, m.CompressedBytes, m.Compression)

	return nil
}

// snapshotPayloadSBOM copies the just-built payload raw image's SBOM
// (config.TempDir()/spdx_manifest.json - the single process-global manifest
// writes to) into payloadDir as payload.sbom.json, before any later build
// stage (initrd package resolution) overwrites the same global path with an
// unrelated SBOM.
func snapshotPayloadSBOM(payloadDir string) (string, error) {
	srcSBOM := filepath.Join(config.TempDir(), manifest.DefaultSPDXFile)
	if _, err := os.Stat(srcSBOM); os.IsNotExist(err) {
		return "", fmt.Errorf("payload SBOM not found at %s", srcSBOM)
	}

	data, err := security.SafeReadFile(srcSBOM, security.RejectSymlinks)
	if err != nil {
		return "", fmt.Errorf("failed to read payload SBOM: %w", err)
	}

	dstSBOM := filepath.Join(payloadDir, "payload.sbom.json")
	if err := security.SafeWriteFile(dstSBOM, data, 0644, security.RejectSymlinks); err != nil {
		return "", fmt.Errorf("failed to write payload SBOM: %w", err)
	}
	return dstSBOM, nil
}

// payloadGraftPathspecs returns xorriso -graft-points pathspecs
// ("/payload/<basename>=<abs local path>") for every payload file that was
// staged by buildPayloadRaw, so createIso can include them in the ISO without
// copying them into the staging tree first.
func (isoMaker *IsoMaker) payloadGraftPathspecs() []string {
	var pathspecs []string
	for _, localPath := range []string{isoMaker.payloadCompressedPath, isoMaker.payloadManifestPath, isoMaker.payloadSBOMPath} {
		if localPath == "" {
			continue
		}
		pathspecs = append(pathspecs, fmt.Sprintf("/%s/%s=%s", payloadDirName, filepath.Base(localPath), localPath))
	}
	return pathspecs
}
