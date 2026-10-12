package shell

import (
	"sort"
	"strings"
	"sync"
)

// proxyReportOnce keeps the proxy summary to a single line per build rather than one
// per chrooted command (a large template runs hundreds).
var proxyReportOnce sync.Once

// ProxyEnvNames returns the sorted NAMES of the proxy variables that will be
// forwarded into chroot/sudo commands. Names only — the values routinely embed
// credentials, and build.go deliberately redacts errors for the same reason.
func ProxyEnvNames() []string {
	proxyEnv := GetOSProxyEnvirons()
	names := make([]string, 0, len(proxyEnv))
	for key := range proxyEnv {
		names = append(names, key)
	}
	sort.Strings(names)
	return names
}

// ReportProxyEnvOnce logs, once per process, which proxy variables the build will
// forward into chrooted commands.
//
// A build whose network egress requires a proxy fails in ways that look nothing like
// a proxy problem: a direct connection to a public host returns NXDOMAIN or a reset
// long before any retry logic engages, so the command reports a bare non-zero exit.
// Stating the forwarded set up front makes "no proxy configured" visible at the top of
// the log instead of inferrable only from a downstream symptom.
func ReportProxyEnvOnce() {
	proxyReportOnce.Do(func() {
		names := ProxyEnvNames()
		if len(names) == 0 {
			log.Warn("No proxy environment variables set; chrooted commands will connect directly. " +
				"Builds that must reach the public internet through a proxy will fail here.")
			return
		}
		log.Infof("Forwarding proxy environment into chrooted commands: %s", strings.Join(names, ", "))
	})
}

// FormatCommandOutput renders a command's captured output as an indented block to be
// appended to a one-line error, or "" when there is nothing to show.
//
// Errors from ExecCmd and friends carry only the process exit status ("exit status
// 4"), which on its own says nothing about why a command failed. Callers that discard
// the returned output leave operators with no diagnostic at all — appending this to
// the wrapped error is what makes a chroot failure explicable from the build log.
func FormatCommandOutput(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	// Indent every line by two spaces so the diagnostic reads as a distinct block
	// beneath the one-line error rather than blending into it.
	indented := "  " + strings.ReplaceAll(out, "\n", "\n  ")
	return "\n" + indented
}
