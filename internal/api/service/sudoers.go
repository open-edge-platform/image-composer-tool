// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// SudoersSpec captures everything needed to render the sudoers drop-in that lets
// a non-root `serve --sudo` process build, cancel, and read build artifacts as
// root — and nothing else.
type SudoersSpec struct {
	User      string // service account that runs `serve` (unqualified login name)
	ICTPath   string // absolute path to the image-composer-tool binary
	KillCmd   string // absolute path sudo will resolve `kill` to (for cancellation)
	CatCmd    string // absolute path sudo will resolve `cat` to (for reading artifacts)
	BuildsDir string // absolute per-build root (<work-dir>/builds); the cat rule is scoped here
}

// SudoersDropInName is the recommended filename under /etc/sudoers.d for the
// generated rules. It contains no dots or tildes (sudo ignores such files).
const SudoersDropInName = "image-composer-tool-webui"

// ResolveSudoersSpec builds a SudoersSpec for the current user and the given ICT
// binary and work-dir paths, resolving the kill/cat helper paths the same way
// sudo will at runtime. ictBinary and workDir may be relative or empty; they are
// resolved to absolute paths (empty ictBinary falls back to discovery, empty
// workDir to the serve default) so the generated rules match what the server
// actually execs and reads — sudo matches the command literally, so a relative
// path would never match an absolute NOPASSWD rule.
func ResolveSudoersSpec(ictBinary, workDir string) (SudoersSpec, error) {
	u, err := user.Current()
	if err != nil {
		return SudoersSpec{}, fmt.Errorf("determining current user: %w", err)
	}
	// A root-owned server needs no sudoers rules at all; generating a rule for
	// "root" would be meaningless and misleading.
	if u.Uid == "0" {
		return SudoersSpec{}, fmt.Errorf("running as root: no sudoers rules are needed " +
			"(the server signals and reads build artifacts directly)")
	}

	if ictBinary == "" {
		ictBinary = discoverICTBinary()
	}
	ictAbs, err := filepath.Abs(ictBinary)
	if err != nil {
		return SudoersSpec{}, fmt.Errorf("resolving ICT binary path %q: %w", ictBinary, err)
	}

	// Match New's default so the generated cat rule covers where artifacts
	// actually land when the operator doesn't pass --work-dir.
	if workDir == "" {
		workDir = "webui-workspace"
	}
	workAbs, err := filepath.Abs(workDir)
	if err != nil {
		return SudoersSpec{}, fmt.Errorf("resolving work dir %q: %w", workDir, err)
	}
	// Builds — and therefore all artifacts the server reads with `sudo cat` — live
	// under <work-dir>/builds/<id>/... (see StartBuild and history.go). Scope
	// the cat rule to that subtree, not the whole work dir, so the service user
	// can't read unrelated root-owned files placed elsewhere under the work dir.
	buildsDir := filepath.Join(workAbs, "builds")

	return SudoersSpec{
		User:      u.Username,
		ICTPath:   ictAbs,
		KillCmd:   resolveSudoHelper("kill"),
		CatCmd:    resolveSudoHelper("cat"),
		BuildsDir: buildsDir,
	}, nil
}

// resolveSudoHelper returns the absolute path a sudo invocation of the given
// helper (kill/cat) will resolve to. The server runs `sudo -n kill ...`, and
// sudo resolves a bare command name against *its own* secure_path — set in
// /etc/sudoers, independent of the caller's $PATH — so the generated rule must
// name the path sudo will pick, not one found on the invoker's $PATH (which
// could point at a writable/unsafe directory).
//
// We therefore query sudo's effective secure_path and probe it in order, falling
// back to the compiled-in default (the sudo default is
// /usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin). We deliberately
// do NOT fall back to exec.LookPath: matching the caller's $PATH is exactly the
// mismatch (and the unsafe-directory risk) this function exists to avoid. If the
// helper isn't found on any secure_path entry we return /usr/bin/<name> so the
// rule still names a concrete absolute path (sudoers rules require one) and a
// genuinely missing helper surfaces at runtime rather than silently.
func resolveSudoHelper(name string) string {
	for _, dir := range sudoSecurePath() {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return filepath.Join("/usr/bin", name)
}

// sudoSecurePath returns the directories sudo searches for a bare command name,
// in order. It asks the local sudo for its effective secure_path
// (`sudo -n -l`), which reflects any site override, and falls back to sudo's
// compiled-in default when that can't be read (no passwordless sudo, sudo
// absent, or secure_path not printed).
func sudoSecurePath() []string {
	// sudo's compiled-in default secure_path.
	def := []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"}

	out, err := exec.Command("sudo", "-n", "-l").CombinedOutput()
	if err != nil {
		return def
	}
	if dirs := parseSecurePath(string(out)); len(dirs) > 0 {
		return dirs
	}
	return def
}

// parseSecurePath extracts the secure_path directory list from `sudo -l` output,
// or nil if none is present. Split out from sudoSecurePath so it can be tested
// against real sudo formatting without invoking sudo.
func parseSecurePath(out string) []string {
	// Look for a line like: "Matching Defaults entries ... secure_path=/usr/bin:/bin"
	// or "    secure_path=...". Parse the value after "secure_path=".
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, "secure_path=")
		if i < 0 {
			continue
		}
		val := line[i+len("secure_path="):]
		// `sudo -l` escapes the ':' path separators as '\:' and lists the value
		// among other comma-separated Defaults tokens (e.g.
		//   secure_path=/usr/local/bin\:/usr/bin\:/bin, use_pty
		// ). Unescape the '\:' first, then cut at the first delimiter that ends the
		// value (a space or an *unescaped* comma) so trailing tokens like
		// "use_pty" don't leak in.
		val = strings.ReplaceAll(val, `\:`, ":")
		val = strings.Trim(val, `" `)
		if c := strings.IndexAny(val, " ,"); c >= 0 {
			val = val[:c]
		}
		var dirs []string
		for _, d := range strings.Split(val, ":") {
			// Strip any stray leading backslash (some sudo builds escape other
			// characters too) and surrounding space.
			d = strings.TrimSpace(strings.TrimPrefix(d, `\`))
			if d != "" {
				dirs = append(dirs, d)
			}
		}
		if len(dirs) > 0 {
			return dirs
		}
	}
	return nil
}

// Render returns the sudoers drop-in content for this spec: three scoped
// NOPASSWD rules and an explanatory header. The content is safe to write to
// /etc/sudoers.d/<SudoersDropInName> (validate with `visudo -cf` first).
//
// The three rules are exactly the privileged operations `serve --sudo` performs:
//   - build:  run the image-composer-tool build (needs root for chroot/mount)
//   - kill:   TERM a build's process group to cancel it (root-owned group)
//   - cat:    read a completed build's artifacts (root-owned output files),
//     scoped to the <work-dir>/builds subtree — not any path on the host
//
// Each is scoped to a single command; the service user gets no blanket sudo.
func (s SudoersSpec) Render() string {
	var b strings.Builder
	b.WriteString("# image-composer-tool web UI (serve --sudo) — scoped passwordless sudo.\n")
	b.WriteString("# Generated by `image-composer-tool serve --print-sudoers`.\n")
	b.WriteString("# Grants the service user root for ONLY these three commands — not blanket sudo.\n")
	b.WriteString("#\n")
	b.WriteString("# SECURITY NOTE on the kill rule: a build's process-group id is not known\n")
	b.WriteString("# ahead of time, so the rule cannot pin it. This grants the service user the\n")
	b.WriteString("# ability to send SIGTERM (only TERM) to ANY process group as root. Accept\n")
	b.WriteString("# this deliberately, or run the server as root on an isolated build host\n")
	b.WriteString("# (no rules needed) instead. See web/README.md.\n")
	b.WriteString("#\n")
	fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s build *\n", s.User, s.ICTPath)
	fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s -TERM -[0-9]*\n", s.User, s.KillCmd)
	// cat is scoped to the builds subtree (sudo's `*` spans '/'), so the service
	// user can read build artifacts but not arbitrary root-owned files.
	fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s %s/*\n", s.User, s.CatCmd, s.BuildsDir)
	return b.String()
}
