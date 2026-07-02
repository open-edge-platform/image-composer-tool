// Package overlay implements baseline-image ingestion for overlay mode: it copies
// the user-provided baseline RAW image into the build workspace (never mutating
// the original), attaches a loop device with partition scanning, and guarantees
// the loop device is detached on success, failure, or panic.
//
// This is the first concrete stage of the overlay build flow. Downstream stages
// (mount, OS detection, package install) consume the Context populated here.
package overlay

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/image/imagedisc"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/network"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/system"
)

var log = logger.Logger()

// baselineCopyName is the fixed filename of the workspace copy of the baseline
// image. A fixed name keeps the layout predictable regardless of the source
// path or URL filename.
const baselineCopyName = "baseline.raw"

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
	// contain path separators or "..". Constrain it to a single, safe path
	// segment before joining it into the workspace path, otherwise the overlay
	// workspace (and the baseline copy/remove that operate under it) could
	// escape the intended work directory.
	sysConfigName := template.GetSystemConfigName()
	if err := validatePathSegment(sysConfigName); err != nil {
		return nil, fmt.Errorf("invalid system config name %q: %w", sysConfigName, err)
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
	for label, part := range map[string]string{
		"target.os":   template.Target.OS,
		"target.dist": template.Target.Dist,
		"target.arch": template.Target.Arch,
	} {
		if err := validatePathSegment(part); err != nil {
			return nil, fmt.Errorf("invalid %s %q: %w", label, part, err)
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
// success, failure, or panic. The workspace baseline copy is removed only on full
// success, unless debug retention is enabled.
//
// fn carries the downstream overlay work (mount, inspect, install). Any error it
// returns is propagated after cleanup runs. A detach failure on the otherwise
// successful path is also surfaced (and the copy retained) so a leaked loop
// device is never mistaken for a clean build.
func (ing *Ingestor) WithBaseline(fn func(*Context) error) (err error) {
	if fn == nil {
		return fmt.Errorf("WithBaseline: fn must not be nil")
	}

	ctx, err := ing.acquire()
	if err != nil {
		return err
	}

	// defer-based cleanup runs on normal return, error, or panic. The loop
	// device is always detached. A detach failure is promoted to the return
	// error when fn itself succeeded, marking the run unsuccessful. The
	// workspace copy is removed only on full success (fn ok and detach ok)
	// unless retention is enabled.
	fnErr := error(nil)
	defer func() {
		detachErr := ing.detach(ctx)
		if fnErr == nil && detachErr != nil {
			// fn succeeded but cleanup did not: fail the run so the caller
			// learns about the leaked loop device.
			err = detachErr
		}
		if err == nil {
			// Honor debug retention only on a fully successful build.
			ing.removeCopy(ctx, false)
		} else {
			log.Debugf("Retaining workspace baseline copy after unsuccessful build: %s", ctx.BaselineCopyPath)
		}
	}()

	fnErr = fn(ctx)
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
	if err := ing.copyBaseline(copyPath); err != nil {
		// copyBaseline may have created or partially written the destination
		// before failing (a URL download truncates the output file before
		// io.Copy completes). Force-remove it so no corrupt partial baseline
		// is left behind, matching this function's no-leak contract.
		ing.removeCopy(ctx, true)
		return nil, err
	}

	loopDevPath, partitions, err := ing.loopDev.AttachImageToLoopDev(copyPath)
	if err != nil {
		// Loop attach failed: remove the copy we just made so nothing leaks.
		// Force removal regardless of debug retention — this is partial-state
		// cleanup, not the post-success copy that retention is meant to keep.
		ing.removeCopy(ctx, true)
		return nil, fmt.Errorf("failed to attach baseline copy to loop device: %w", err)
	}
	ctx.LoopDevPath = loopDevPath
	ctx.Partitions = partitions

	log.Infof("Attached baseline copy %s to loop device %s (%d partitions)",
		copyPath, loopDevPath, len(partitions))
	return ctx, nil
}

// copyBaseline copies the source baseline into dst. A local path is copied (never
// symlinked or moved); an http(s) URL is downloaded — over TLS when the URL uses
// https, in the clear when it uses http (both schemes are permitted by
// BaselineSource.Validate). The user-provided source is never modified.
func (ing *Ingestor) copyBaseline(dst string) error {
	src := ing.template.Baseline.Source

	switch {
	case src.URL != "":
		log.Debugf("Downloading baseline image from %s to %s", src.URL, dst)
		if err := network.DownloadFile(src.URL, dst, false); err != nil {
			return fmt.Errorf("failed to download baseline image from %s: %w", src.URL, err)
		}
		// DownloadFile creates the file 0644; tighten it to 0600 so the baseline
		// stays private by default, matching the local-copy path (copyLocalFile),
		// regardless of any pre-existing workspace directory permissions.
		if err := os.Chmod(dst, 0600); err != nil {
			return fmt.Errorf("failed to restrict permissions on downloaded baseline %s: %w", dst, err)
		}
		log.Debugf("Finished downloading baseline image to %s", dst)
	default:
		log.Debugf("Copying baseline image from %s to %s", src.Path, dst)
		if err := copyLocalFile(src.Path, dst); err != nil {
			return fmt.Errorf("failed to copy baseline image from %s: %w", src.Path, err)
		}
		log.Debugf("Finished copying baseline image to %s", dst)
	}
	return nil
}

// copyLocalFile copies src to dst using a native streaming copy (no shell).
// file.CopyFile shells out via bash -c with single-quoted paths, so a source or
// destination path containing a single quote could break quoting; both paths
// here are user-influenced (baseline.source.path and the systemConfig.name in
// dst), so the copy is done in-process to avoid any shell handling. The
// workspace copy is created user-owned, so no sudo is needed.
func copyLocalFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(out, in)
	return err
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
