package overlay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/config/manifest"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageconvert"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageinspect"
	"github.com/open-edge-platform/image-composer-tool/internal/ospackage"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/display"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

// StageTiming records how long one overlay build stage took. The overlay pipeline
// does not run through the create-mode maker/chroot stages that populate the
// template's build timers, so it accumulates its own stage timings here for the
// caller to render (see Builder.Timings).
type StageTiming struct {
	Stage    string
	Duration time.Duration
}

// Builder drives an overlay-mode image build across the provider's three phases
// (preprocess, build, postprocess) while keeping a SINGLE baseline mount lifecycle
// open for their whole duration.
//
// The overlay stage primitives (Ingestor.WithBaseline, Inspector.WithMountedLayout)
// are closure-scoped: they tear their mounts down when their callback returns. That
// is the right shape for a single-shot caller, but the provider pipeline invokes
// PreProcess, BuildImage and PostProcess as separate calls, so the baseline must
// stay attached and mounted from preprocess through build and only be released in
// postprocess (or during failure unwind). Builder owns that explicit lifecycle: it
// acquires the loop device and mounts in Preprocess, reuses them in Build, and runs
// the cleanup chain (unmount then detach) in Postprocess regardless of outcome.
//
// Builder never mutates the user-provided baseline source (Ingestor copies it
// first), it is strictly additive (only ResolutionPlan.ToInstall is installed), and
// it never modifies the installed bootloader binary or the ESP (mounted read-only).
// Boot regeneration refreshes the initramfs and, on a GRUB2 baseline, the GRUB
// config (applying overlayPolicy.kernelCmdline and overlayPolicy.grubDefault) —
// both live on the writable root, never the ESP; grub-install is never run.
type Builder struct {
	template  *config.ImageTemplate
	ingestor  *Ingestor
	inspector *Inspector

	// State populated as the phases run; each is nil until its phase produces it.
	// Only the cross-phase state is retained: the inspected inventory and install
	// result are consumed within the phase that produces them, so they are local.
	ctx      *Context          // loop device + workspace baseline copy
	layout   *Layout           // mounted partition layout
	info     *BaselineInfo     // detected baseline OS/arch/package manager
	baseline []BaselinePackage // baseline package inventory for stats
	plan     *ResolutionPlan   // resolved additive package plan
	report   *PreflightReport  // preflight policy gate result

	// mountTeardown unmounts the layout (reverse order). It is set by Preprocess
	// and run once by Postprocess; nil before mounts exist or after teardown.
	mountTeardown func() error
	// preprocessed and built track how far the pipeline got, so Postprocess only
	// finalizes artifacts on a fully successful build and always runs cleanup.
	preprocessed bool
	built        bool

	// timings accumulates per-stage durations across the three phases, in the
	// order the stages ran, for the caller to render as a timing table.
	timings []StageTiming
}

// nowFn is the clock seam for stage timing, exposed as a package var so a test
// can override it for deterministic durations.
var nowFn = time.Now

// timeStage runs fn, recording its wall-clock duration under the given stage
// label (appended to b.timings in call order) regardless of whether fn errors,
// and returns fn's error. Stages that never run leave no row, so the rendered
// table reflects exactly the pipeline that executed.
func (b *Builder) timeStage(stage string, fn func() error) error {
	start := nowFn()
	err := fn()
	b.timings = append(b.timings, StageTiming{Stage: stage, Duration: nowFn().Sub(start)})
	return err
}

// Timings returns the per-stage durations recorded so far, in execution order.
// It returns a defensive copy so callers cannot mutate the Builder's internal
// timing state (e.g. by appending or sorting) and corrupt later reporting.
func (b *Builder) Timings() []StageTiming {
	out := make([]StageTiming, len(b.timings))
	copy(out, b.timings)
	return out
}

// Builder-stage indirection seams over the impure overlay stages so the phase
// orchestration is unit-testable without root, loop devices, mounts, or network.
// Tests override these; production uses the real stage functions.
var (
	builderAcquire     = func(ing *Ingestor) (*Context, error) { return ing.acquire() }
	builderMountLayout = func(insp *Inspector, loopDev string) (*Layout, func() error, error) {
		return insp.MountLayout(loopDev)
	}
	builderDetach      = func(ing *Ingestor, ctx *Context) error { return ing.detach(ctx) }
	builderRemoveCopy  = func(ing *Ingestor, ctx *Context) { ing.removeCopy(ctx, true) }
	builderDetectFn    = DetectBaseline
	builderResolveFn   = ResolveOverlayPackages
	builderPreflightFn = Preflight
	builderInstallFn   = InstallOverlayPackages
	builderConfigureFn = RunOverlayConfigurations
	builderRegenBootFn = RegenerateBoot
	builderGrubRegenFn = RegenerateGrub
	builderResizeFn    = ResizeBaseline
	builderSBOMFn      = generateOverlaySBOM
	builderEmitFn      = emitOverlayArtifact
	builderInspectFn   = inspectOverlayArtifact
	builderConvertFn   = func(path string, template *config.ImageTemplate) error {
		return imageconvert.NewImageConvert().ConvertImageFile(path, template)
	}
)

// NewBuilder constructs an overlay Builder for an overlay-mode template. It returns
// an error when the template is not in overlay mode or is missing its baseline
// source (the same gate as NewIngestor), so a create-mode build never reaches here.
func NewBuilder(template *config.ImageTemplate) (*Builder, error) {
	ingestor, err := NewIngestor(template)
	if err != nil {
		return nil, err
	}
	return &Builder{
		template:  template,
		ingestor:  ingestor,
		inspector: NewInspector(ingestor.workDir),
	}, nil
}

// Preprocess runs the overlay preprocess phase: it copies the baseline into the
// workspace, attaches it to a loop device, mounts the layout, inspects the baseline
// to extract its metadata and package inventory, resolves the requested overlay
// packages, and runs the preflight policy gate.
//
// The loop device and mounts it establishes are deliberately left open for the
// Build phase; on any error here it unwinds whatever it set up so no loop device or
// mount leaks, and a later Postprocess call becomes a no-op cleanup.
func (b *Builder) Preprocess() (err error) {
	if b.preprocessed {
		return fmt.Errorf("overlay build: Preprocess already ran")
	}

	// If anything below fails, unwind the partial mount/loop state immediately so
	// the lifecycle is not left half-open between phases.
	defer func() {
		if err != nil {
			b.cleanup()
		}
	}()

	var baseline []BaselinePackage
	if err := b.timeStage("Acquire & Mount Baseline", func() error {
		ctx, aerr := builderAcquire(b.ingestor)
		if aerr != nil {
			return fmt.Errorf("overlay preprocess: failed to acquire baseline: %w", aerr)
		}
		b.ctx = ctx

		layout, teardown, merr := builderMountLayout(b.inspector, ctx.LoopDevPath)
		if merr != nil {
			return fmt.Errorf("overlay preprocess: failed to mount baseline layout: %w", merr)
		}
		b.layout = layout
		b.mountTeardown = teardown
		return nil
	}); err != nil {
		return err
	}

	if err := b.timeStage("Inspect Baseline", func() error {
		info, base, derr := builderDetectFn(b.layout.RootMount, b.template.Target)
		if derr != nil {
			return fmt.Errorf("overlay preprocess: failed to inspect baseline: %w", derr)
		}
		b.info = info
		baseline = base
		b.baseline = base // Retain for stats computation
		return nil
	}); err != nil {
		return err
	}

	if err := b.timeStage("Resolve Packages", func() error {
		plan, rerr := builderResolveFn(b.template, b.info, baseline)
		if rerr != nil {
			return fmt.Errorf("overlay preprocess: dependency resolution failed: %w", rerr)
		}
		b.plan = plan
		return nil
	}); err != nil {
		return err
	}

	if err := b.timeStage("Preflight", func() error {
		report, perr := builderPreflightFn(b.info, baseline, b.plan, b.template.OverlayPolicy)
		// Preflight returns the report alongside a blocked error; retain it for
		// diagnostics even though installation will not proceed.
		b.report = report
		if perr != nil {
			return fmt.Errorf("overlay preprocess: preflight gate blocked the build: %w", perr)
		}
		return nil
	}); err != nil {
		return err
	}

	b.preprocessed = true
	return nil
}

// Build runs the overlay build phase against the already-mounted baseline: it
// performs an optional grow-only resize (first, so the added packages have room),
// installs the approved package plan, runs the template's configuration commands,
// regenerates the initramfs for any added packages, and finally — on a GRUB2
// baseline — applies overlayPolicy.kernelCmdline and overlayPolicy.grubDefault and
// regenerates the GRUB config (never the bootloader binary or the read-only ESP).
//
// It requires Preprocess to have succeeded; the mount lifecycle opened there is
// reused here and is not torn down until Postprocess.
func (b *Builder) Build() error {
	if !b.preprocessed {
		return fmt.Errorf("overlay build: Build called before a successful Preprocess")
	}
	if b.built {
		return fmt.Errorf("overlay build: Build already ran")
	}

	// Resize FIRST, before installing packages: the whole point of a grow is to
	// create the headroom the added packages need. Running it after install would
	// be too late — a near-full baseline fails the install with "no space left on
	// device" before the resize could ever make room. The root filesystem is
	// mounted (resize2fs/xfs_growfs both grow online) and the loop device is
	// already attached from Preprocess, so growing here is safe.
	if err := b.timeStage("Resize", func() error {
		if rerr := builderResizeFn(b.template, b.ctx, b.layout); rerr != nil {
			return fmt.Errorf("overlay build: resize failed: %w", rerr)
		}
		return nil
	}); err != nil {
		return err
	}

	var installed *InstallResult
	if err := b.timeStage("Install Packages", func() error {
		var ierr error
		installed, ierr = builderInstallFn(b.info, b.layout.RootMount, b.plan, b.report)
		if ierr != nil {
			return fmt.Errorf("overlay build: package installation failed: %w", ierr)
		}
		return nil
	}); err != nil {
		return err
	}

	// Run the template's arbitrary configuration commands AFTER the resolved package
	// install but BEFORE boot regeneration. This mirrors create mode's ordering
	// (addImageConfigs runs after installImagePkgs) and is deliberate: a
	// configuration command may itself install content that affects the initramfs
	// (e.g. an out-of-repo driver installed via wget+dpkg), so any resulting kernel
	// module or hook must be picked up by the subsequent Boot Regeneration.
	if err := b.timeStage("Configurations", func() error {
		if cerr := builderConfigureFn(b.template, b.layout.RootMount); cerr != nil {
			return fmt.Errorf("overlay build: configuration commands failed: %w", cerr)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := b.timeStage("Boot Regeneration", func() error {
		if berr := builderRegenBootFn(b.info, b.layout.RootMount, installed, b.plan); berr != nil {
			return fmt.Errorf("overlay build: boot regeneration failed: %w", berr)
		}
		return nil
	}); err != nil {
		return err
	}

	// Regenerate the GRUB2 config AFTER the initramfs: grub-mkconfig enumerates the
	// initrd images, so a newly added kernel's initramfs must already exist. This
	// stage also applies overlayPolicy.kernelCmdline (a full-line GRUB_CMDLINE_LINUX
	// replace) and overlayPolicy.grubDefault (a full-line GRUB_DEFAULT replace, to pin
	// the default boot entry) before regenerating. It never touches the bootloader
	// binary or the read-only ESP; the regenerated grub.cfg lives on the writable root.
	if err := b.timeStage("GRUB Regeneration", func() error {
		if gerr := builderGrubRegenFn(b.template, b.info, b.layout.RootMount); gerr != nil {
			return fmt.Errorf("overlay build: GRUB regeneration failed: %w", gerr)
		}
		return nil
	}); err != nil {
		return err
	}

	b.built = true
	return nil
}

// Postprocess finalizes the build and ALWAYS runs the cleanup chain. On a fully
// successful build (buildErr nil and Build completed) it embeds the overlay SBOM
// into the baseline while it is still mounted, then — after unmounting and
// detaching — emits the modified baseline as the final RAW artifact. On any failure
// it skips finalization but still unmounts and detaches, so a stage failure unwinds
// the whole lifecycle.
//
// buildErr is the error (if any) from the preceding phases; it is threaded in so a
// failed build still triggers the full cleanup chain.
func (b *Builder) Postprocess(buildErr error) (err error) {
	// Cleanup (unmount + detach) must run no matter what, so defer it before any
	// fallible finalization work below. Its error is SURFACED, not just logged: a
	// failed unmount or loop detach leaves a leaked mount/loop device that must not
	// be mistaken for a clean run. On the success path the explicit pre-emit
	// cleanupOnce below already released the baseline, so this deferred call is then
	// a no-op that returns nil; it only contributes an error on the failure/
	// incomplete path (or when it retries a previously-failed detach).
	//
	// build.go prioritizes the PostProcess error over the original buildErr it
	// passed in, so the three cases below preserve the root cause:
	//   - a finalization error is already being returned: keep it (the root cause)
	//     and only log the cleanup failure so the leaked device is still visible;
	//   - the build already failed (buildErr != nil): join buildErr with the cleanup
	//     failure so the caller sees BOTH the original build failure and the leak,
	//     rather than the cleanup error masking the root cause;
	//   - otherwise (clean build, cleanup itself failed): surface the cleanup error.
	defer func() {
		cerr := b.cleanupOnce()
		// Only a fully successful Postprocess moves the workspace baseline copy out
		// via emit; on any unsuccessful exit — a failed/incomplete build, or a
		// finalization failure (SBOM/emit) where err is set — the copy is left behind
		// and would otherwise accumulate across repeated builds (baseline images are
		// large). Remove it unconditionally in those cases, but only once the loop
		// device is released: a still-attached device (detach failed) references the
		// backing file, so unlinking it would hinder recovery. builderRemoveCopy
		// force-removes, ignoring debug retention, per the "remove on failure"
		// contract; on the clean path emit already moved the copy so this never runs.
		unsuccessful := buildErr != nil || !b.built || err != nil
		if unsuccessful && b.ctx != nil && b.ctx.LoopDevPath == "" {
			builderRemoveCopy(b.ingestor, b.ctx)
		}
		if cerr == nil {
			return
		}
		switch {
		case err != nil:
			// Log only the prior error's TYPE, not its full text: the finalization
			// error chain can transit provider internals whose messages may embed
			// sensitive data (matching the redaction convention in build.go). The
			// prior error is still returned in full to the caller; this warn line
			// exists only to make the additionally-leaked device visible.
			log.Warnf("Overlay postprocess cleanup failed (masked by prior error of type %T): %v", err, cerr)
		case buildErr != nil:
			err = errors.Join(buildErr, fmt.Errorf("overlay postprocess: cleanup failed: %w", cerr))
		default:
			err = fmt.Errorf("overlay postprocess: cleanup failed: %w", cerr)
		}
	}()

	if buildErr != nil || !b.built {
		// A failed or incomplete build: nothing to finalize; the deferred cleanup
		// still unmounts, detaches, and surfaces any error.
		return nil
	}

	// Embed the overlay SBOM into the baseline while the root is still mounted.
	if err := b.timeStage("Generate SBOM", func() error {
		if serr := builderSBOMFn(b.info, b.layout.RootMount, b.plan); serr != nil {
			return fmt.Errorf("overlay postprocess: SBOM generation failed: %w", serr)
		}
		return nil
	}); err != nil {
		return err
	}

	// Release the mounts and loop device before emitting the artifact: the final
	// image is the modified backing file, which must no longer be in use. The
	// release is timed together with the emit as the finalization stage.
	version := b.imageVersion()
	var artifact string
	if err := b.timeStage("Emit Artifact", func() error {
		if cerr := b.cleanupOnce(); cerr != nil {
			return fmt.Errorf("overlay postprocess: failed to release baseline before emit: %w", cerr)
		}
		emitted, eerr := builderEmitFn(b.template, b.ctx.BaselineCopyPath, version)
		if eerr != nil {
			return fmt.Errorf("overlay postprocess: failed to emit image artifact: %w", eerr)
		}
		artifact = emitted
		log.Infof("Overlay build complete: emitted %s", artifact)
		return nil
	}); err != nil {
		return err
	}

	// Inspect the emitted image unless the operator disabled it (--no-inspect).
	// The inspection is a post-build report on the finished artifact — distinct
	// from the mandatory baseline inspection in Preprocess that drives package
	// resolution — so it runs against the already-released RAW file here.
	if b.template.InspectEnabled {
		if err := b.timeStage("Inspect Image", func() error {
			if ierr := builderInspectFn(artifact); ierr != nil {
				return fmt.Errorf("overlay postprocess: image inspection failed: %w", ierr)
			}
			return nil
		}); err != nil {
			return err
		}
	} else {
		log.Infof("Overlay postprocess: image inspection disabled (--no-inspect); skipping")
	}

	// Convert the emitted RAW into the disk.artifacts output formats (qcow2, vhd,
	// vhdx, vmdk, vdi) and apply any configured compression, so overlay mode emits
	// every output format create/build mode supports. This reuses the same
	// converter the create-mode rawmaker runs, keyed on template disk.artifacts;
	// it is a no-op when disk.artifacts is empty or lists only raw, so plain RAW
	// overlay builds are unchanged. It runs AFTER the RAW inspection above because
	// a request that omits raw deletes the RAW file as part of conversion.
	if err := b.timeStage("Convert Artifacts", func() error {
		if cerr := builderConvertFn(artifact, b.template); cerr != nil {
			return fmt.Errorf("overlay postprocess: failed to convert image artifact: %w", cerr)
		}
		return nil
	}); err != nil {
		return err
	}

	// Display package statistics showing what was added/upgraded vs unchanged
	stats := ComputePackageStats(b.baseline, b.plan)
	PrintPackageStats(stats)

	// In debug mode, also print the full unchanged package list
	if config.IsDebugMode() {
		PrintVerboseUnchangedPackages(stats)
	}

	// Print the same success/artifact summary box the create-mode makers emit, so
	// overlay builds also report the generated artifacts. The build directory is the
	// parent of the emitted artifact; emitOverlayArtifact has placed the .raw there
	// (and the conversion above may have added qcow2/vhd/... alongside or in place of
	// it), plus the SBOM sidecar when its best-effort copy succeeded. The summary
	// lists whatever files are actually present, so it reflects the converted formats
	// and a missing sidecar simply isn't shown.
	display.PrintImageDirectorySummary(filepath.Dir(artifact), overlayArtifactTypeLabel(b.template))

	return nil
}

// overlayArtifactTypeLabel derives the "Image Type" label for the success
// summary from the template's disk.artifacts, so the box reflects the formats
// actually requested (e.g. "QCOW2" or "QCOW2, RAW") rather than always claiming
// "RAW" after the Convert Artifacts stage may have replaced the .raw file. It
// defaults to "RAW" when no artifacts are declared, matching the plain overlay
// build that emits only the RAW image.
func overlayArtifactTypeLabel(t *config.ImageTemplate) string {
	artifacts := t.GetDiskConfig().Artifacts
	if len(artifacts) == 0 {
		return "RAW"
	}
	types := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		if a.Type == "" {
			continue
		}
		types = append(types, strings.ToUpper(a.Type))
	}
	if len(types) == 0 {
		return "RAW"
	}
	return strings.Join(types, ", ")
}

// cleanup runs the mount/loop teardown chain. It is idempotent: the mount teardown
// clears itself after running and detach is a no-op once the loop device is gone,
// so Postprocess's deferred cleanup is harmless after an explicit cleanupOnce.
func (b *Builder) cleanup() {
	if err := b.cleanupOnce(); err != nil {
		// Already logged in detail by the teardown; record at warn for the unwind path.
		log.Warnf("Overlay cleanup: %v", err)
	}
}

// cleanupOnce unmounts the layout (reverse order) and detaches the loop device,
// returning the combined unmount and detach errors (via errors.Join, so both are
// surfaced rather than silently dropping the detach failure). A leaked loop
// device is thereby never mistaken for a clean build. It is safe to call
// multiple times.
func (b *Builder) cleanupOnce() error {
	var umountErr, detachErr error
	if b.mountTeardown != nil {
		umountErr = b.mountTeardown()
		b.mountTeardown = nil
	}
	if b.ctx != nil && b.ctx.LoopDevPath != "" {
		detachErr = builderDetach(b.ingestor, b.ctx)
		if detachErr == nil {
			// Clear the loop path ONLY after a successful detach, so a second
			// cleanup does not re-detach an already-released device (idempotence).
			// On a detach failure the path is deliberately retained so a later
			// cleanup (or a subsequent Postprocess) retries the detach and can
			// re-surface the failure rather than mistaking a leaked loop device
			// for a released one.
			b.ctx.LoopDevPath = ""
		}
	}
	return errors.Join(umountErr, detachErr)
}

// imageVersion derives the artifact version tag. The overlaid image fundamentally
// carries the baseline's version, so the detected baseline VERSION_ID is preferred;
// a template-specified image version overrides it, and "overlay" is the last resort.
func (b *Builder) imageVersion() string {
	if v := strings.TrimSpace(b.template.Image.Version); v != "" {
		return v
	}
	if b.info != nil {
		if v := strings.TrimSpace(b.info.Version); v != "" {
			return v
		}
	}
	return "overlay"
}

// generateOverlaySBOM updates the baseline's embedded SPDX SBOM at
// /usr/share/sbom so it reflects the COMPLETE inventory of the overlaid image —
// the baseline packages the image inherited plus the packages the overlay
// contributed (ToInstall: newly added packages, and in additive-and-upgrade mode
// baseline packages upgraded to a newer version).
//
// The overlay image is a copy of the baseline, so it already carries the
// baseline's full SBOM. This reads that inherited document, merges the
// contributed packages into it (a name already present is replaced — an upgrade;
// a new name is appended — an addition), and writes the merged document back
// UNDER THE BASELINE'S OWN FILENAME so it replaces the inherited SBOM rather than
// dropping a second, delta-only file beside it. That prevents SBOM consumers
// (compare, CVE scanners) from reading a misleading partial inventory.
//
// When the baseline carries no readable SBOM, it falls back to writing just the
// contributed packages so the image still gets a manifest.
func generateOverlaySBOM(info *BaselineInfo, rootMount string, plan *ResolutionPlan) error {
	if plan == nil {
		return nil
	}
	pkgType := pkgTypeDeb
	if info != nil && info.PackageType != "" {
		pkgType = info.PackageType
	}

	pkgs := make([]ospackage.PackageInfo, 0, len(plan.ToInstall))
	for _, rp := range plan.ToInstall {
		pkgs = append(pkgs, ospackage.PackageInfo{
			Name:    rp.Name,
			PkgName: rp.Name,
			Type:    pkgType,
			Version: rp.Version,
			Arch:    rp.Arch,
			URL:     rp.URL,
		})
	}

	// Locate the SBOM the image inherited from the baseline. Its filename is
	// build-specific (create mode timestamps it), so discover it rather than
	// assume a fixed name.
	baselineSBOMName, baselineSBOMData, found := readBaselineSBOM(rootMount)
	if !found {
		log.Warnf("Overlay SBOM: no baseline SBOM found at %s; writing overlay-contributed packages only", manifest.ImageSBOMPath)
		return writeOverlaySBOMToChroot(pkgs, manifest.DefaultSPDXFile, rootMount)
	}

	// Stage and embed the merged SBOM under the baseline's OWN filename so it
	// replaces the inherited file in place (same path + name) rather than
	// shadowing it with a second, delta-only file. DefaultSPDXFile is set (not
	// restored) so the later sidecar copy in emitOverlayArtifact — which keys off
	// this variable — packages the same merged manifest. This mirrors create
	// mode's generateSBOM, which likewise assigns DefaultSPDXFile.
	manifest.DefaultSPDXFile = baselineSBOMName
	tempSBOM := filepath.Join(config.TempDir(), baselineSBOMName)
	if err := manifest.WriteMergedSPDXToFile(baselineSBOMData, pkgs, tempSBOM); err != nil {
		// A malformed baseline SBOM must not fail the build; fall back to the
		// delta so the image still gets a manifest.
		log.Warnf("Overlay SBOM: merging into baseline SBOM %s failed (%v); writing overlay-contributed packages only", baselineSBOMName, err)
		return writeOverlaySBOMToChroot(pkgs, baselineSBOMName, rootMount)
	}

	if err := manifest.CopySBOMToChroot(rootMount); err != nil {
		return fmt.Errorf("embedding merged overlay SBOM into baseline: %w", err)
	}
	log.Infof("Overlay SBOM merged: %d contributed package(s) folded into the baseline inventory at %s/%s",
		len(pkgs), manifest.ImageSBOMPath, baselineSBOMName)
	return nil
}

// readBaselineSBOM finds and reads the SBOM the overlay image inherited from the
// baseline under <rootMount>/usr/share/sbom. It returns the file's base name, its
// bytes, and whether a usable SBOM was found. Selection mirrors the inspector's
// picker: an "spdx_manifest*" JSON is preferred, otherwise the first JSON file.
func readBaselineSBOM(rootMount string) (string, []byte, bool) {
	sbomDir := filepath.Join(rootMount, strings.TrimPrefix(manifest.ImageSBOMPath, "/"))
	entries, err := os.ReadDir(sbomDir)
	if err != nil {
		return "", nil, false
	}

	name, ok := pickBaselineSBOMName(entries)
	if !ok {
		return "", nil, false
	}

	data, err := os.ReadFile(filepath.Join(sbomDir, name))
	if err != nil {
		return "", nil, false
	}
	return name, data, true
}

// pickBaselineSBOMName selects the SBOM file among directory entries, preferring
// a name starting with "spdx_manifest" and falling back to any ".json" file. Both
// tiers are chosen deterministically (lexicographically smallest) so the pick is
// stable across runs.
func pickBaselineSBOMName(entries []os.DirEntry) (string, bool) {
	var preferred, fallback string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".json") {
			continue
		}
		if strings.HasPrefix(lower, "spdx_manifest") {
			if preferred == "" || name < preferred {
				preferred = name
			}
			continue
		}
		if fallback == "" || name < fallback {
			fallback = name
		}
	}
	if preferred != "" {
		return preferred, true
	}
	if fallback != "" {
		return fallback, true
	}
	return "", false
}

// writeOverlaySBOMToChroot stages a from-scratch SBOM of pkgs under sbomName and
// embeds it into the mounted root at /usr/share/sbom/<sbomName>. It backs the
// fallback paths where no baseline SBOM is available to merge into. It sets (not
// restores) manifest.DefaultSPDXFile so both CopySBOMToChroot here and the later
// sidecar copy — which key off that variable — use sbomName.
func writeOverlaySBOMToChroot(pkgs []ospackage.PackageInfo, sbomName, rootMount string) error {
	manifest.DefaultSPDXFile = sbomName
	tempSBOM := filepath.Join(config.TempDir(), sbomName)
	if err := manifest.WriteSPDXToFile(pkgs, tempSBOM); err != nil {
		return fmt.Errorf("writing overlay SBOM: %w", err)
	}
	if err := manifest.CopySBOMToChroot(rootMount); err != nil {
		return fmt.Errorf("embedding overlay SBOM into baseline: %w", err)
	}
	log.Infof("Overlay SBOM generated for %d contributed package(s) (added or upgraded)", len(pkgs))
	return nil
}

// emitOverlayArtifact moves the modified baseline copy into the image build
// directory as "<name>-<version>.raw" and copies the SBOM sidecar alongside it,
// mirroring the create-mode RAW artifact naming. It returns the final image path.
//
// The loop device must already be detached (the backing file is moved, not the
// live device).
func emitOverlayArtifact(template *config.ImageTemplate, copyPath, version string) (string, error) {
	if strings.TrimSpace(copyPath) == "" {
		return "", fmt.Errorf("overlay emit: baseline copy path is empty")
	}
	buildDir, err := overlayImageBuildDir(template)
	if err != nil {
		return "", err
	}
	// Create the build directory in-process rather than via `bash -c "mkdir -p"`:
	// buildDir derives from user-controlled template fields, so a shell command
	// would be exposed to metacharacter/whitespace parsing. The workspace copy is
	// user-owned, so no sudo is needed here. Mode 0700 matches the create-mode
	// makers (e.g. rawmaker) and keeps build artifacts / intermediate data private.
	if err := os.MkdirAll(buildDir, 0o700); err != nil {
		return "", fmt.Errorf("overlay emit: failed to create image build directory %s: %w", buildDir, err)
	}

	finalPath := filepath.Join(buildDir, fmt.Sprintf("%s-%s.raw", template.GetImageName(), version))
	// Move the finished baseline into place without a shell (same rationale as the
	// mkdir above). os.Rename covers the common same-filesystem case; fall back to
	// a copy+remove when the workspace and build directory are on different mounts
	// (os.Rename returns EXDEV), which a cross-device `mv` would otherwise handle.
	if err := moveFile(copyPath, finalPath); err != nil {
		return "", fmt.Errorf("overlay emit: failed to move %s to %s: %w", copyPath, finalPath, err)
	}

	// Best-effort SBOM sidecar next to the artifact; absence is not fatal.
	if err := manifest.CopySBOMToImageBuildDir(buildDir); err != nil {
		log.Warnf("Overlay emit: failed to copy SBOM sidecar to %s: %v", buildDir, err)
	}
	return finalPath, nil
}

// inspectOverlayArtifact runs a post-build inspection of the emitted RAW image and
// renders the summary to the build log. It reuses the same diskfs inspector the
// standalone `inspect` command uses, so it needs no loop device or root: the image
// is already released and inspected purely in userspace. Inspection is a reporting
// step; a failure here is surfaced by the caller so a broken emitted image does not
// pass silently.
func inspectOverlayArtifact(artifactPath string) error {
	if strings.TrimSpace(artifactPath) == "" {
		return fmt.Errorf("overlay inspect: artifact path is empty")
	}
	// Enable SBOM inspection (second arg) so the reported summary includes the
	// image's SBOM, matching the documented overlay-inspection behavior and the
	// standalone compare/inspect commands. Hashing stays off: it is expensive and
	// not needed for this reporting step.
	summary, err := imageinspect.NewDiskfsInspectorWithOptions(false, true).Inspect(artifactPath)
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", artifactPath, err)
	}

	var buf strings.Builder
	if rerr := imageinspect.RenderSummaryText(&buf, summary, imageinspect.TextOptions{}); rerr != nil {
		return fmt.Errorf("rendering inspection summary for %s: %w", artifactPath, rerr)
	}
	log.Infof("Overlay image inspection:\n%s", buf.String())
	return nil
}

// moveFile moves src to dst without invoking a shell. It first attempts an atomic
// os.Rename; if that fails with a cross-device link error (src and dst live on
// different filesystems, which os.Rename cannot span), it falls back to a
// streaming copy followed by removing the source, mirroring what `mv` would do.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	// Cross-device: copy then remove the source.
	if err := copyLocalFile(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("failed to remove source %s after cross-device move: %w", src, err)
	}
	return nil
}

// overlayImageBuildDir returns the image build directory for the template, matching
// the create-mode layout (<workDir>/<providerId>/imagebuild/<systemConfigName>).
func overlayImageBuildDir(template *config.ImageTemplate) (string, error) {
	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return "", fmt.Errorf("overlay emit: failed to resolve work directory: %w", err)
	}
	// Derive the same safe/defaulted system-config segment NewIngestor uses for the
	// workspace. Overlay mode allows templates to omit systemConfig.name, so using
	// GetSystemConfigName() directly here would place artifacts under an empty
	// segment (.../imagebuild/), colliding with other builds and diverging from the
	// overlay workspace layout.
	sysConfigName, err := overlaySysConfigName(template)
	if err != nil {
		return "", fmt.Errorf("overlay emit: %w", err)
	}
	providerID := system.GetProviderId(template.Target.OS, template.Target.Dist, template.Target.Arch)
	return filepath.Join(globalWorkDir, providerID, "imagebuild", sysConfigName), nil
}
