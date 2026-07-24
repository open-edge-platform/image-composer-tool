// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package api

import "strings"

// buildPhase is a coarse, ordered stage of an ICT image build, derived by
// matching milestone strings in the build's log output. It drives the UI's
// progress stepper. These are best-effort: ICT has no structured progress
// signal, so we classify from log lines emitted by shared (OS-independent) code
// where possible.
type buildPhase int

const (
	phasePreparing  buildPhase = iota // config load / provider init
	phasePackages                     // resolve + download packages AND build chroot env
	phaseInstalling                   // installing packages into the image
	phaseGenerating                   // assembling the image / SBOM / ISO
	phaseDone                         // build finished successfully
)

// phaseNames maps each phase to a stable id sent to the UI (also used as the
// stepper's ordered key list).
var phaseNames = map[buildPhase]string{
	phasePreparing:  "preparing",
	phasePackages:   "packages",
	phaseInstalling: "installing",
	phaseGenerating: "generating",
	phaseDone:       "done",
}

// phaseMarkers maps a phase to case-insensitive substrings; if any is present in
// a log line, the build has reached at least that phase. Ordered earliest to
// latest so detectPhase can take the max reached.
//
// ICT's flow is NOT linear and offers no clean milestone between "all packages
// downloaded" and "installation begins": package resolve+download runs THREE
// interleaved times (image packages, chroot packages, then — after the chroot
// is built — initrd packages), so "chroot done" is NOT a safe step boundary
// (more downloads follow it). To guarantee a step never turns green while its
// own logs are still streaming, everything up to installation is one "packages"
// phase (resolve + download + chroot build). It advances to "installing" only
// at the authoritative "Installing package X/Y" marker. "Building image:" is
// the START of the image stage, so it is deliberately NOT a generating marker —
// generating is only genuine artifact assembly that always follows install.
var phaseMarkers = []struct {
	phase   buildPhase
	substrs []string
}{
	{phasePreparing, []string{"loaded image template", "merged configuration", "repositories for package download"}},
	{phasePackages, []string{"resolving dependencies for", "fetching packages from user package list", "downloading", "packages to", "all downloads complete", "chroot environment build completed successfully", "packages for chroot environment"}},
	{phaseInstalling, []string{"image package installation", "installing package "}},
	{phaseGenerating, []string{"installation post-processing", "copying sbom", "creating iso", "iso creation completed", "creating image for bios"}},
	{phaseDone, []string{"image build completed successfully"}},
}

// detectPhase returns the id of the furthest phase reached across all log lines.
// Defaults to "preparing" before any marker appears.
func detectPhase(logs []string) string {
	reached := phasePreparing
	for _, line := range logs {
		lower := strings.ToLower(line)
		for _, m := range phaseMarkers {
			if m.phase <= reached {
				continue
			}
			for _, sub := range m.substrs {
				if strings.Contains(lower, sub) {
					reached = m.phase
					break
				}
			}
		}
	}
	return phaseNames[reached]
}

// installProgress extracts the most recent "Installing package X/Y" counter from
// the logs, returning done/total (0,0 if none seen). Lets the UI show a real
// percentage during the install phase.
func installProgress(logs []string) (done, total int) {
	const marker = "installing package "
	for _, line := range logs {
		lower := strings.ToLower(line)
		i := strings.Index(lower, marker)
		if i < 0 {
			continue
		}
		// Parse "X/Y" immediately after the marker, e.g. "Installing package 42/128:".
		rest := line[i+len(marker):]
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			continue
		}
		d := atoiSafe(rest[:slash])
		// Total runs until a non-digit (colon, space, etc.).
		tEnd := slash + 1
		for tEnd < len(rest) && rest[tEnd] >= '0' && rest[tEnd] <= '9' {
			tEnd++
		}
		t := atoiSafe(rest[slash+1 : tEnd])
		if d > 0 && t > 0 {
			done, total = d, t
		}
	}
	return done, total
}

// atoiSafe parses a leading integer from s, returning 0 on any junk.
func atoiSafe(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
