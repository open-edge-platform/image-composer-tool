package overlay

// maxDpkgArgBytes caps how many bytes of quoted artifact paths go into a single
// dpkg command line.
//
// Commands here are assembled as one string and executed as `bash -c "<cmd>"`,
// which makes the entire command a single argv entry. Linux limits one argument
// to MAX_ARG_STRLEN = PAGE_SIZE * 32 = 128 KiB (on 4 KiB pages) — far below
// ARG_MAX (typically 2 MiB) — and execve fails with E2BIG / "argument list too
// long" past it. Observed in the wild: 2004 ROS 2 artifacts produced a ~142 KiB
// command line and the install failed before dpkg started.
//
// 96 KiB leaves ~32 KiB of headroom for everything the executor prepends to the
// same string: `sudo `, the DEBIAN_FRONTEND/DEBCONF_* environment assignments, any
// injected proxy variables, `chroot <path> `, and the absolute-path rewrite of
// dpkg itself. The limit is per-argument, not per-list, so a conservative budget
// costs only an extra batch or two.
const maxDpkgArgBytes = 96 * 1024

// chunkArgs splits pre-quoted command arguments into batches whose joined length
// (including the single separating space between entries) stays within budget.
//
// An argument longer than the budget on its own is placed in a batch by itself
// rather than dropped or truncated: dropping it would silently skip a package,
// and the caller must see the real execve failure instead of a mystery omission.
// A nil or empty input yields a single empty batch so callers still run once and
// preserve existing behavior for the no-artifact case.
func chunkArgs(args []string, budget int) [][]string {
	if len(args) == 0 {
		return [][]string{nil}
	}

	var chunks [][]string
	var current []string
	currentLen := 0
	for _, arg := range args {
		// +1 for the space that will join this entry to the previous one.
		addition := len(arg)
		if len(current) > 0 {
			addition++
		}
		if len(current) > 0 && currentLen+addition > budget {
			chunks = append(chunks, current)
			current, currentLen = nil, 0
			addition = len(arg)
		}
		current = append(current, arg)
		currentLen += addition
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}
