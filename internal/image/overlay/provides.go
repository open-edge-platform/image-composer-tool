package overlay

import (
	"fmt"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// ArtifactProvides records the virtual capabilities a to-install package
// declares via its deb Provides: field or rpm Provides. It backs the
// preflight's intra-set conflict check: a Conflicts: entry naming a virtual
// capability (e.g. "ros-jazzy-univloc-slam", which both the "-sse" and "-lze"
// hardware variants Provides:) only names a real package via this — without it,
// two mutually exclusive to-install packages that conflict through a shared
// virtual name would go undetected.
type ArtifactProvides struct {
	// Package is the real to-install package name declaring the Provides.
	Package string
	// Provides are the virtual capability/package names it provides.
	Provides []string
}

// readOverlayArtifactProvides is the impure seam that reads the Provides:
// declarations of every plan.ToInstall artifact (deb Provides:, rpm --provides).
// Like the dependency/conflict readers it is a best-effort validation aid: a
// read failure is non-fatal, so a package whose Provides could not be read
// simply loses this one cross-check. Tests override it to inject synthetic
// Provides.
var readOverlayArtifactProvides = func(family PackageManager, plan *ResolutionPlan) ([]ArtifactProvides, error) {
	if plan == nil || len(plan.ToInstall) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(plan.DownloadDir) == "" {
		return nil, fmt.Errorf("overlay provides check: plan has packages to install but no artifact download directory")
	}

	var provides []ArtifactProvides
	for _, rp := range plan.ToInstall {
		artifact, err := artifactFileFor(rp)
		if err != nil {
			return nil, err
		}
		hostPath := joinArtifactPath(plan.DownloadDir, artifact)

		var names []string
		switch family {
		case PackageManagerDNF:
			names, err = readRPMArtifactProvides(hostPath)
		default:
			names, err = readDebArtifactProvides(hostPath)
		}
		if err != nil {
			// Best-effort: a single unreadable artifact must not fail the preflight;
			// the two-slice model and the remaining artifacts still gate the build.
			log.Warnf("Overlay provides check: failed to read Provides of %q from %s (continuing): %v", rp.Name, hostPath, err)
			continue
		}
		if len(names) == 0 {
			continue
		}
		provides = append(provides, ArtifactProvides{Package: rp.Name, Provides: names})
	}
	return provides, nil
}

// readDebArtifactProvides reads the Provides control field of a prepared .deb
// with `dpkg -f` and reduces it to the provided package names, reusing the same
// parser detect.go uses for the baseline's installed packages.
func readDebArtifactProvides(hostPath string) ([]string, error) {
	// hostPath is a URL-derived artifact path; quote it before interpolating it
	// into the bash -c command so metacharacters can't alter execution.
	out, err := shell.ExecCmdSilent(fmt.Sprintf("dpkg -f %s Provides", shell.QuoteArg(hostPath)), true, shell.HostPath, nil)
	if err != nil {
		return nil, fmt.Errorf("reading Provides of %s: %w", hostPath, err)
	}
	return parseDebProvides(out), nil
}

// readRPMArtifactProvides reads a prepared .rpm's Provides with
// `rpm -qp --provides` (rpm is on the shell allowlist) and reduces it to bare
// capability names, discarding version constraints and skipping file/rpmlib
// entries that can never match a Conflicts: package name.
func readRPMArtifactProvides(hostPath string) ([]string, error) {
	// hostPath is a URL-derived artifact path; quote it before interpolating it
	// into the bash -c command so metacharacters can't alter execution.
	out, err := shell.ExecCmdSilent(fmt.Sprintf("rpm -qp --provides %s", shell.QuoteArg(hostPath)), true, shell.HostPath, nil)
	if err != nil {
		return nil, fmt.Errorf("reading provides of %s: %w", hostPath, err)
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "/") || strings.HasPrefix(line, "rpmlib(") {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	return names, nil
}
