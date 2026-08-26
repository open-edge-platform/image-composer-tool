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
	"github.com/open-edge-platform/image-composer-tool/internal/utils/security"
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
	// emittedArtifact is the output-directory path of the emitted RAW image, set
	// once the Emit stage moves the image into place. It is retained so a failure
	// in a LATER Postprocess stage (inspect, convert) can remove the partial output
	// — nothing from an unsuccessful build must be left in the output directory.
	emittedArtifact string
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
	builderDetach          = func(ing *Ingestor, ctx *Context) error { return ing.detach(ctx) }
	builderRemoveCopy      = func(ing *Ingestor, ctx *Context) { ing.removeCopy(ctx, true) }
	builderDetectFn        = DetectBaseline
	builderValidateUsersFn = ValidateOverlayUsers
	builderResolveFn       = ResolveOverlayPackages
	builderPreflightFn     = Preflight
	builderInstallFn       = InstallOverlayPackages
	builderCreateUsersFn   = RunOverlayUsers
	builderConfigureFn     = RunOverlayConfigurations
	builderRegenBootFn     = RegenerateBoot
	builderGrubRegenFn     = RegenerateGrub
	builderAddFilesFn      = RunOverlayAdditionalFiles
	builderResizeFn        = ResizeBaseline
	builderSBOMFn          = generateOverlaySBOM
	builderEmitFn          = emitOverlayArtifact
	builderInspectFn       = inspectOverlayArtifact
	builderConvertFn       = func(path string, template *config.ImageTemplate) error {
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

	// Reject a requested user that already exists in the baseline BEFORE any
	// mutation (resize, install) occurs. The baseline is mounted (from the stage
	// above) so its /etc/passwd is readable; failing here unwinds the mount via the
	// deferred cleanup and stops the build early, per the fail-fast guarantee.
	if err := b.timeStage("Validate Users", func() error {
		if verr := builderValidateUsersFn(b.template, b.layout.RootMount); verr != nil {
			return fmt.Errorf("overlay preprocess: user validation failed: %w", verr)
		}
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
// regenerates the initramfs for any added packages, then — on a GRUB2 baseline —
// applies overlayPolicy.kernelCmdline and overlayPolicy.grubDefault and regenerates
// the GRUB config (never the bootloader binary or the read-only ESP), and finally
// copies the template's additionalFiles into the baseline root (last, so a
// user-supplied boot artifact is not clobbered by the preceding regeneration).
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

	// Create the template's users AFTER the package install but BEFORE the
	// configuration commands, so a configuration command can reference a
	// newly-created account (e.g. chown, install a per-user unit). This mirrors
	// create mode, where user provisioning precedes the arbitrary config step.
	// Base-image conflicts were already rejected in Preprocess ("Validate Users").
	if err := b.timeStage("Users", func() error {
		if uerr := builderCreateUsersFn(b.template, b.layout.RootMount); uerr != nil {
			return fmt.Errorf("overlay build: user creation failed: %w", uerr)
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

	// Copy the pre-initramfs additionalFiles BEFORE boot regeneration, so files the
	// initramfs generator consumes (a dracut module, an initramfs-tools hook) are in
	// place when the initramfs is rebuilt below. Only entries marked
	// stage: pre-initramfs are copied here; the default (unmarked) entries copy at
	// the end, after regeneration (see the "Additional Files" stage below).
	if err := b.timeStage("Additional Files (pre-initramfs)", func() error {
		if aerr := builderAddFilesFn(b.template, b.layout.RootMount, config.AdditionalFileStagePreInitramfs); aerr != nil {
			return fmt.Errorf("overlay build: pre-initramfs additional files copy failed: %w", aerr)
		}
		return nil
	}); err != nil {
		return err
	}

	// A pre-initramfs additionalFiles entry (a dracut module / initramfs-tools hook)
	// forces initramfs regeneration: it is boot-relevant content the package
	// manifests do not reflect, so without this the regen gate would skip it when no
	// boot-relevant PACKAGE was installed and the file would never be baked in.
	forceRegen := b.hasPreInitramfsAdditionalFiles()
	if err := b.timeStage("Boot Regeneration", func() error {
		if berr := builderRegenBootFn(b.template, b.info, b.layout.RootMount, installed, b.plan, forceRegen); berr != nil {
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

	// Copy the default-stage additionalFiles into the baseline root LAST, after both
	// regeneration stages. This ordering lets a user-supplied boot artifact (e.g. a
	// prebuilt /boot/initrd.img) survive: dropping it before Boot Regeneration would
	// let update-initramfs overwrite it. Files that must instead be consumed BY
	// regeneration are marked stage: pre-initramfs and were copied earlier, above.
	if err := b.timeStage("Additional Files", func() error {
		if aerr := builderAddFilesFn(b.template, b.layout.RootMount, config.AdditionalFileStageDefault); aerr != nil {
			return fmt.Errorf("overlay build: additional files copy failed: %w", aerr)
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
		// The teardown chain must never itself crash the process: a panic here (e.g.
		// an unexpected nil deref in a stage seam that already opened mounts) would
		// otherwise skip the loop/mount release. Recover it, convert it to an error,
		// and still run the release below. The recovered panic is surfaced (or joined)
		// so it is never silently swallowed.
		if r := recover(); r != nil {
			log.Errorf("Overlay postprocess: recovered from panic during finalization: %v", r)
			perr := fmt.Errorf("overlay postprocess: panic during finalization: %v", r)
			if err != nil {
				err = errors.Join(err, perr)
			} else {
				err = perr
			}
		}

		cerr := b.cleanupOnce()
		// Only a fully successful Postprocess moves the workspace baseline copy out
		// via emit; on any unsuccessful exit — a failed/incomplete build, or a
		// finalization failure (SBOM/emit) where err is set — the copy is left behind
		// and would otherwise accumulate across repeated builds (baseline images are
		// large). Remove it unconditionally in those cases, but only once BOTH the loop
		// device AND every mount have been released. A still-attached device references
		// the backing file directly; and a mount can outlive the loop device — a failed
		// unmount followed by a successful `losetup -d` leaves the loop autoclearing
		// while the mount stays live, so LoopDevPath is cleared but the file is still in
		// use. Unlinking it in either case would leave a live mount backed by a deleted
		// file and hinder recovery. cleanupOnce clears mountTeardown only on a fully
		// successful unmount, so mountTeardown==nil is the "all mounts released" signal.
		// builderRemoveCopy force-removes, ignoring debug retention, per the "remove on
		// failure" contract; on the clean path emit already moved the copy so this never runs.
		unsuccessful := buildErr != nil || !b.built || err != nil
		released := b.ctx != nil && b.ctx.LoopDevPath == "" && b.mountTeardown == nil
		if unsuccessful && released {
			builderRemoveCopy(b.ingestor, b.ctx)
		} else if unsuccessful && b.ctx != nil {
			// The copy is retained (a still-attached loop device or a still-live mount
			// references it), so log the workspace path for debugging rather than leaving
			// it silently behind.
			log.Warnf("Overlay postprocess: retaining workspace baseline copy for debugging "+
				"(loop device %q / mounts could not be fully released): %s", b.ctx.LoopDevPath, b.ctx.BaselineCopyPath)
		}
		// If the artifact was already emitted to the output directory but a LATER
		// finalization stage (inspect/convert) then failed, remove the partial
		// output so nothing from an unsuccessful build leaks to the output directory.
		// Only fires when err != nil AND emit had run (emittedArtifact set); on the
		// clean path err is nil so the finished artifact is kept.
		if err != nil && b.emittedArtifact != "" {
			removeEmittedArtifacts(b.emittedArtifact, b.template)
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

	// Generate the overlay SBOMs while the root is still mounted: this embeds the
	// complete inventory into the image and stages both the delta and complete SBOM
	// documents in the temp dir for the emit stage to place beside the artifact.
	var sbomArtifacts *overlaySBOMArtifacts
	if err := b.timeStage("Generate SBOM", func() error {
		staged, serr := builderSBOMFn(b.template, b.info, b.layout.RootMount, b.plan, b.report)
		if serr != nil {
			return fmt.Errorf("overlay postprocess: SBOM generation failed: %w", serr)
		}
		sbomArtifacts = staged
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
		emitted, eerr := builderEmitFn(b.template, b.ctx.BaselineCopyPath, version, sbomArtifacts)
		if eerr != nil {
			return fmt.Errorf("overlay postprocess: failed to emit image artifact: %w", eerr)
		}
		artifact = emitted
		// Record the emitted path so the deferred cleanup can remove it (and its
		// sidecars) if a later stage — inspect or convert — fails: a partial output
		// must never be left in the build directory.
		b.emittedArtifact = emitted
		log.Infof("Overlay build complete: emitted %s", artifact)
		return nil
	}); err != nil {
		return err
	}

	// Inspect the emitted image when the operator opted in (--inspect). The
	// inspection is a post-build report on the finished artifact — distinct from
	// the mandatory baseline inspection in Preprocess that drives package
	// resolution — so it runs against the already-released RAW file here. The
	// report is written to a sidecar artifact file, not the console.
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
		log.Debugf("Overlay postprocess: image inspection not requested (--inspect off); skipping")
		// A rebuild of the same name/version with --inspect now OFF must not leave an
		// earlier build's inspection report at the deterministic path, or the artifact
		// summary would present that stale report as describing the new image.
		removeStaleArtifact(overlayInspectReportPath(artifact))
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
	stats := ComputePackageStats(b.baseline, b.plan, b.report)
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
//
// Each operation is individually panic-safe: a panic in mountTeardown is recovered
// and converted to an error so the loop-device detach STILL runs, and a panic in
// detach is likewise converted to an error. Neither can crash the process or skip
// the other release step — the cleanup-never-panics guarantee holds even for the
// teardown chain itself, not just the finalization stages around it.
func (b *Builder) cleanupOnce() error {
	var umountErr, detachErr error
	if b.mountTeardown != nil {
		umountErr = callWithRecover("overlay cleanup: unmount", b.mountTeardown)
		// Clear the teardown ONLY when it fully succeeded, mirroring the loop-detach
		// handling below. On failure it is retained so a later cleanup (or the deferred
		// second cleanup in Postprocess) re-runs it — the teardown closure itself now
		// retries only the still-mounted points. Discarding it here would make the
		// mount teardown unretryable and leak any point that failed to unmount, even
		// though the detach could still be retried — defeating the full-unwind
		// guarantee. A panic is treated as failure too (retain and retry).
		if umountErr == nil {
			b.mountTeardown = nil
		}
	}
	if b.ctx != nil && b.ctx.LoopDevPath != "" {
		detachErr = callWithRecover("overlay cleanup: loop detach", func() error {
			return builderDetach(b.ingestor, b.ctx)
		})
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

// callWithRecover runs fn and returns its error, converting a panic into an error
// (tagged with label) so a panicking cleanup step neither crashes the process nor
// skips the steps that follow it.
func callWithRecover(label string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("%s: recovered from panic: %v", label, r)
			err = fmt.Errorf("%s: panic: %v", label, r)
		}
	}()
	return fn()
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

// hasPreInitramfsAdditionalFiles reports whether the template declares any
// additionalFiles entry staged pre-initramfs (after path resolution / dropping of
// missing entries). It drives the boot-regeneration force flag so such a file is
// baked into the initramfs even when no boot-relevant package was installed.
func (b *Builder) hasPreInitramfsAdditionalFiles() bool {
	for _, f := range b.template.GetAdditionalFileInfo() {
		if additionalFileStage(f) == config.AdditionalFileStagePreInitramfs {
			return true
		}
	}
	return false
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
// The base SBOM to merge into is resolved in this order:
//  1. An externally-supplied SBOM at baseline.source.sbomPath, when set and
//     readable — this lets a caller provide a full baseline inventory even when
//     the baseline image does not embed one.
//  2. Otherwise the SBOM the overlay image inherited from the baseline at
//     /usr/share/sbom (discovered by name).
//
// When neither yields a readable/valid base SBOM, it falls back to writing just
// the contributed packages so the image still gets a manifest. A missing or
// malformed base SBOM (external or inherited) never fails the build.
func generateOverlaySBOM(template *config.ImageTemplate, info *BaselineInfo, rootMount string, plan *ResolutionPlan, report *PreflightReport) (*overlaySBOMArtifacts, error) {
	if plan == nil {
		return nil, nil
	}

	// Stage both the delta and complete SBOMs to the temp dir (no sudo). This also
	// sets manifest.DefaultSPDXFile to the complete SBOM's name so CopySBOMToChroot
	// below embeds the complete inventory.
	artifacts, err := stageOverlaySBOMArtifacts(template, info, rootMount, plan, report)
	if err != nil {
		return nil, err
	}

	// Embed the COMPLETE SBOM into the image at /usr/share/sbom so the in-image
	// manifest reflects the full final inventory (baseline + overlay), replacing the
	// inherited file in place. CopySBOMToChroot keys off manifest.DefaultSPDXFile,
	// which stageOverlaySBOMArtifacts set to the complete SBOM's name.
	//
	// The baseline is UNTRUSTED: its `cp`-based embed follows destination symlinks, so
	// a baseline that made /usr/share/sbom (or the manifest file, or any ancestor) a
	// symlink to a host path would cause the elevated (sudo) copy to overwrite that
	// host file. Reject a symlinked destination chain within the mount before copying.
	embedDst := filepath.Join(manifest.ImageSBOMPath, manifest.DefaultSPDXFile)
	if err := assertNoSymlinkInChrootPath(rootMount, embedDst); err != nil {
		return nil, fmt.Errorf("refusing to embed overlay SBOM: unsafe destination in baseline: %w", err)
	}
	if err := manifest.CopySBOMToChroot(rootMount); err != nil {
		return nil, fmt.Errorf("embedding overlay SBOM into baseline: %w", err)
	}
	log.Infof("Overlay SBOM: embedded complete inventory into the image at %s/%s", manifest.ImageSBOMPath, manifest.DefaultSPDXFile)
	return artifacts, nil
}

// assertNoSymlinkInChrootPath rejects a destination whose path inside the mounted
// baseline traverses a symlink at ANY component — every ancestor directory from
// rootMount down to (and including) the final element, when it exists. The baseline
// is untrusted and the SBOM embed copies under sudo following destination symlinks,
// so a symlinked ancestor (e.g. /usr/share -> /etc on the host) or a symlinked
// target file would let the copy escape the mount and overwrite a host path. Each
// component is Lstat'd (no resolution) so a symlink is detected rather than
// followed; a not-yet-existing component (the copy's mkdir -p / cp will create it)
// is fine and stops the walk. It is overlay-specific: create-mode makers build their
// own trusted root and use manifest.CopySBOMToChroot directly.
func assertNoSymlinkInChrootPath(rootMount, relPath string) error {
	// Walk component-by-component from rootMount, appending one path element at a
	// time, so an intermediate symlink is caught before it is traversed.
	current := rootMount
	for _, part := range strings.Split(filepath.Clean(relPath), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		fi, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				// This and every deeper component do not exist yet; nothing to traverse.
				return nil
			}
			return fmt.Errorf("checking %s: %w", current, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %s is a symlink", current)
		}
	}
	return nil
}

// safeChrootDest resolves an in-image relative path against rootMount and confirms
// the result stays WITHIN rootMount, rejecting a `..`-traversal that would escape it
// (e.g. relPath "../../etc/passwd"). It returns the confined absolute destination.
// It complements assertNoSymlinkInChrootPath: this blocks lexical traversal in the
// requested path, that blocks symlink redirection through an existing chain. The
// baseline root is untrusted and copies run under sudo, so both guards run before a
// destination inside the mount is written.
func safeChrootDest(rootMount, relPath string) (string, error) {
	// A cleaned rootMount plus the joined+cleaned destination; filepath.Join cleans
	// the result, collapsing any ".." segments.
	cleanRoot := filepath.Clean(rootMount)
	dst := filepath.Join(cleanRoot, relPath)
	// The destination must be rootMount itself or a path strictly under it. Compare on
	// a trailing-separator boundary so a sibling like "<root>-evil" cannot masquerade
	// as being under "<root>".
	if dst != cleanRoot && !strings.HasPrefix(dst, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the image root")
	}
	return dst, nil
}

// overlaySBOMArtifacts holds the temp-staged paths of the two SBOM documents an
// overlay build produces: the delta (only the overlay-contributed packages) and
// the complete (the full final inventory = base + delta). Both are copied to the
// build output directory at emit time under deterministic names.
type overlaySBOMArtifacts struct {
	deltaPath    string
	completePath string
	// completeIsDeltaOnly is true when no base SBOM was available, so the complete
	// document degraded to exactly the delta contents. In that case the build-dir
	// ".complete.spdx.json" sidecar is NOT emitted (it would be byte-identical to
	// the delta and its "complete" name would misleadingly imply a full inventory);
	// the in-image /usr/share/sbom manifest is still written from completePath.
	completeIsDeltaOnly bool
}

// contributedPackages converts the plan's added/upgraded packages (the overlay
// delta) into SBOM package records, carrying the enriched repo metadata so overlay
// entries reach the SBOM writer with the same completeness as baseline-derived ones.
func contributedPackages(info *BaselineInfo, plan *ResolutionPlan) []ospackage.PackageInfo {
	pkgType := pkgTypeDeb
	if info != nil && info.PackageType != "" {
		pkgType = info.PackageType
	}
	pkgs := make([]ospackage.PackageInfo, 0, len(plan.ToInstall))
	for _, rp := range plan.ToInstall {
		// Prefer the resolved package's own type; fall back to the baseline
		// package family only when the resolver did not record one.
		t := rp.Type
		if t == "" {
			t = pkgType
		}
		pkgs = append(pkgs, ospackage.PackageInfo{
			Name:        rp.Name,
			PkgName:     rp.Name,
			Type:        t,
			Version:     rp.Version,
			Arch:        rp.Arch,
			URL:         rp.URL,
			Description: rp.Description,
			Origin:      rp.Origin,
			License:     rp.License,
			Checksums:   rp.Checksums,
		})
	}
	return pkgs
}

// stageOverlaySBOMArtifacts writes the delta and complete SBOM documents to the
// temp directory and returns their paths. It is pure with respect to the image
// (it stages files and reads the base SBOM only — no sudo-backed chroot copy), so
// it is unit-testable.
//
//   - delta: an SPDX doc of ONLY the overlay-contributed packages (added and, in
//     additive-and-upgrade mode, upgraded). Always written.
//   - complete: the full final inventory. When a base SBOM (external override or
//     baseline-inherited) is available it is the union of that base and the delta;
//     otherwise (or if the base is malformed) it degrades to the delta content so
//     a manifest is always produced without failing the build.
//
// The complete SBOM is staged under the resolved base-SBOM name and
// manifest.DefaultSPDXFile is set to it, so the caller's CopySBOMToChroot embeds
// the complete inventory in place of the inherited file.
func stageOverlaySBOMArtifacts(template *config.ImageTemplate, info *BaselineInfo, rootMount string, plan *ResolutionPlan, report *PreflightReport) (*overlaySBOMArtifacts, error) {
	pkgs := contributedPackages(info, plan)

	// Packages the overlay removed must be dropped from the base document so the
	// complete SBOM reflects the final inventory, not the pre-removal baseline. Uses
	// ApprovedRemovals (not ToRemove) so an rpm Obsoletes-driven removal — which
	// `rpm -U` erases implicitly and so never appears in ToRemove — is still excluded
	// from the complete inventory.
	var removedNames []string
	if report != nil {
		removedNames = report.ApprovedRemovals
	}

	// Resolve the base SBOM (external override or baseline-inherited) plus the name
	// the complete document is written under.
	base := resolveOverlayBaseSBOM(template, rootMount)

	tempDir := config.TempDir()

	// Delta SBOM: only the overlay-contributed packages.
	deltaPath := filepath.Join(tempDir, overlayDeltaTempName(base.name))
	if err := manifest.WriteSPDXToFile(pkgs, deltaPath); err != nil {
		return nil, fmt.Errorf("writing overlay delta SBOM: %w", err)
	}

	// Complete SBOM: base + delta (minus removals) when a base is available, else
	// delta-only. Track whether it degraded to delta-only so the caller can skip the
	// redundant build-dir sidecar (see overlaySBOMArtifacts.completeIsDeltaOnly).
	completePath := filepath.Join(tempDir, base.name)
	completeIsDeltaOnly := false
	if base.found {
		err := manifest.WriteMergedSPDXToFile(base.data, pkgs, removedNames, completePath)
		// A malformed EXTERNAL base must fall back to the inherited SBOM before
		// degrading to delta-only, matching the documented external -> inherited ->
		// delta-only order. (A malformed inherited base has no further base to try.)
		if err != nil && base.fromExternal && base.inheritedFound {
			log.Warnf("Overlay SBOM: external base SBOM failed to merge (%v); falling back to the baseline-embedded SBOM", err)
			err = manifest.WriteMergedSPDXToFile(base.inheritedData, pkgs, removedNames, completePath)
		}
		if err != nil {
			// No usable base left; degrade the complete SBOM to the delta so the
			// image still gets a manifest.
			log.Warnf("Overlay SBOM: merging into base SBOM %q failed (%v); complete SBOM will contain the overlay delta only", base.name, err)
			if werr := manifest.WriteSPDXToFile(pkgs, completePath); werr != nil {
				return nil, fmt.Errorf("writing overlay complete SBOM (delta fallback): %w", werr)
			}
			completeIsDeltaOnly = true
		}
	} else {
		log.Warnf("Overlay SBOM: no base SBOM available (external or at %s); the in-image manifest will contain the overlay delta only and no separate complete SBOM sidecar is emitted", manifest.ImageSBOMPath)
		if err := manifest.WriteSPDXToFile(pkgs, completePath); err != nil {
			return nil, fmt.Errorf("writing overlay complete SBOM: %w", err)
		}
		completeIsDeltaOnly = true
	}

	// Set (not restore) DefaultSPDXFile to the complete SBOM's name so the in-image
	// embed (manifest.CopySBOMToChroot, which keys off this variable) writes the
	// complete inventory under that name, mirroring create mode's generateSBOM. The
	// build-dir sidecars do NOT consult this variable — they are copied from the
	// staged completePath/deltaPath threaded via overlaySBOMArtifacts.
	manifest.DefaultSPDXFile = base.name

	return &overlaySBOMArtifacts{deltaPath: deltaPath, completePath: completePath, completeIsDeltaOnly: completeIsDeltaOnly}, nil
}

// overlayDeltaTempName derives the delta SBOM's temp-file name from the complete
// SBOM name by inserting a ".delta" infix before the .json extension (e.g.
// spdx_manifest_deb_x.json -> spdx_manifest_deb_x.delta.json). Keeping it distinct
// from the complete name means both can be staged in the same temp dir without
// clobbering each other.
func overlayDeltaTempName(completeName string) string {
	ext := filepath.Ext(completeName)
	return strings.TrimSuffix(completeName, ext) + ".delta" + ext
}

// resolveOverlayBaseSBOM decides which SBOM the overlay-contributed packages are
// merged into, and the filename the merged document is written under. It is pure
// (reads files only) so it is unit-testable without the sudo-backed chroot copy.
//
// Precedence:
//  1. The externally-supplied SBOM at baseline.source.sbomPath, when set AND
//     readable — a missing/unreadable external file is not fatal and falls through.
//  2. The SBOM the overlay image inherited from the baseline at /usr/share/sbom.
//
// The inherited SBOM is always discovered (even when the external override wins)
// so the merged document can REPLACE the inherited file in place under its own
// name rather than leaving a stale second inventory behind. When no inherited SBOM
// exists the package default filename is used. found is false only when neither an
// external nor an inherited base SBOM is available, in which case the caller writes
// the delta alone under the returned name.
// overlayBaseSBOM is the resolved base-SBOM choice for the complete SBOM merge. It
// carries the chosen base bytes plus the baseline-inherited bytes as a fallback,
// so the merge step can retry the inherited SBOM when a chosen EXTERNAL base is
// readable but malformed (see stageOverlaySBOMArtifacts) — matching the documented
// external -> inherited -> delta-only fallback order.
type overlaySBOMBase struct {
	// name is the filename the complete document is written under (the inherited
	// SBOM's own name when present, else the package default).
	name string
	// data is the chosen base: the external SBOM when set+readable, otherwise the
	// inherited one. found is false when neither is available.
	data  []byte
	found bool
	// fromExternal is true when data came from baseline.source.sbomPath rather than
	// the inherited SBOM.
	fromExternal bool
	// inheritedData/inheritedFound are the baseline-embedded SBOM, retained as a
	// fallback for the malformed-external case even when the external base won.
	inheritedData  []byte
	inheritedFound bool
}

func resolveOverlayBaseSBOM(template *config.ImageTemplate, rootMount string) overlaySBOMBase {
	inheritedName, inheritedData, inheritedFound := readBaselineSBOM(rootMount)

	base := overlaySBOMBase{
		name:           manifest.DefaultSPDXFile,
		data:           inheritedData,
		found:          inheritedFound,
		inheritedData:  inheritedData,
		inheritedFound: inheritedFound,
	}
	if inheritedFound {
		base.name = inheritedName
	}

	if extPath := baselineExternalSBOMPath(template); extPath != "" {
		if extData, ok := readExternalBaseSBOM(extPath); ok {
			base.data, base.found, base.fromExternal = extData, true, true
			log.Infof("Overlay SBOM: using externally-supplied base SBOM %q", extPath)
		} else {
			log.Warnf("Overlay SBOM: external base SBOM %q is absent or unreadable; falling back to the baseline-embedded SBOM", extPath)
		}
	}
	return base
}

// baselineExternalSBOMPath returns the trimmed externally-supplied base SBOM path
// from baseline.source.sbomPath, or "" when it is unset (or the baseline/source
// is absent). It defaults to unset so an overlay template that omits the field
// keeps the previous inherited-SBOM behavior.
func baselineExternalSBOMPath(template *config.ImageTemplate) string {
	if template == nil || template.Baseline == nil || template.Baseline.Source == nil {
		return ""
	}
	return strings.TrimSpace(template.Baseline.Source.SBOMPath)
}

// readExternalBaseSBOM reads the externally-supplied base SBOM at path. It reports
// ok=false (rather than an error) when the file is absent, unreadable, or empty,
// so the caller can fall back to the inherited SBOM without failing the build. The
// bytes are validated as parseable SPDX later, by the merge step.
//
// The path is user-controlled (baseline.source.sbomPath) and the build often runs
// under sudo, so it is read with the symlink-rejecting safe reader: a symlink there
// must not be followed to slurp an arbitrary host file (e.g. /etc/shadow) into the
// generated SBOM artifacts.
func readExternalBaseSBOM(path string) ([]byte, bool) {
	data, err := security.SafeReadFile(path, security.RejectSymlinks)
	if err != nil {
		return nil, false
	}
	if len(data) == 0 {
		return nil, false
	}
	return data, true
}

// readBaselineSBOM finds and reads the SBOM the overlay image inherited from the
// baseline under <rootMount>/usr/share/sbom. It returns the file's base name, its
// bytes, and whether a usable SBOM was found. Selection mirrors the inspector's
// picker: an "spdx_manifest*" JSON is preferred, otherwise the first JSON file.
//
// The SBOM lives inside a mounted, potentially untrusted baseline image and the
// build often runs under sudo, so it is read with the symlink-rejecting safe
// reader: a symlink planted under /usr/share/sbom must not be followed to read an
// arbitrary host file and propagate its contents into output artifacts.
func readBaselineSBOM(rootMount string) (string, []byte, bool) {
	// The final-file symlink check below (SafeReadFile) does not cover a symlinked
	// ANCESTOR: a baseline that made /usr/share/sbom itself point at a host directory
	// would otherwise have os.ReadDir list that host dir and ingest an arbitrary host
	// JSON into the emitted SBOM. Reject a symlink anywhere in the directory chain
	// before listing it (a genuinely-absent dir simply yields "no inherited SBOM").
	sbomRel := strings.TrimPrefix(manifest.ImageSBOMPath, "/")
	if err := assertNoSymlinkInChrootPath(rootMount, sbomRel); err != nil {
		log.Warnf("Overlay SBOM: refusing to read inherited SBOM directory (unsafe path in baseline): %v", err)
		return "", nil, false
	}
	sbomDir := filepath.Join(rootMount, sbomRel)
	entries, err := os.ReadDir(sbomDir)
	if err != nil {
		return "", nil, false
	}

	name, ok := pickBaselineSBOMName(entries)
	if !ok {
		return "", nil, false
	}

	data, err := security.SafeReadFile(filepath.Join(sbomDir, name), security.RejectSymlinks)
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

// Deterministic build-dir SBOM sidecar name suffixes, keyed off the emitted
// image's base name (<name>-<version>): the delta SBOM (overlay-contributed
// packages only) and the complete SBOM (full final baseline+overlay inventory).
// Both are SPDX JSON, preserving the repo's spdx JSON format convention.
const (
	overlayDeltaSBOMSuffix    = ".delta.spdx.json"
	overlayCompleteSBOMSuffix = ".complete.spdx.json"
)

// emitOverlayArtifact moves the modified baseline copy into the image build
// directory as "<name>-<version>.raw" and copies the two SBOM sidecars alongside
// it (delta + complete), mirroring the create-mode RAW artifact naming. It returns
// the final image path.
//
// The loop device must already be detached (the backing file is moved, not the
// live device).
func emitOverlayArtifact(template *config.ImageTemplate, copyPath, version string, sbom *overlaySBOMArtifacts) (string, error) {
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

	base := fmt.Sprintf("%s-%s", template.GetImageName(), version)
	finalPath := filepath.Join(buildDir, base+".raw")
	// Move the finished baseline into place without a shell (same rationale as the
	// mkdir above). os.Rename covers the common same-filesystem case; fall back to
	// a copy+remove when the workspace and build directory are on different mounts
	// (os.Rename returns EXDEV), which a cross-device `mv` would otherwise handle.
	if err := moveFile(copyPath, finalPath); err != nil {
		return "", fmt.Errorf("overlay emit: failed to move %s to %s: %w", copyPath, finalPath, err)
	}

	if sbom != nil {
		// The delta sidecar is a GUARANTEED artifact (always emitted), so a failure to
		// write it fails the emit — otherwise a "successful" build would silently omit
		// a documented output. Remove the just-moved image first so the failed emit
		// leaves nothing partial in the output directory (emit returns "" on error, so
		// the deferred cleanup cannot see this path).
		deltaDst := filepath.Join(buildDir, base+overlayDeltaSBOMSuffix)
		if serr := emitOverlaySBOMSidecar(sbom.deltaPath, deltaDst); serr != nil {
			removeEmittedOutputs(finalPath, deltaDst)
			return "", fmt.Errorf("overlay emit: failed to write the delta SBOM sidecar: %w", serr)
		}
		// The complete sidecar is emitted ONLY when it is a true base+delta union. When
		// no base SBOM was available it degraded to exactly the delta, so a separate
		// ".complete.spdx.json" would be byte-identical to the delta and its name would
		// misleadingly imply a full inventory — skip it (and clear any stale sidecar a
		// PRIOR build left at that deterministic path, so a rebuild without a base never
		// ships an inventory describing the old image). The in-image /usr/share/sbom
		// manifest is still written regardless.
		completeDst := filepath.Join(buildDir, base+overlayCompleteSBOMSuffix)
		if !sbom.completeIsDeltaOnly {
			// The complete sidecar is documented as emitted whenever a base SBOM is
			// available, so a write failure is fatal (like the delta): roll back the
			// image and both sidecars rather than ship a build missing a promised
			// artifact — or, worse, retaining a stale complete sidecar from an earlier
			// build at the same deterministic path.
			if serr := emitOverlaySBOMSidecar(sbom.completePath, completeDst); serr != nil {
				removeEmittedOutputs(finalPath, deltaDst, completeDst)
				return "", fmt.Errorf("overlay emit: failed to write the complete SBOM sidecar: %w", serr)
			}
		} else {
			log.Infof("Overlay emit: no base SBOM was available, so the complete SBOM equals the delta; skipping the redundant %s sidecar", base+overlayCompleteSBOMSuffix)
			// A rebuild of the same name/version that now has no base must not leave a
			// stale complete sidecar from an earlier build claiming a full inventory.
			removeStaleArtifact(completeDst)
		}
	}
	return finalPath, nil
}

// removeEmittedOutputs deletes a set of just-written output paths on the emit
// failure path, so a fatal sidecar-write failure leaves nothing partial in the
// output directory (emit returns "" on error, so the deferred cleanup cannot see
// these paths). Best-effort: a removal failure is logged, never returned, and a
// missing file is not an error.
func removeEmittedOutputs(paths ...string) {
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Warnf("Overlay emit: failed to remove output %s during failure cleanup: %v", p, err)
		}
	}
}

// removeEmittedArtifacts deletes the emitted image and its deterministic sidecars
// from the build output directory. It is called on the failure path when a stage
// AFTER emit (inspect/convert) fails, so a partial/abandoned output never lingers.
// It is best-effort and logs-not-fails: cleanup must never itself abort or panic.
//
// The set covered is the emitted RAW plus every sibling this pipeline writes off
// the same "<name>-<version>" base: the SBOM sidecars, the inspection report, and
// the converted disk formats. The Convert Artifacts stage can also emit COMPRESSED
// outputs (e.g. <base>.qcow2.gz, <base>.raw.xz) named "<file>.<compression>", so
// the template's disk.artifacts type+compression pairs are added to the suffix set
// — otherwise a partial compressed artifact from a failed convert would linger.
// A missing file is not an error.
func removeEmittedArtifacts(rawPath string, template *config.ImageTemplate) {
	if strings.TrimSpace(rawPath) == "" {
		return
	}
	dir := filepath.Dir(rawPath)
	base := strings.TrimSuffix(filepath.Base(rawPath), filepath.Ext(rawPath)) // "<name>-<version>"
	suffixes := []string{
		".raw", ".qcow2", ".vhd", ".vhdx", ".vmdk", ".vdi",
		overlayDeltaSBOMSuffix, overlayCompleteSBOMSuffix, overlayInspectReportSuffix,
	}
	// Add each configured artifact's format and, when compressed, its
	// "<type>.<compression>" form so a partial/leftover compressed output is removed.
	if template != nil {
		for _, a := range template.GetDiskConfig().Artifacts {
			t := strings.TrimSpace(a.Type)
			if t == "" {
				continue
			}
			suffixes = append(suffixes, "."+t)
			if c := strings.TrimSpace(a.Compression); c != "" {
				suffixes = append(suffixes, "."+t+"."+c)
			}
		}
	}
	seen := make(map[string]bool, len(suffixes))
	for _, suf := range suffixes {
		p := filepath.Join(dir, base+suf)
		if seen[p] {
			continue
		}
		seen[p] = true
		if err := os.Remove(p); err != nil {
			if !os.IsNotExist(err) {
				log.Warnf("Overlay cleanup: failed to remove partial output artifact %s: %v", p, err)
			}
			continue
		}
		log.Infof("Overlay cleanup: removed partial output artifact %s", p)
	}
}

// emitOverlaySBOMSidecar copies a staged SBOM to dst next to the emitted image,
// returning an error on a read/write failure so the caller can decide whether it is
// fatal (the delta sidecar, a guaranteed artifact) or best-effort (the complete
// sidecar). It uses the symlink-rejecting safe read/write (the build dir is
// user-owned, no sudo needed).
func emitOverlaySBOMSidecar(src, dst string) error {
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("staged SBOM path is empty")
	}
	data, err := security.SafeReadFile(src, security.RejectSymlinks)
	if err != nil {
		return fmt.Errorf("reading staged SBOM %s: %w", src, err)
	}
	if err := security.SafeWriteFile(dst, data, 0o644, security.RejectSymlinks); err != nil {
		return fmt.Errorf("writing SBOM sidecar %s: %w", dst, err)
	}
	log.Infof("Overlay emit: wrote SBOM sidecar %s", dst)
	return nil
}

// overlayInspectReportSuffix is the extension appended to the emitted image's
// base name to form the inspection report artifact (e.g. myimage-1.0.raw ->
// myimage-1.0.inspect.txt). It is documented in the overlay build help text and
// CLI specification.
const overlayInspectReportSuffix = ".inspect.txt"

// removeStaleArtifact deletes a deterministic output path that this build will NOT
// (re)write, so a rebuild of the same name/version never leaves an earlier build's
// artifact behind to be presented as describing the new image (e.g. a stale
// inspection report when --inspect is now off, or a stale complete SBOM sidecar).
// Best-effort: a missing file is the expected common case and not an error; a real
// removal failure is logged.
func removeStaleArtifact(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("Overlay emit: failed to remove stale artifact %s: %v", path, err)
		}
		return
	}
	log.Infof("Overlay emit: removed stale artifact %s from a previous build", path)
}

// overlayInspectReportPath returns the inspection report artifact path for an
// emitted image artifact: the same directory and base name with the RAW/format
// extension replaced by overlayInspectReportSuffix. Keeping it a sibling of the
// image (rather than a fixed name) means multiple images in one build directory
// each get their own report.
func overlayInspectReportPath(artifactPath string) string {
	base := filepath.Base(artifactPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(filepath.Dir(artifactPath), base+overlayInspectReportSuffix)
}

// inspectOverlayArtifact runs a post-build inspection of the emitted RAW image and
// writes the rendered summary to an artifact file alongside the image, rather than
// to the console. It reuses the same diskfs inspector the standalone `inspect`
// command uses, so it needs no loop device or root: the image is already released
// and inspected purely in userspace. The console gets only a short pointer to the
// generated report. Inspection is a reporting step; a failure here is surfaced by
// the caller so a broken emitted image does not pass silently.
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

	// Write the report as a sidecar artifact next to the image. Mode 0644 and the
	// symlink-rejecting safe writer match the SBOM sidecar convention
	// (manifest.CopySBOMToImageBuildDir). The build directory already exists (emit
	// created it before the artifact was placed there).
	reportPath := overlayInspectReportPath(artifactPath)
	if werr := security.SafeWriteFile(reportPath, []byte(buf.String()), 0o644, security.RejectSymlinks); werr != nil {
		return fmt.Errorf("writing overlay inspection report to %s: %w", reportPath, werr)
	}
	// Console output stays clean: just a one-line pointer to the artifact, not the
	// full summary.
	log.Infof("Overlay image inspection report written to %s", reportPath)
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

	// Cross-device: copy then remove the source. A copy that fails partway can
	// leave a truncated file at dst (in the output directory), so remove it before
	// returning the error — the no-partial-state contract requires the output
	// directory to hold nothing from a failed emit.
	if err := copyLocalFile(src, dst); err != nil {
		if rerr := os.Remove(dst); rerr != nil && !os.IsNotExist(rerr) {
			log.Warnf("Overlay emit: failed to remove partial destination %s after a failed cross-device copy: %v", dst, rerr)
		}
		return err
	}
	if err := os.Remove(src); err != nil {
		// The copy succeeded but the source could not be removed: moveFile still
		// returns an error, and because emit never returns a path in that case the
		// deferred cleanup cannot see the (complete) destination. Remove dst here so
		// the failed emit leaves nothing in the output directory.
		if rerr := os.Remove(dst); rerr != nil && !os.IsNotExist(rerr) {
			log.Warnf("Overlay emit: failed to remove destination %s after a failed source cleanup: %v", dst, rerr)
		}
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
