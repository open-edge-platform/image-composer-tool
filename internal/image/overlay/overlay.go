// Package overlay implements baseline-image ingestion for overlay mode: it copies
// the user-provided baseline RAW image into the build workspace (never mutating
// the original), attaches a loop device with partition scanning, and guarantees
// the loop device is detached on success, failure, or panic.
//
// This is the first concrete stage of the overlay build flow. Downstream stages
// (mount, OS detection, package install) consume the Context populated here.
package overlay

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imageconvert"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

var log = logger.Logger()

// Baseline-format normalization seams. They wrap the impure format detection,
// conversion, and tool-availability probes so unit tests can exercise acquire()'s
// normalization logic without qemu-img or real disk images (mirroring resize.go's
// resizeExec seam). Production uses the real implementations.
var (
	detectBaselineFormatFn = imageconvert.DetectImageFormat
	convertBaselineToRawFn = convertBaselineToRaw
	qemuImgAvailableFn     = func() bool {
		// IsCommandExist returns (false, nil) when the command is genuinely
		// absent; a non-nil error signals an unexpected probe failure. Surface
		// that at debug level instead of discarding it, then treat qemu-img as
		// unavailable so non-RAW baselines fail with a clear "unsupported" path.
		ok, err := shell.IsCommandExist("qemu-img", shell.HostPath)
		if err != nil {
			log.Debugf("qemu-img availability probe failed: %v", err)
			return false
		}
		return ok
	}
)

// convertBaselineToRaw converts src (a non-RAW image in a qemu-img supported
// format) into dst as a RAW whole-disk image using qemu-img. srcFormat is the
// qemu-img-native format string (e.g. "qcow2", "vpc") already detected and
// verified by normalizeBaseline; it is passed via -f so qemu-img uses that
// verified format instead of re-probing the source independently, closing a
// format-confusion window where the re-probe could disagree with our detection.
// All arguments are shell-quoted: they are workspace-internal and their composing
// segments are already validated, but the overlay convention is to always quote
// values interpolated into a command executed via bash -c. (imageconvert.ConvertImageToRaw
// is intentionally not reused because it does not quote its convert paths.)
func convertBaselineToRaw(src, dst, srcFormat string) error {
	cmd := fmt.Sprintf("qemu-img convert -f %s -O raw %s %s",
		shell.QuoteArg(srcFormat), shell.QuoteArg(src), shell.QuoteArg(dst))
	if _, err := shell.ExecCmd(cmd, false, shell.HostPath, nil); err != nil {
		return fmt.Errorf("failed to convert baseline to raw: %w", err)
	}
	return nil
}

// baselineCopyName is the fixed filename of the workspace copy of the baseline
// image. A fixed name keeps the layout predictable regardless of the source
// path or URL filename.
const baselineCopyName = "baseline.raw"

// baselineStageName is the fixed filename of the workspace staging copy used when
// the source baseline is not RAW. The source is copied/downloaded here first, its
// actual format is verified, then it is converted into baselineCopyName
// ("baseline.raw") and this staging file is removed. A fixed name keeps the layout
// predictable regardless of the source path or URL filename.
const baselineStageName = "baseline.src"

// maxBaselineBytes bounds the size of a downloaded baseline image so a
// misconfigured or malicious URL cannot exhaust workspace disk. Sized generously
// for a whole-disk RAW image; adjust if larger baselines become legitimate.
const maxBaselineBytes = 64 << 30 // 64 GiB

// defaultSysConfigName is the workspace path segment used when a template omits
// systemConfig.name (which is schema-optional). It is a safe, fixed segment so
// the overlay workspace stays predictable and inside the work directory.
const defaultSysConfigName = "overlay"

// safePathSegment matches a conservative allowlist for user-supplied path
// segments: ASCII letters, digits, and the separators "._-", one or more times.
// It deliberately excludes whitespace and shell metacharacters (';', '&', '$',
// backticks, quotes, globs, ...) because these segments are later both joined
// into filesystem paths AND interpolated into commands executed via bash -c
// (through internal/utils/shell), so anything outside this set could change
// shell parsing or escape the workspace directory.
var safePathSegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validatePathSegment rejects values that are not a single, safe path segment.
// It is used to guard user-supplied names (e.g. the system config name and the
// target OS/dist/arch fields) before they are joined into filesystem paths and
// embedded into shell commands, so a value like "../..", "/etc", or one carrying
// shell metacharacters cannot redirect the overlay workspace or alter command
// parsing.
func validatePathSegment(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("must not be %q", name)
	}
	// The allowlist already forbids the path separator, so a valid value is
	// inherently a single segment; this covers traversal and shell-injection in
	// one check.
	if !safePathSegment.MatchString(name) {
		return fmt.Errorf("must contain only ASCII letters, digits, '.', '_' or '-' (no separators, whitespace, or shell metacharacters)")
	}
	return nil
}

// overlaySysConfigName returns the single, safe path segment used for the
// per-build overlay workspace and image build directory. SystemConfig.Name is
// user-supplied, schema-optional, and has no schema pattern, so it may be empty
// or carry path separators / "..". An empty value falls back to a fixed default
// (so a template omitting systemConfig.name still gets a predictable, isolated
// location instead of an empty segment that collides with other builds); a
// non-empty value is constrained to a single safe segment. Both NewIngestor (the
// workspace) and overlayImageBuildDir (the emitted artifact) derive their path
// segment through this helper so they stay consistent.
func overlaySysConfigName(template *config.ImageTemplate) (string, error) {
	name := template.GetSystemConfigName()
	if strings.TrimSpace(name) == "" {
		return defaultSysConfigName, nil
	}
	if err := validatePathSegment(name); err != nil {
		return "", fmt.Errorf("invalid system config name %q: %w", name, err)
	}
	return name, nil
}

// LoopDevManager is the subset of imagedisc.LoopDevInterface needed to attach
// and detach a baseline image. It is declared here so tests can inject a fake.
type LoopDevManager interface {
	AttachImageToLoopDev(imagePath string) (string, []string, error)
	LoopSetupDelete(loopDevPath string) error
}

// Context carries overlay ingestion state across downstream build stages.
type Context struct {
	// BaselineCopyPath is the workspace copy of the baseline RAW image. All
	// modification happens here; the user-provided source is never touched.
	BaselineCopyPath string
	// LoopDevPath is the attached loop device, e.g. "/dev/loop0".
	LoopDevPath string
	// Partitions are the enumerated partition nodes, e.g. ["/dev/loop0p1"].
	Partitions []string
}

// Ingestor copies a baseline image into the workspace and attaches it to a loop
// device for downstream overlay stages.
type Ingestor struct {
	template *config.ImageTemplate
	loopDev  LoopDevManager
	// workDir is the per-build overlay workspace directory.
	workDir string
	// retainCopy, when true, keeps the workspace baseline copy after a
	// successful build for debugging. Defaults to the global debug-mode flag.
	retainCopy bool
}

// NewIngestor constructs an Ingestor for an overlay-mode template. It returns an
// error if the template is not in overlay mode or is missing a baseline source.
func NewIngestor(template *config.ImageTemplate) (*Ingestor, error) {
	if template == nil {
		return nil, fmt.Errorf("image template cannot be nil")
	}
	if !template.IsOverlayMode() {
		return nil, fmt.Errorf("template is not in overlay mode")
	}
	if template.Baseline == nil || template.Baseline.Source == nil {
		return nil, fmt.Errorf("overlay template is missing baseline.source")
	}
	// Enforce the baseline.source contract (exactly one of path or url, with a
	// well-formed value) here as well as at template load. NewIngestor may be
	// constructed from a programmatically built template that never passed
	// through validateBaseline, so re-checking gives a clear error up front
	// instead of an opaque copy/download failure later.
	src := template.Baseline.Source
	if err := src.Validate(); err != nil {
		return nil, fmt.Errorf("invalid baseline.source: %w", err)
	}

	globalWorkDir, err := config.WorkDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve work directory: %w", err)
	}
	// SystemConfig.Name is user-supplied and has no schema pattern, so it may
	// contain path separators or "..". It is also schema-optional, so a valid
	// template may omit it entirely; fall back to a safe default in that case
	// rather than rejecting the template. A non-empty value is still constrained
	// to a single, safe path segment before being joined into the workspace path,
	// otherwise the overlay workspace (and the baseline copy/remove that operate
	// under it) could escape the intended work directory.
	sysConfigName, err := overlaySysConfigName(template)
	if err != nil {
		return nil, err
	}
	// Image.Name is user-supplied and, like SystemConfig.Name, has no schema
	// pattern. It is later joined into the emitted artifact filename
	// (<buildDir>/<image.name>-<version>.raw), so a programmatic caller supplying a
	// name with path separators or ".." could write the artifact outside the build
	// directory. Constrain it to a single, safe path segment here too.
	imageName := template.GetImageName()
	if err := validatePathSegment(imageName); err != nil {
		return nil, fmt.Errorf("invalid image name %q: %w", imageName, err)
	}
	// Target.{OS,Dist,Arch} feed providerID, which is also joined into the
	// workspace path. The JSON schema constrains these for YAML-loaded templates,
	// but NewIngestor also accepts programmatically built templates that never
	// passed schema validation, so a component containing separators or ".."
	// could otherwise redirect the workspace. Validate each component too.
	// Use a fixed-order slice (not a map) so that when more than one component
	// is invalid the error is deterministic — a map iteration order would make
	// the reported field vary between runs and can make tests flaky.
	for _, part := range []struct {
		label string
		value string
	}{
		{"target.os", template.Target.OS},
		{"target.dist", template.Target.Dist},
		{"target.arch", template.Target.Arch},
	} {
		if err := validatePathSegment(part.value); err != nil {
			return nil, fmt.Errorf("invalid %s %q: %w", part.label, part.value, err)
		}
	}
	providerID := system.GetProviderId(template.Target.OS, template.Target.Dist, template.Target.Arch)
	workDir := filepath.Join(globalWorkDir, providerID, "overlay", sysConfigName)

	return &Ingestor{
		template:   template,
		loopDev:    imagedisc.NewLoopDev(),
		workDir:    workDir,
		retainCopy: config.IsDebugMode(),
	}, nil
}

// WithBaseline acquires the baseline (copy into workspace + loop attach), invokes
// fn with the resulting Context, and guarantees the loop device is detached on
// success, failure, or panic. The workspace baseline copy is removed on success
// and on a plain fn failure with a clean detach (honoring debug retention in both
// cases); it is retained only when debug retention is enabled, when fn panics, or
// when detach fails (a leaked loop device still references the backing file). This
// keeps large baseline copies from accumulating across repeated failures while
// preserving whatever state matters for a postmortem or a leaked-device recovery.
//
// fn carries the downstream overlay work (mount, inspect, install). Any error it
// returns is propagated after cleanup runs. On a normal (non-panic) return a
// detach failure is surfaced too (joined with fn's error when both fail) so a
// leaked loop device is never hidden behind another error or mistaken for a
// clean build; the copy is retained in that case. When fn panics, cleanup still
// runs (detach is attempted and the copy retained) but the function re-panics,
// so a detach failure on that path cannot be returned — it is only logged.
func (ing *Ingestor) WithBaseline(fn func(*Context) error) (err error) {
	if fn == nil {
		return fmt.Errorf("WithBaseline: fn must not be nil")
	}

	ctx, err := ing.acquire()
	if err != nil {
		return err
	}

	// defer-based cleanup runs on normal return, error, or panic. The loop
	// device is always detached. A detach failure is always surfaced to the
	// caller (joined with fn's error if fn also failed), marking the run
	// unsuccessful. The workspace copy is removed only on full success (fn ok
	// and detach ok) unless retention is enabled.
	fnErr := error(nil)
	panicked := true // cleared right after fn returns; still set if fn panics
	defer func() {
		// Recover any panic so cleanup can treat it as an unsuccessful run
		// (retain the copy for debugging) before re-panicking to preserve the
		// original behavior. Without this, a panic leaves fnErr and err nil, so
		// the deferred cleanup would wrongly remove the copy as if the build had
		// fully succeeded.
		r := recover()

		detachErr := ing.detach(ctx)
		if !panicked && detachErr != nil {
			// A detach failure must always reach the caller so a leaked loop
			// device is never silently swallowed. When fn already failed, join
			// the two so both are reported; when fn succeeded, this alone marks
			// the run unsuccessful.
			err = errors.Join(fnErr, detachErr)
		}
		// Retain the copy only when there is something to preserve: a panic (keep
		// state for the postmortem) or a detach failure (the leaked loop device
		// still references this backing file, so unlinking it would hinder
		// recovery). On success and on a plain fn failure with a clean detach the
		// copy is removed — baseline images are large, so keeping them across
		// repeated failures would accumulate significant disk usage. removeCopy
		// still honors debug retention (force=false) in both remove cases.
		if panicked || detachErr != nil {
			log.Debugf("Retaining workspace baseline copy after unsuccessful build: %s", ctx.BaselineCopyPath)
		} else {
			ing.removeCopy(ctx, false)
		}

		if r != nil {
			panic(r)
		}
	}()

	fnErr = fn(ctx)
	panicked = false
	err = fnErr
	return err
}

// acquire prepares the workspace, copies (or downloads) the baseline image into
// it, and attaches a loop device. On any failure it cleans up whatever it created
// so no partial state (workspace copy or loop device) is leaked.
func (ing *Ingestor) acquire() (*Context, error) {
	if err := os.MkdirAll(ing.workDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create overlay workspace %s: %w", ing.workDir, err)
	}

	copyPath := filepath.Join(ing.workDir, baselineCopyName)
	ctx := &Context{BaselineCopyPath: copyPath}
	if err := ing.normalizeBaseline(ctx); err != nil {
		// normalizeBaseline may have created or partially written the destination
		// before failing (a URL download truncates the output file before io.Copy
		// completes; a conversion may leave a partial baseline.raw). It already
		// removes any non-raw staging file it created, but the canonical
		// baseline.raw is force-removed here so no corrupt partial baseline is left
		// behind, matching this function's no-leak contract.
		ing.removeCopy(ctx, true)
		return nil, err
	}

	loopDevPath, partitions, err := ing.loopDev.AttachImageToLoopDev(copyPath)
	if err != nil {
		if loopDevPath != "" {
			// A loop device was created but could not be detached, so it still
			// references this backing file. Removing the file now would unlink a
			// file the leaked device points at, making recovery/debugging harder.
			// Retain the copy and surface the (already path-annotated) error.
			log.Errorf("Retaining workspace baseline copy %s: loop device %s may still be attached after attach failure", copyPath, loopDevPath)
			return nil, fmt.Errorf("failed to attach baseline copy to loop device: %w", err)
		}
		// No loop device outstanding: remove the copy we just made so nothing
		// leaks. Force removal regardless of debug retention — this is
		// partial-state cleanup, not the post-success copy that retention keeps.
		ing.removeCopy(ctx, true)
		return nil, fmt.Errorf("failed to attach baseline copy to loop device: %w", err)
	}
	ctx.LoopDevPath = loopDevPath
	ctx.Partitions = partitions

	log.Infof("Attached baseline copy %s to loop device %s (%d partitions)",
		copyPath, loopDevPath, len(partitions))
	return ctx, nil
}

// prepareDestination makes dst safe to write by unlinking any pre-existing entry
// there, so the subsequent open creates a fresh regular file we own rather than
// writing through an attacker-planted node.
//
// The workspace directory may be attacker-writable when the tool runs elevated,
// and the destination filename is fixed/predictable (baselineCopyName). Two
// escalation vectors follow from that:
//   - a symlink at dst: an O_TRUNC|O_WRONLY open follows it and clobbers the target;
//   - a hardlink at dst: it looks like a regular file, but O_TRUNC zeroes the
//     shared inode, destroying whatever else references it.
//
// Unlinking removes only the directory entry (the name), so it breaks a hardlink
// without touching the shared inode's data and drops a symlink without following
// it — neither victim is modified. A missing dst is the normal case. We refuse to
// unlink a pre-existing directory (that indicates a misconfigured workDir, not a
// stale copy). Both callers then open with O_CREATE|O_EXCL (copyLocalFile and
// downloadBaseline), so a node re-planted in the TOCTOU window between unlink and
// open fails the open rather than being followed — the race is closed on both the
// local-copy and URL-download paths.
func prepareDestination(dst string) error {
	fi, err := os.Lstat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat baseline destination %s: %w", dst, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("baseline destination %s is a directory; refusing to overwrite it", dst)
	}
	if err := os.Remove(dst); err != nil {
		return fmt.Errorf("failed to clear existing baseline destination %s: %w", dst, err)
	}
	return nil
}

// copyBaseline copies the source baseline into dst. A local path is copied (never
// symlinked or moved); an https URL is downloaded over TLS (BaselineSource.Validate
// permits only https for remote sources). The user-provided source is never modified.
func (ing *Ingestor) copyBaseline(dst string) error {
	src := ing.template.Baseline.Source

	if err := prepareDestination(dst); err != nil {
		return err
	}

	switch {
	case src.URL != "":
		log.Debugf("Downloading baseline image from %s to %s", src.URL, dst)
		if err := downloadBaseline(src.URL, dst); err != nil {
			return fmt.Errorf("failed to download baseline image from %s to %s: %w", src.URL, dst, err)
		}
		log.Debugf("Finished downloading baseline image to %s", dst)
	default:
		log.Debugf("Copying baseline image from %s to %s", src.Path, dst)
		if err := copyLocalFile(src.Path, dst); err != nil {
			return fmt.Errorf("failed to copy baseline image from %s to %s: %w", src.Path, dst, err)
		}
		log.Debugf("Finished copying baseline image to %s", dst)
	}
	return nil
}

// normalizeBaseline gets a RAW whole-disk image into ctx.BaselineCopyPath
// (workDir/baseline.raw), ready for loop-attach, from a source baseline that may be
// RAW, QCOW2, VHD, or VHDX.
//
// The declared format (baseline.source.format, defaulting to raw) governs the flow:
//
//   - Non-RAW requires qemu-img: if it is missing, this fails fast before copying
//     any bytes.
//   - RAW with qemu-img absent preserves the legacy behavior exactly — the source is
//     copied verbatim to baseline.raw with no format detection, so a raw-only host
//     that never had qemu-img keeps working.
//   - With qemu-img present, the actual format is detected and verified against the
//     declared format. For a declared-RAW source this is advisory (a detection error
//     is logged and ignored; only a positive mismatch to a known non-RAW format is
//     fatal) so a legitimate raw image is never newly rejected. For a declared
//     non-RAW source a detection error or mismatch is fatal, and the verified source
//     is converted from a staging file into baseline.raw.
//
// On any failure the staging file (if created) is removed here; the caller
// (acquire) force-removes baseline.raw. The user-provided source is never modified.
func (ing *Ingestor) normalizeBaseline(ctx *Context) error {
	declared := ing.template.Baseline.Source.Format
	if declared == "" {
		declared = config.BaselineFormatRaw
	}
	// config validation constrains baseline.source.format for YAML-loaded
	// templates, but a programmatically-built template bypasses that path. Re-check
	// the declared format against the supported set here so an unsupported value
	// (e.g. "vmdk") fails with a clear error instead of being handed to qemu-img,
	// keeping in-memory and YAML templates consistent.
	switch declared {
	case config.BaselineFormatRaw, config.BaselineFormatQcow2, config.BaselineFormatVHD, config.BaselineFormatVHDX:
		// supported
	default:
		return fmt.Errorf("baseline.source.format %q is not supported (must be one of %q, %q, %q, %q)",
			declared, config.BaselineFormatRaw, config.BaselineFormatQcow2, config.BaselineFormatVHD, config.BaselineFormatVHDX)
	}
	copyPath := ctx.BaselineCopyPath
	qemuAvailable := qemuImgAvailableFn()

	// Non-RAW baselines can only be handled by converting via qemu-img. Fail fast
	// (before copying gigabytes) when the tooling is absent.
	if declared != config.BaselineFormatRaw && !qemuAvailable {
		return fmt.Errorf("baseline.source.format %q requires qemu-img, which was not found on the host", declared)
	}

	// RAW source, no qemu-img: legacy path. Copy bytes verbatim, no detection.
	if declared == config.BaselineFormatRaw && !qemuAvailable {
		log.Warnf("qemu-img not found; skipping baseline format verification for declared raw source")
		return ing.copyBaseline(copyPath)
	}

	// qemu-img is available from here.
	if declared == config.BaselineFormatRaw {
		// Copy directly to baseline.raw, then verify the copied bytes really are RAW.
		if err := ing.copyBaseline(copyPath); err != nil {
			return err
		}
		actual, derr := detectBaselineFormatFn(copyPath)
		if derr != nil {
			// Advisory only: a detection failure must not newly reject a raw image
			// that worked before. Warn and proceed with the copied bytes.
			log.Warnf("skipping baseline format verification (detection failed): %v", derr)
			return nil
		}
		if canon := canonicalBaselineFormat(actual); canon != config.BaselineFormatRaw && canon != "" {
			return fmt.Errorf("baseline actual format %q does not match declared baseline.source.format %q", actual, declared)
		}
		return nil
	}

	// Declared non-RAW: stage the source, verify its format, then convert to RAW.
	stagePath := filepath.Join(ing.workDir, baselineStageName)
	// The staging file lives entirely within acquire()'s lifetime; remove it on
	// every exit path (success or failure) so it never leaks. baseline.raw is the
	// canonical artifact and is handled by acquire()/removeCopy.
	defer func() {
		if err := os.Remove(stagePath); err != nil && !os.IsNotExist(err) {
			log.Warnf("Failed to remove baseline staging file %s: %v", stagePath, err)
		}
	}()

	if err := ing.copyBaseline(stagePath); err != nil {
		return err
	}

	actual, derr := detectBaselineFormatFn(stagePath)
	if derr != nil {
		return fmt.Errorf("failed to detect baseline format: %w", derr)
	}
	if canonicalBaselineFormat(actual) != declared {
		return fmt.Errorf("baseline actual format %q does not match declared baseline.source.format %q", actual, declared)
	}

	// Clear any pre-existing entry at the canonical baseline.raw before qemu-img
	// writes it. qemu-img opens the output itself (we cannot pass O_EXCL as
	// copyLocalFile/writeBoundedBody do), so without this a symlink or hardlink
	// planted at baseline.raw in an attacker-writable workspace would be followed
	// and clobbered — the same escalation prepareDestination guards against on the
	// raw copy/download paths. Unlinking first drops that node so the convert writes
	// a fresh file we own.
	if err := prepareDestination(copyPath); err != nil {
		return err
	}
	log.Infof("Converting %s baseline to raw for overlay", declared)
	// Pass the qemu-img-detected format (actual) rather than the declared one:
	// it is what qemu-img itself reported and verified above, and it is already
	// in qemu-img's native vocabulary (e.g. "vpc" for VHD), so -f needs no
	// translation from the baseline.source.format spelling.
	if err := convertBaselineToRawFn(stagePath, copyPath, actual); err != nil {
		return err
	}
	return nil
}

// canonicalBaselineFormat maps a qemu-img detected format string to the format
// vocabulary used by baseline.source.format. qemu-img reports a VHD image as "vpc"
// (its internal name for the format), so it is folded to "vhd"; all other values
// are returned lower-cased as-is.
func canonicalBaselineFormat(detected string) string {
	f := strings.ToLower(strings.TrimSpace(detected))
	if f == "vpc" {
		return config.BaselineFormatVHD
	}
	return f
}

// downloadBaseline fetches the baseline image from url over TLS and writes it to
// dst via writeBoundedBody, which creates dst with O_CREATE|O_EXCL (0600) and
// enforces the maxBaselineBytes cap. Unlike network.DownloadFile (which opens
// O_TRUNC and would follow a symlink re-planted in the TOCTOU window between
// prepareDestination's unlink and open), the exclusive create fails rather than
// following/truncating a re-planted node — closing the same race the local-copy
// path closes, so a privileged run against an attacker-writable workspace cannot
// be redirected to clobber an unintended path. The 0600 mode up front keeps the
// baseline private (no post-hoc chmod window), matching copyLocalFile.
//
// A CheckRedirect policy rejects any redirect to a non-https target: BaselineSource
// .Validate enforces https-only on the configured URL, but Go's default client
// follows redirects, so an https URL could be bounced to an http Location and fetch
// the baseline over plaintext. Refusing the redirect keeps the whole fetch on TLS.
func downloadBaseline(url, dst string) (err error) {
	client := network.NewSecureHTTPClient()
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to non-https URL %s", req.URL.Redacted())
		}
		return nil
	}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	return writeBoundedBody(dst, resp.ContentLength, resp.Body, maxBaselineBytes)
}

// writeBoundedBody streams body into a freshly created dst (O_CREATE|O_EXCL, 0600)
// while enforcing the cap (max bytes) and, when contentLength is known (>= 0),
// verifying the bytes written match it. It is split out of downloadBaseline so the
// size/truncation guards are unit-testable without a TLS server (and with a small
// cap, so tests need not stream gigabytes). Callers pass maxBaselineBytes.
//
// prepareDestination has already unlinked any entry at dst, so O_EXCL fails rather
// than following/truncating a node re-planted in the TOCTOU window between unlink
// and open.
func writeBoundedBody(dst string, contentLength int64, body io.Reader, maxBytes int64) (err error) {
	// Reject early when the server declares a length larger than the cap, so we
	// never begin writing an oversized image.
	if contentLength > maxBytes {
		return fmt.Errorf("baseline image too large: declared %d bytes exceeds limit %d", contentLength, maxBytes)
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// Bound the copy even when Content-Length is unknown (-1): read at most one
	// byte past the cap so an over-limit body is detectable rather than silently
	// truncated exactly at the cap.
	written, err := io.Copy(out, io.LimitReader(body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}
	if written > maxBytes {
		return fmt.Errorf("baseline image too large: exceeded limit %d bytes", maxBytes)
	}

	// Guard against a silently truncated download: when the server declared a
	// Content-Length (>= 0), the bytes actually written must match it. A short
	// body otherwise yields a corrupt baseline that only fails later at
	// mount/inspect with a far less obvious error.
	if contentLength >= 0 && written != contentLength {
		return fmt.Errorf("incomplete download of %s: wrote %d of %d bytes", dst, written, contentLength)
	}
	return nil
}

// sparseCopyChunk is the block size used by copyLocalFile for zero-run detection.
// It matches a common filesystem block/cluster granularity so a fully-zero chunk
// can be turned into a hole in the destination.
const sparseCopyChunk = 64 * 1024

// copyLocalFile copies src to dst using a native streaming copy (no shell).
// file.CopyFile shells out via bash -c with single-quoted paths, so a source or
// destination path containing a single quote could break quoting; both paths
// here are user-influenced (baseline.source.path and the systemConfig.name in
// dst), so the copy is done in-process to avoid any shell handling. The
// workspace copy is created user-owned, so no sudo is needed.
//
// The copy is sparse-aware: baseline RAW images are typically sparse (large runs
// of holes that read back as zeros). A plain io.Copy would materialize every
// hole into allocated zero blocks, inflating workspace usage and slowing the
// copy. Instead, all-zero chunks are skipped by seeking the destination forward,
// leaving a hole, and the file is truncated to the exact source size at the end
// so a trailing zero run is preserved as a hole rather than written out.
func copyLocalFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// O_EXCL (no O_TRUNC): prepareDestination just unlinked any existing entry, so
	// dst must not exist. If it does, a node was re-planted in the TOCTOU window and
	// O_EXCL fails the open instead of following/truncating it.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	buf := make([]byte, sparseCopyChunk)
	var written int64
	for {
		n, readErr := io.ReadFull(in, buf)
		if n > 0 {
			chunk := buf[:n]
			if isAllZero(chunk) {
				// Leave a hole: advance the destination offset without writing.
				if _, serr := out.Seek(int64(n), io.SeekCurrent); serr != nil {
					return serr
				}
			} else {
				nw, werr := out.Write(chunk)
				if werr != nil {
					return werr
				}
				// Guard against a short write: io.Writer may legally write fewer
				// bytes than requested without an error, which would silently
				// corrupt the copied image and desync the offset.
				if nw != len(chunk) {
					return fmt.Errorf("short write copying %s: wrote %d of %d bytes: %w", dst, nw, len(chunk), io.ErrShortWrite)
				}
			}
			written += int64(n)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	// A trailing hole (or the seek past the final zero chunk) does not extend the
	// file, so set the exact length explicitly. This also materializes a hole for
	// any zero run at the very end of the source.
	if terr := out.Truncate(written); terr != nil {
		return terr
	}
	return nil
}

// isAllZero reports whether b consists entirely of zero bytes.
func isAllZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// detach detaches the loop device if one is attached. It returns the detach
// error (also logged) so callers on the success path can surface a failed
// cleanup instead of silently leaking the loop device. A no-op (nothing
// attached) returns nil.
func (ing *Ingestor) detach(ctx *Context) error {
	if ctx == nil || ctx.LoopDevPath == "" {
		return nil
	}
	if err := ing.loopDev.LoopSetupDelete(ctx.LoopDevPath); err != nil {
		log.Errorf("Failed to detach loop device %s: %v", ctx.LoopDevPath, err)
		return fmt.Errorf("failed to detach loop device %s: %w", ctx.LoopDevPath, err)
	}
	log.Infof("Detached loop device %s", ctx.LoopDevPath)
	return nil
}

// removeCopy removes the workspace baseline copy. When force is false, debug
// retention is honored and the copy is kept. When force is true (partial-state
// cleanup after an acquire failure), the copy is always removed so nothing
// leaks, regardless of retention.
func (ing *Ingestor) removeCopy(ctx *Context, force bool) {
	if ctx == nil || ctx.BaselineCopyPath == "" {
		return
	}
	if ing.retainCopy && !force {
		log.Infof("Retaining workspace baseline copy for debugging: %s", ctx.BaselineCopyPath)
		return
	}
	// The copy is created user-owned (CopyFile with sudo=false), so removal
	// needs no sudo and no shell: os.Remove takes the path literally, avoiding
	// any shell-metacharacter handling on the workspace path.
	if err := os.Remove(ctx.BaselineCopyPath); err != nil && !os.IsNotExist(err) {
		log.Errorf("Failed to remove workspace baseline copy %s: %v", ctx.BaselineCopyPath, err)
		return
	}
	log.Debugf("Removed workspace baseline copy %s", ctx.BaselineCopyPath)
}
