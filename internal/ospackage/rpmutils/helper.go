package rpmutils

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/open-edge-platform/os-image-composer/internal/ospackage"
	"github.com/open-edge-platform/os-image-composer/internal/utils/logger"
)

const defaultRepoPriority = 500

func normalizeRepositoryPriority(priority int) int {
	if priority == 0 {
		return defaultRepoPriority
	}
	return priority
}

func getRepositoryPriority(packageURL string) int {
	normalizedPkgURL := strings.TrimRight(packageURL, "/")
	highestPriority := defaultRepoPriority

	for _, repo := range UserRepo {
		repoURL := strings.TrimRight(repo.URL, "/")
		if repoURL == "" {
			continue
		}

		if normalizedPkgURL == repoURL || strings.HasPrefix(normalizedPkgURL, repoURL+"/") {
			repoPriority := normalizeRepositoryPriority(repo.Priority)
			if repoPriority > highestPriority {
				highestPriority = repoPriority
			}
		}
	}

	return highestPriority
}

func selectByPriorityThenRepo(parentBase string, candidates []ospackage.PackageInfo) ospackage.PackageInfo {
	highestPriority := -1
	highestPriorityCandidates := make([]ospackage.PackageInfo, 0, len(candidates))

	for _, candidate := range candidates {
		candidatePriority := getRepositoryPriority(candidate.URL)
		if candidatePriority > highestPriority {
			highestPriority = candidatePriority
			highestPriorityCandidates = highestPriorityCandidates[:0]
			highestPriorityCandidates = append(highestPriorityCandidates, candidate)
			continue
		}

		if candidatePriority == highestPriority {
			highestPriorityCandidates = append(highestPriorityCandidates, candidate)
		}
	}

	if len(highestPriorityCandidates) == 1 {
		return highestPriorityCandidates[0]
	}

	var sameRepoCandidates []ospackage.PackageInfo
	for _, candidate := range highestPriorityCandidates {
		candidateBase, err := extractRepoBase(candidate.URL)
		if err != nil {
			continue
		}
		if candidateBase == parentBase {
			sameRepoCandidates = append(sameRepoCandidates, candidate)
		}
	}

	if len(sameRepoCandidates) == 1 {
		return sameRepoCandidates[0]
	}

	if len(sameRepoCandidates) > 1 {
		latest := sameRepoCandidates[0]
		for _, candidate := range sameRepoCandidates[1:] {
			cmp := compareVersions(candidate.Version, latest.Version)
			if cmp > 0 {
				latest = candidate
			}
		}
		return latest
	}

	return highestPriorityCandidates[0]
}

func resolveMultiCandidates(parentPkg ospackage.PackageInfo, candidates []ospackage.PackageInfo) (ospackage.PackageInfo, error) {
	parentBase, err := extractRepoBase(parentPkg.URL)
	if err != nil {
		return ospackage.PackageInfo{}, fmt.Errorf("failed to extract repo base from parent package URL: %w", err)
	}

	/////////////////////////////////////
	//A: if version is specified
	/////////////////////////////////////
	// All candidates have the same .Name, so just use candidates[0].Name for version extraction
	op := ""
	ver := ""
	hasVersionConstraint := false
	if len(candidates) > 0 {
		op, ver, hasVersionConstraint = extractVersionRequirement(parentPkg.RequiresVer, extractBasePackageNameFromFile(candidates[0].Name))
	}

	if hasVersionConstraint {
		var matchingCandidates []ospackage.PackageInfo

		for _, candidate := range candidates {
			// Check if version constraint is satisfied
			cmp, err := comparePackageVersions(candidate.Version, ver)
			if err != nil {
				continue
			}

			versionMatches := false
			switch op {
			case "=":
				versionMatches = (cmp == 0)
			case "<<", "<":
				versionMatches = (cmp < 0)
			case "<=":
				versionMatches = (cmp <= 0)
			case ">>", ">":
				versionMatches = (cmp > 0)
			case ">=":
				versionMatches = (cmp >= 0)
			}

			if versionMatches {
				matchingCandidates = append(matchingCandidates, candidate)
			}
		}

		if len(matchingCandidates) > 0 {
			return selectByPriorityThenRepo(parentBase, matchingCandidates), nil
		}

		return ospackage.PackageInfo{}, fmt.Errorf("no candidates satisfy version constraint = %s%s", op, ver)
	}

	/////////////////////////////////////
	// B: if version is not specified
	//////////////////////////////////////

	// Check for empty candidates list
	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, fmt.Errorf("no candidates provided for selection")
	}

	// If only one candidate, return it
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	return selectByPriorityThenRepo(parentBase, candidates), nil
}

func extractRepoBase(rawURL string) (string, error) {
	log := logger.Logger()
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	path := u.Path

	// For Debian repositories - split by "/pool/"
	if strings.Contains(path, "/pool/") {
		parts := strings.SplitN(path, "/pool/", 2)
		base := fmt.Sprintf("%s://%s%s/pool/", u.Scheme, u.Host, parts[0])
		return base, nil
	}

	// For RPM repositories - split by "/Packages/"
	if strings.Contains(path, "/Packages/") {
		parts := strings.SplitN(path, "/Packages/", 2)
		base := fmt.Sprintf("%s://%s%s/Packages/", u.Scheme, u.Host, parts[0])
		return base, nil
	}

	// For RPM repositories with RPMS structure - find the directory containing the RPM file
	if strings.HasSuffix(path, ".rpm") {
		// Remove the filename to get the directory
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash > 0 {
			dirPath := path[:lastSlash+1] // Keep the trailing slash
			base := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, dirPath)
			return base, nil
		}
	}

	// For DEB repositories with .deb files
	if strings.HasSuffix(path, ".deb") {
		// Remove the filename to get the directory
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash > 0 {
			dirPath := path[:lastSlash+1] // Keep the trailing slash
			base := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, dirPath)
			return base, nil
		}
	}

	// Fallback: if no specific pattern found, try to extract directory from any file
	if strings.Contains(path, ".") { // Likely a file with extension
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash > 0 {
			dirPath := path[:lastSlash+1]
			base := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, dirPath)
			return base, nil
		}
	}

	log.Errorf("Unable to extract repo base from URL: %s", rawURL)
	return "", fmt.Errorf("unable to extract repo base from URL: %s", rawURL)
}

// compareVersions compares two Debian package versions
// Returns 1 if v1 > v2, -1 if v1 < v2, 0 if equal
func compareVersions(v1, v2 string) int {
	// Extract version from Debian package names like "acct_6.6.4-5+b1_amd64.deb"
	extractVersion := func(name string) string {
		parts := strings.Split(name, "_")
		if len(parts) >= 2 {
			return parts[1]
		}
		return name
	}
	ver1 := extractVersion(v1)
	ver2 := extractVersion(v2)
	cmp, _ := comparePackageVersions(ver1, ver2)
	return cmp
}

// extractBasePackageNameFromFile extracts the base package name from a full package filename
// e.g., "curl-8.8.0-2.azl3.x86_64.rpm" -> "curl"
// e.g., "curl-devel-8.8.0-1.azl3.x86_64.rpm" -> "curl-devel"
func extractBasePackageNameFromFile(fullName string) string {
	// Remove .rpm suffix if present
	name := strings.TrimSuffix(fullName, ".rpm")

	// Split by '-' and find where the version starts
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return name
	}

	// Find the first part that looks like a version (starts with digit)
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 && (parts[i][0] >= '0' && parts[i][0] <= '9') {
			// get the name
			maybe_name := strings.Join(parts[:i], "-")
			// check if version is part of the name
			// full name contains version, if package name has version,
			// it will be repeated in the full name
			for j := i + 1; j < len(parts); j++ {
				if len(parts[j]) > 0 && strings.Contains(parts[j], parts[i]) {
					maybe_name = strings.Join(parts[:j], "-")
					break
				}
			}
			// return name or name-version
			return maybe_name
		}
	}

	// If no version-like part found, return the whole name
	return name
}

// extractBaseNameFromDep takes a potentially complex requirement string
// and returns only the base package/capability name.
func extractBaseNameFromDep(req string) string {
	req = strings.TrimSpace(req)
	if req == "" {
		return ""
	}

	// Handle complex conditional dependencies with "if" clauses
	if strings.Contains(req, ") if ") {
		// Extract content between first '((' and ') if'
		if start := strings.Index(req, "(("); start != -1 {
			if end := strings.Index(req, ") if "); end != -1 {
				inner := req[start+2 : end]
				// Handle multiple operators in priority order
				for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
					if idx := strings.Index(inner, op); idx != -1 {
						return strings.TrimSpace(inner[:idx])
					}
				}
				return strings.TrimSpace(inner)
			}
		}
	}

	// Handle simple parentheses cases
	if strings.HasPrefix(req, "(") && strings.HasSuffix(req, ")") {
		inner := req[1 : len(req)-1]
		inner = strings.TrimSpace(inner)
		// Handle version operators in priority order
		for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
			if idx := strings.Index(inner, op); idx != -1 {
				return strings.TrimSpace(inner[:idx])
			}
		}
		parts := strings.Fields(inner)
		if len(parts) > 0 {
			return parts[0]
		}
		return inner
	}

	// Handle regular cases with operators
	for _, op := range []string{" >= ", " <= ", " > ", " < ", " = "} {
		if idx := strings.Index(req, op); idx != -1 {
			return strings.TrimSpace(req[:idx])
		}
	}

	finalParts := strings.Fields(req)
	if len(finalParts) == 0 {
		return ""
	}
	base := finalParts[0]
	return base
}

func findAllCandidates(parent ospackage.PackageInfo, depName string, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, error) {
	// log := logger.Logger()

	var candidates []ospackage.PackageInfo

	// First pass: look for exact name (canonical name) matches
	for _, pi := range all {
		// Extract the base package name (everything before the first '-' that starts a version)
		baseName := extractBasePackageNameFromFile(pi.Name)
		if baseName == depName {
			candidates = append(candidates, pi)
		}
	}

	// If no direct matches found, search in Provides field
	if len(candidates) == 0 {
		for _, pi := range all {
			for _, provided := range pi.Provides {
				if provided == depName {
					candidates = append(candidates, pi)
				}
			}
		}
	}

	// If no direct matches found, search in Files field
	if len(candidates) == 0 {
		for _, pi := range all {
			for _, file := range pi.Files {
				if file == depName {
					candidates = append(candidates, pi)
				}
			}
		}
	}

	// Sort candidates by version (highest version first)
	sort.Slice(candidates, func(i, j int) bool {
		cmp, _ := comparePackageVersions(candidates[i].Version, candidates[j].Version)
		return cmp > 0
	})

	return candidates, nil
}

func isGlobPattern(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func matchPackageRequest(want, packageName string) bool {
	if !isGlobPattern(want) {
		return packageName == want
	}

	matched, err := filepath.Match(want, packageName)
	if err != nil {
		return false
	}

	return matched
}

// ResolvePackage finds the best matching package for a given package name
func ResolveTopPackageConflicts(want string, all []ospackage.PackageInfo) (ospackage.PackageInfo, bool) {
	var candidates []ospackage.PackageInfo
	isGlob := isGlobPattern(want)
	for _, pi := range all {
		// 1) exact name, e.g. acct-205-25.azl3.noarch.rpm
		if !isGlob && pi.Name == want {
			candidates = append(candidates, pi)
			break
		}
		// Use PkgName if available, otherwise extract from filename
		cleanName := pi.PkgName
		if cleanName == "" {
			cleanName = extractBasePackageNameFromFile(pi.Name)
		}

		if isGlob {
			if matchPackageRequest(want, cleanName) || matchPackageRequest(want, pi.Name) {
				candidates = append(candidates, pi)
			}
			continue
		}
		// 2) base name, e.g. acct
		if cleanName == want {
			candidates = append(candidates, pi)
			continue
		}
		// 3) prefix by want-version ("acl-")
		// expected pi.Name should look like openvino-2025.3.0-2025.3.0.19807-1.noarch.rpm
		// want = openvino-2025.3.0
		if strings.HasPrefix(pi.Name, want) {
			// Extract string after "-" and compare with pi.Version
			if dashIdx := strings.LastIndex(want, "-"); dashIdx != -1 {
				verStr := want[dashIdx+1:]
				if strings.Contains(pi.Version, verStr) {
					candidates = append(candidates, pi)
					continue
				}
			}
		}
	}

	if len(candidates) == 0 {
		return ospackage.PackageInfo{}, false
	}

	// If we got an exact match in step (1), it's the only candidate
	if len(candidates) == 1 && (candidates[0].Name == want || extractBasePackageNameFromFile(candidates[0].Name) == want) {
		return candidates[0], true
	}

	// If multiple candidates, apply further filtering based on Dist
	if Dist != "" {
		// Filter candidates by release if any candidate matches Dist
		distRelease := ""
		for _, pi := range candidates {
			if idx := strings.LastIndex(pi.Version, "-"); idx != -1 {
				verPart := pi.Version[idx+1:]
				if dotIdx := strings.Index(verPart, "."); dotIdx != -1 {
					release := verPart[dotIdx+1:]
					if release == Dist {
						distRelease = release
						break
					}
				}
			}
		}
		if distRelease != "" {
			filtered := candidates[:0]
			for _, pi := range candidates {
				if idx := strings.LastIndex(pi.Version, "-"); idx != -1 {
					verPart := pi.Version[idx+1:]
					if dotIdx := strings.Index(verPart, "."); dotIdx != -1 {
						release := verPart[dotIdx+1:]
						if release == distRelease {
							filtered = append(filtered, pi)
						}
					}
				}
			}
			candidates = filtered
		}
	}

	// Sort by version (highest version first)
	sort.Slice(candidates, func(i, j int) bool {
		return compareVersions(candidates[i].Version, candidates[j].Version) > 0
	})

	return candidates[0], true
}

// ResolveWildcardPackageConflicts expands a wildcard request to the best package
// for each matched base package name.
func ResolveWildcardPackageConflicts(want string, all []ospackage.PackageInfo) ([]ospackage.PackageInfo, bool) {
	if !isGlobPattern(want) {
		pkg, found := ResolveTopPackageConflicts(want, all)
		if !found {
			return nil, false
		}
		return []ospackage.PackageInfo{pkg}, true
	}

	baseNames := make(map[string]struct{})
	for _, pi := range all {
		cleanName := extractBasePackageNameFromFile(pi.Name)
		if matchPackageRequest(want, cleanName) || matchPackageRequest(want, pi.Name) {
			baseNames[cleanName] = struct{}{}
		}
	}

	if len(baseNames) == 0 {
		return nil, false
	}

	var results []ospackage.PackageInfo
	for baseName := range baseNames {
		pkg, found := ResolveTopPackageConflicts(baseName, all)
		if found {
			results = append(results, pkg)
		}
	}

	if len(results) == 0 {
		return nil, false
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Name == results[j].Name {
			return compareVersions(results[i].Version, results[j].Version) > 0
		}
		return results[i].Name < results[j].Name
	})

	return results, true
}

func extractVersionRequirement(reqVers []string, depName string) (op string, ver string, found bool) {
	for _, reqVer := range reqVers {
		reqVer = strings.TrimSpace(reqVer)

		// Handle alternatives (|) - check if our depName is in any of the alternatives
		alternatives := strings.Split(reqVer, "|")
		for _, alt := range alternatives {
			alt = strings.TrimSpace(alt)

			// Extract the base package name from the requirement
			var baseName string
			if idx := strings.Index(alt, " ("); idx != -1 {
				// Case: "systemd (= 0:255-29.emt3)"
				baseName = strings.TrimSpace(alt[:idx])
			} else if idx := strings.Index(alt, "("); idx != -1 {
				// Case: "python3dist(cryptography)"
				baseName = strings.TrimSpace(alt[:idx])
			} else {
				// Case: no parentheses, just the package name
				baseName = alt
			}

			// Check if this matches our dependency name
			if baseName != depName {
				continue // Skip to next alternative
			}

			// Found our dependency, now extract version constraint
			// Look for version constraint in format: "packagename (operator version)"
			if idx := strings.Index(alt, " ("); idx != -1 {
				verConstraint := alt[idx+2:] // Skip " ("
				if idx2 := strings.Index(verConstraint, ")"); idx2 != -1 {
					verConstraint = verConstraint[:idx2]
				}

				// Split into operator and version
				parts := strings.Fields(verConstraint)
				if len(parts) >= 2 {
					op := parts[0]
					ver := strings.Join(parts[1:], " ") // Join in case version has spaces

					// DO NOT strip epoch - keep the full version including epoch
					// The epoch (e.g., "1:" in "1:3.0.0-9.emt3") is crucial for version comparison

					return op, ver, true
				}
			}

			// If we found the dependency but no version constraint, return found=false
			return "", "", false
		}
	}

	return "", "", false
}

func comparePackageVersions(a, b string) (int, error) {
	// Empty-version handling: empty < any non-empty
	if a == "" && b == "" {
		return 0, nil
	}
	if a == "" {
		return -1, nil
	}
	if b == "" {
		return 1, nil
	}

	// Helper to split epoch
	splitEpoch := func(ver string) (epoch int, rest string) {
		parts := strings.SplitN(ver, ":", 2)
		if len(parts) == 2 {
			if _, err := fmt.Sscanf(parts[0], "%d", &epoch); err != nil {
				epoch = 0
			}
			rest = parts[1]
		} else {
			epoch = 0
			rest = ver
		}
		return
	}

	// Handle epoch first
	epochA, restA := splitEpoch(a)
	epochB, restB := splitEpoch(b)
	if epochA < epochB {
		return -1, nil
	}
	if epochA > epochB {
		return 1, nil
	}

	// After epoch comparison, check if one is a prefix of the other
	// This handles cases like "1.19-1.emt3" vs "1.19"
	if strings.HasPrefix(restA, restB) {
		if restA == restB {
			return 0, nil // exact match
		}
		// restA is longer, check if the next character is a separator
		if len(restA) > len(restB) {
			nextChar := restA[len(restB)]
			if nextChar == '-' || nextChar == '.' || nextChar == '+' || nextChar == '~' {
				return 0, nil // treat as equal since restB is a valid prefix
			}
		}
	}
	if strings.HasPrefix(restB, restA) {
		if restA == restB {
			return 0, nil // exact match
		}
		// restB is longer, check if the next character is a separator
		if len(restB) > len(restA) {
			nextChar := restB[len(restA)]
			if nextChar == '-' || nextChar == '.' || nextChar == '+' || nextChar == '~' {
				return 0, nil // treat as equal since restA is a valid prefix
			}
		}
	}

	// If no prefix match, fall back to detailed comparison
	// Split version string into upstream version and release for RPMat last hyphen
	splitRevision := func(ver string) (upstream string, revision string) {
		if i := strings.LastIndex(ver, "-"); i >= 0 {
			return ver[:i], ver[i+1:]
		}
		return ver, ""
	}

	// nextSegment returns the next contiguous numeric or non-numeric segment.
	nextSegment := func(s string) (seg string, rest string, numeric bool) {
		if s == "" {
			return "", "", false
		}
		// numeric segment
		if s[0] >= '0' && s[0] <= '9' {
			i := 0
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			return s[:i], s[i:], true
		}
		// non-numeric segment
		i := 0
		for i < len(s) && (s[i] < '0' || s[i] > '9') {
			i++
		}
		return s[:i], s[i:], false
	}

	// Character ordering per Debian: '~' < end-of-string < letters < other characters.
	// This ordering is crucial for correct Debian version comparison, as defined in
	// Debian Policy Manual section 5.6.12 ("Version"). See:
	// https://www.debian.org/doc/debian-policy/ch-controlfields.html#version
	charOrder := func(r rune) int {
		if r == '~' {
			return -2
		}
		if r == 0 {
			return -1
		}
		if unicode.IsLetter(r) {
			return int(r)
		}
		return 0x100 + int(r)
	}

	// Compare two non-digit segments using Debian ordering
	compareNonDigitSegments := func(aSeg, bSeg string) int {
		ai, bi := 0, 0
		for {
			var ra, rb rune
			if ai < len(aSeg) {
				ra = rune(aSeg[ai])
			} else {
				ra = 0
			}
			if bi < len(bSeg) {
				rb = rune(bSeg[bi])
			} else {
				rb = 0
			}
			// both ended
			if ra == 0 && rb == 0 {
				return 0
			}
			if ra != rb {
				oa := charOrder(ra)
				ob := charOrder(rb)
				if oa < ob {
					return -1
				}
				return 1
			}
			ai++
			bi++
		}
	}

	// Compare numeric segments (as dpkg: strip leading zeros, compare length, then lexicographically)
	compareNumericSegments := func(aSeg, bSeg string) int {
		aTrim := strings.TrimLeft(aSeg, "0")
		bTrim := strings.TrimLeft(bSeg, "0")
		// treat empty as zero
		if aTrim == "" && bTrim == "" {
			return 0
		}
		if aTrim == "" {
			return -1
		}
		if bTrim == "" {
			return 1
		}
		// longer numeric (more digits) is greater
		if len(aTrim) > len(bTrim) {
			return 1
		}
		if len(aTrim) < len(bTrim) {
			return -1
		}
		// same length -> lexical compare works
		if aTrim > bTrim {
			return 1
		}
		if aTrim < bTrim {
			return -1
		}
		return 0
	}

	// Split upstream and debian revisions
	upA, debA := splitRevision(restA)
	upB, debB := splitRevision(restB)

	// Compare iterative parts (used for upstream version and debian revision)
	compareParts := func(sa, sb string) int {
		for sa != "" || sb != "" {
			// Handle tilde first: '~' sorts before everything (including end-of-string)
			if (len(sa) > 0 && sa[0] == '~') || (len(sb) > 0 && sb[0] == '~') {
				if len(sa) > 0 && sa[0] == '~' && !(len(sb) > 0 && sb[0] == '~') {
					return -1
				}
				if len(sb) > 0 && sb[0] == '~' && !(len(sa) > 0 && sa[0] == '~') {
					return 1
				}
				// both have tilde: consume and continue
				if len(sa) > 0 && sa[0] == '~' && len(sb) > 0 && sb[0] == '~' {
					sa = sa[1:]
					sb = sb[1:]
					continue
				}
			}

			// After tilde handling, if either side is exhausted, the exhausted side is less
			if sa == "" && sb == "" {
				break
			}
			if sa == "" {
				return -1
			}
			if sb == "" {
				return 1
			}

			segA, restASeg, numA := nextSegment(sa)
			segB, restBSeg, numB := nextSegment(sb)

			// both empty segments -> continue
			if segA == "" && segB == "" {
				sa, sb = restASeg, restBSeg
				continue
			}

			// numeric vs non-numeric: numeric < non-numeric
			if numA != numB {
				if numA {
					return -1
				}
				return 1
			}

			// both numeric
			if numA && numB {
				if cmp := compareNumericSegments(segA, segB); cmp != 0 {
					return cmp
				}
			} else { // both non-numeric
				if cmp := compareNonDigitSegments(segA, segB); cmp != 0 {
					return cmp
				}
			}

			sa, sb = restASeg, restBSeg
		}
		return 0
	}

	// Compare upstream versions first, then debian revisions
	if cmp := compareParts(upA, upB); cmp != 0 {
		return cmp, nil
	}
	if cmp := compareParts(debA, debB); cmp != 0 {
		return cmp, nil
	}
	return 0, nil
}

// GenerateSPDXFileName creates a SPDX manifest filename based on repository configuration
func GenerateSPDXFileName(repoNm string) string {
	timestamp := time.Now().Format("20060102_150405")
	SPDXFileNm := filepath.Join("spdx_manifest_rpm_" + strings.ReplaceAll(repoNm, " ", "_") + "_" + timestamp + ".json")
	return SPDXFileNm
}
