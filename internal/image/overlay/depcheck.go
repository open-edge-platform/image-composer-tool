package overlay

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
)

// VersionConstraint is a single parsed version requirement on a dependency,
// e.g. the "(= 255.4-1ubuntu8.16)" in a deb Depends field or the "= 1.2-3" in
// an rpm requires entry. Op is the comparison operator normalized to one of
// "=", ">=", "<=", ">>"/">", "<<"/"<"; Ver is the target version string.
type VersionConstraint struct {
	Op  string
	Ver string
}

// DependencyAlternative is one option within a dependency edge. A deb dependency
// term can list alternatives ("a | b"), any one of which satisfies the edge; an
// rpm requires entry is always a single alternative. Constraint is nil when the
// alternative carries no version requirement (mere presence satisfies it).
type DependencyAlternative struct {
	Name       string
	Constraint *VersionConstraint
}

// ArtifactDependency is one dependency edge declared by a package the overlay
// will install (a plan.ToInstall member), read directly from the prepared
// artifact's control metadata. The edge is satisfied when any of its
// Alternatives is satisfied by the post-install package set.
type ArtifactDependency struct {
	// Package is the ToInstall package that declares this dependency.
	Package string
	// Alternatives are the "a | b" options; the edge holds if any one holds.
	Alternatives []DependencyAlternative
}

// readOverlayArtifactDependencies is the impure seam that reads the version-
// constrained dependency edges of every plan.ToInstall artifact from its
// on-disk package metadata. It is analogous to simulateOverlayInstall: its
// output is a validation aid feeding the pure preflight check, so a read failure
// is non-fatal (the preflight simply loses this one safety net). Tests override
// it to inject synthetic dependency edges without real artifacts.
var readOverlayArtifactDependencies = func(family PackageManager, plan *ResolutionPlan) ([]ArtifactDependency, error) {
	if plan == nil || len(plan.ToInstall) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(plan.DownloadDir) == "" {
		return nil, fmt.Errorf("overlay dependency check: plan has packages to install but no artifact download directory")
	}

	var deps []ArtifactDependency
	for _, rp := range plan.ToInstall {
		artifact, err := artifactFileFor(rp)
		if err != nil {
			return nil, err
		}
		hostPath := joinArtifactPath(plan.DownloadDir, artifact)

		var edges []ArtifactDependency
		switch family {
		case PackageManagerDNF:
			edges, err = readRPMArtifactDependencies(rp.Name, hostPath)
		default:
			edges, err = readDebArtifactDependencies(rp.Name, hostPath)
		}
		if err != nil {
			// Best-effort: a single unreadable artifact must not fail the preflight;
			// the two-slice model and the remaining artifacts still gate the build.
			log.Warnf("Overlay dependency check: failed to read dependencies of %q from %s (continuing): %v", rp.Name, hostPath, err)
			continue
		}
		deps = append(deps, edges...)
	}
	return deps, nil
}

// readDebArtifactDependencies reads the Depends and Pre-Depends control fields
// of a prepared .deb with `dpkg -f` and parses their version-constrained edges.
// The file is read on the host, so no chroot is entered.
func readDebArtifactDependencies(pkgName, hostPath string) ([]ArtifactDependency, error) {
	var edges []ArtifactDependency
	for _, field := range []string{"Depends", "Pre-Depends"} {
		// hostPath is a URL-derived artifact path; quote it before interpolating it
		// into the bash -c command so metacharacters can't alter execution.
		out, err := shell.ExecCmdSilent(fmt.Sprintf("dpkg -f %s %s", shell.QuoteArg(hostPath), field), true, shell.HostPath, nil)
		if err != nil {
			return nil, fmt.Errorf("reading %s of %s: %w", field, hostPath, err)
		}
		edges = append(edges, parseDebDependsField(pkgName, out)...)
	}
	return edges, nil
}

// readRPMArtifactDependencies reads a prepared .rpm's requires with `rpm -qpR`
// (rpm is on the shell allowlist) and parses their version-constrained edges.
func readRPMArtifactDependencies(pkgName, hostPath string) ([]ArtifactDependency, error) {
	// hostPath is a URL-derived artifact path; quote it before interpolating it
	// into the bash -c command so metacharacters can't alter execution.
	out, err := shell.ExecCmdSilent(fmt.Sprintf("rpm -qpR %s", shell.QuoteArg(hostPath)), true, shell.HostPath, nil)
	if err != nil {
		return nil, fmt.Errorf("reading requires of %s: %w", hostPath, err)
	}
	return parseRPMRequires(pkgName, out), nil
}

// parseDebDependsField parses a deb Depends/Pre-Depends field value into
// dependency edges. The field is a comma-separated list of terms; each term is a
// pipe-separated list of alternatives; each alternative is "name[:arch] [(op
// ver)]" with optional build-profile "<...>" annotations that are ignored.
func parseDebDependsField(pkgName, field string) []ArtifactDependency {
	var edges []ArtifactDependency
	for _, term := range strings.Split(field, ",") {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		var alts []DependencyAlternative
		for _, alt := range strings.Split(term, "|") {
			if a, ok := parseDebAlternative(alt); ok {
				alts = append(alts, a)
			}
		}
		if len(alts) > 0 {
			edges = append(edges, ArtifactDependency{Package: pkgName, Alternatives: alts})
		}
	}
	return edges
}

// parseDebAlternative parses one deb dependency alternative ("libc6 (>= 2.34)")
// into a name and optional version constraint. Architecture qualifiers (":amd64",
// ":any") and build-profile annotations ("<!nocheck>") are stripped.
func parseDebAlternative(alt string) (DependencyAlternative, bool) {
	alt = strings.TrimSpace(alt)
	if alt == "" {
		return DependencyAlternative{}, false
	}

	var constraint *VersionConstraint
	if open := strings.Index(alt, "("); open != -1 {
		if close := strings.Index(alt[open:], ")"); close != -1 {
			inner := alt[open+1 : open+close]
			if c, ok := parseConstraint(inner); ok {
				constraint = &c
			}
		}
		alt = alt[:open]
	}

	// Drop any build-profile annotation and trailing whitespace, then the name is
	// the first token; strip a ":arch" multiarch qualifier from it.
	if angle := strings.Index(alt, "<"); angle != -1 {
		alt = alt[:angle]
	}
	name := strings.TrimSpace(alt)
	if sp := strings.IndexAny(name, " \t"); sp != -1 {
		name = name[:sp]
	}
	if colon := strings.Index(name, ":"); colon != -1 {
		name = name[:colon]
	}
	if name == "" {
		return DependencyAlternative{}, false
	}
	return DependencyAlternative{Name: name, Constraint: constraint}, true
}

// parseRPMRequires parses `rpm -qpR` output into dependency edges. File
// requirements ("/bin/sh"), rpmlib feature requirements, and soname/capability
// requirements without a version operator are skipped: they are satisfied via
// Provides rather than by a package name+version, so they cannot produce the
// present-but-wrong-version case this check targets.
func parseRPMRequires(pkgName, out string) []ArtifactDependency {
	var edges []ArtifactDependency
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "/") || strings.HasPrefix(line, "rpmlib(") {
			continue
		}
		fields := strings.Fields(line)
		// Only "name op version" (3 tokens) carries a version constraint we can
		// check; bare capability names have nothing to compare.
		if len(fields) != 3 {
			continue
		}
		c, ok := parseConstraint(fields[1] + " " + fields[2])
		if !ok {
			continue
		}
		edges = append(edges, ArtifactDependency{
			Package:      pkgName,
			Alternatives: []DependencyAlternative{{Name: fields[0], Constraint: &c}},
		})
	}
	return edges
}

// parseConstraint parses an "op version" pair (deb: "= 1.2", ">= 1.2"; rpm the
// same) into a VersionConstraint. It accepts the operator and version separated
// by whitespace or joined ("(>=1.2)"). Returns ok=false when no recognized
// operator/version is present.
func parseConstraint(s string) (VersionConstraint, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return VersionConstraint{}, false
	}

	// Longest operators first so ">=" is not misread as ">".
	for _, op := range []string{"<<", ">>", "<=", ">=", "=", "<", ">"} {
		if !strings.HasPrefix(s, op) {
			continue
		}
		ver := strings.TrimSpace(s[len(op):])
		if ver == "" {
			return VersionConstraint{}, false
		}
		return VersionConstraint{Op: op, Ver: ver}, true
	}
	return VersionConstraint{}, false
}

// joinArtifactPath joins a download directory and an artifact filename. It is a
// thin wrapper so the dependency reader and the installer share one notion of an
// artifact's on-disk location.
func joinArtifactPath(dir, artifact string) string {
	return filepath.Join(dir, artifact)
}
