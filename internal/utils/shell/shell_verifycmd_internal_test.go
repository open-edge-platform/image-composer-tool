package shell

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVerifyCmdWithFullPath_PreservesMultilineTail is a regression test for a
// bug where verifyCmdWithFullPath rebuilt the whole command via
// strings.Fields/strings.Join, which is blind to quoting and collapses any
// whitespace run — including real newlines inside a quoted bash -c '<script>'
// argument — to a single space. That broke multi-line scripts relying on
// newlines/semicolons as statement separators (if/then/fi, for/do/done),
// producing "syntax error near unexpected token `then'" for a script like
// image-composer-tool's DKMS post-install hook. The fix must still resolve
// the leading command name to its full path, but leave everything after it
// byte-for-byte untouched.
func TestVerifyCmdWithFullPath_PreservesMultilineTail(t *testing.T) {
	tempDir := t.TempDir()
	fakeBashPath := filepath.Join(tempDir, "bash")
	if err := os.WriteFile(fakeBashPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to write fake bash: %v", err)
	}

	originalBashPaths := commandMap["bash"]
	defer func() { commandMap["bash"] = originalBashPaths }()
	commandMap["bash"] = []string{fakeBashPath}

	// Shaped like the real EdgePack DKMS hook: nested double-quoted command
	// substitutions, an if/then/fi block, and a for/do/done block.
	tail := ` -c 'set -e
target_kernel="$(basename "$(ls -d /lib/modules/*-generic 2>/dev/null | sort -V | tail -1)")"
if [ -z "$target_kernel" ]; then
  echo "No target kernel found under /lib/modules" >&2
  exit 1
fi
for mod in edge-gfx edge-edac; do
  dkms status -k "$target_kernel" | grep -q "^${mod}/.*: installed"
done
'`

	got, err := verifyCmdWithFullPath("bash"+tail, HostPath)
	if err != nil {
		t.Fatalf("verifyCmdWithFullPath returned error: %v", err)
	}

	want := fakeBashPath + tail
	if got != want {
		t.Errorf("verifyCmdWithFullPath did not preserve the tail byte-for-byte:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestVerifyCmdWithFullPath_QuoteArgEscapedSeparatorSurvives is a regression
// test for QuoteArg's close-quote/escaped-quote/reopen-quote sequence (used
// whenever the quoted payload itself contains a literal single quote, e.g. a
// script with its own single-quoted printf/echo argument): the resulting
// pattern (close quote, backslash, quote, quote, reopen quote) must not fool
// findSeparatorOutsideQuotes into treating a ';' or '|' that is logically
// still inside the quoted payload (between two escaped-quote boundaries) as
// an outer command separator.
func TestVerifyCmdWithFullPath_QuoteArgEscapedSeparatorSurvives(t *testing.T) {
	tempDir := t.TempDir()
	fakeBashPath := filepath.Join(tempDir, "bash")
	if err := os.WriteFile(fakeBashPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to write fake bash: %v", err)
	}

	originalBashPaths := commandMap["bash"]
	defer func() { commandMap["bash"] = originalBashPaths }()
	commandMap["bash"] = []string{fakeBashPath}

	for _, script := range []string{
		`printf '%s;%s' a b`,
		`printf '%s|%s' a b`,
		// A literal backslash immediately preceding one of the script's own
		// embedded quotes, still inside the OUTER single-quoted region at
		// that point: backslash has no special meaning inside single quotes
		// in real POSIX shells, so treating it as an escape there
		// desynchronizes the quote-toggle tracking for everything after it.
		`printf %s \'; echo ok`,
		`printf %s \'| echo ok`,
	} {
		cmd := "bash -c " + QuoteArg(script)
		got, err := verifyCmdWithFullPath(cmd, HostPath)
		if err != nil {
			t.Fatalf("verifyCmdWithFullPath(%q) returned error: %v", cmd, err)
		}
		want := fakeBashPath + " -c " + QuoteArg(script)
		if got != want {
			t.Errorf("verifyCmdWithFullPath split/mangled a separator embedded in a quoted payload:\ngot:  %q\nwant: %q", got, want)
		}
	}
}

// TestVerifyCmdWithFullPath_RejectsUnquotedNewlineSeparator is a regression
// test for an allowlist bypass: an unquoted newline terminates a shell
// statement exactly like ";" does, but the separator scan did not treat it as
// one, so only the first line's leading token was ever verified against
// commandMap and the whole multi-line string (both lines) still reached the
// final bash -c unmodified — letting a non-allowlisted second command run.
func TestVerifyCmdWithFullPath_RejectsUnquotedNewlineSeparator(t *testing.T) {
	tempDir := t.TempDir()
	fakeAllowedPath := filepath.Join(tempDir, "allowed-command")
	if err := os.WriteFile(fakeAllowedPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to write fake allowed command: %v", err)
	}

	originalAllowedPaths := commandMap["allowed-command"]
	defer func() {
		if originalAllowedPaths == nil {
			delete(commandMap, "allowed-command")
		} else {
			commandMap["allowed-command"] = originalAllowedPaths
		}
	}()
	commandMap["allowed-command"] = []string{fakeAllowedPath}
	delete(commandMap, "unallowlisted-command")

	if _, err := verifyCmdWithFullPath("allowed-command\nunallowlisted-command", HostPath); err == nil {
		t.Fatal("expected verifyCmdWithFullPath to reject the unallowlisted second line separated by an unquoted newline, got no error")
	}
}
