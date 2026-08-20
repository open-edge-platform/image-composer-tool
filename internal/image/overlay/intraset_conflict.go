package overlay

import (
	"fmt"
	"sort"
	"strings"
)

// classifyIntraSetConflicts turns a declared Conflicts:/Breaks: (deb) or
// Conflicts: (rpm) between two DIFFERENT to-install packages into an
// ActionConflict. classifyConflicts only compares a to-install artifact against
// the BASELINE, so it cannot see this case: neither conflicting package is
// installed yet, so there is nothing there to clash with — both are downloaded
// and handed to dpkg/rpm together, which unpacks the first cleanly and aborts on
// the second. This turns that mid-install failure into an up-front,
// actionable preflight block instead.
//
// The conflict target can be a virtual capability rather than a real package
// name (e.g. Ubuntu's ros-jazzy-univloc-slam alternatives, where the "-sse" and
// "-lze" hardware variants each Provides: the same virtual name and Conflicts:
// it), so provides resolves the target through every to-install package's
// declared Provides:, not just its bare name.
//
// Unlike a baseline conflict, there is no removal-based resolution: both
// packages are newly requested, so removing the "loser" would just have the
// template re-request it on the next resolve. The template must select only one
// alternative — which is exactly what conflictPolicy=fail (the default) is
// there to force. Each unordered conflicting pair is reported once, keyed by its
// two package names sorted, so a Conflicts: declared on both sides does not
// double-report.
func classifyIntraSetConflicts(family PackageManager, resolved []ResolvedPackage, conflicts []ArtifactConflict, provides []ArtifactProvides) []PlannedAction {
	if len(resolved) == 0 || len(conflicts) == 0 {
		return nil
	}

	// providers maps a capability name (a to-install package's own name, or a
	// virtual name it Provides:) to the real to-install package name(s) that
	// satisfy it.
	versions := make(map[string]string, len(resolved))
	providers := make(map[string][]string, len(resolved))
	for _, rp := range resolved {
		name := strings.TrimSpace(rp.Name)
		if name == "" {
			continue
		}
		versions[name] = rp.Version
		providers[name] = append(providers[name], name)
	}
	for _, p := range provides {
		for _, capability := range p.Provides {
			capability = strings.TrimSpace(capability)
			if capability == "" {
				continue
			}
			providers[capability] = append(providers[capability], p.Package)
		}
	}

	var actions []PlannedAction
	seen := make(map[string]bool)
	for _, c := range conflicts {
		declarer := strings.TrimSpace(c.Package)
		target := strings.TrimSpace(c.Conflicts.Name)
		if declarer == "" || target == "" {
			continue
		}
		for _, candidate := range providers[target] {
			if candidate == declarer {
				continue // a package's own Provides never conflicts with itself
			}
			// A versioned conflict only clashes when the candidate's resolved version
			// falls within the declared range, mirroring classifyConflicts.
			if vc := c.Conflicts.Constraint; vc != nil {
				if instVer, ok := versions[candidate]; ok {
					if cmp, err := comparePkgVersions(family, instVer, vc.Ver); err == nil && !constraintSatisfied(vc.Op, cmp) {
						continue
					}
				}
			}
			pair := []string{declarer, candidate}
			sort.Strings(pair)
			key := pair[0] + "\x00" + pair[1]
			if seen[key] {
				continue
			}
			seen[key] = true
			actions = append(actions, PlannedAction{
				Type:             ActionConflict,
				Package:          candidate,
				RequestedVersion: versions[candidate],
				ConflictWith:     declarer,
				Detail: fmt.Sprintf("declared as a conflict by %q, but both are requested for install in "+
					"this overlay — the template must select only one", declarer),
			})
		}
	}
	return actions
}
