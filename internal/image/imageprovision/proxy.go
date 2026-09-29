package imageprovision

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/config"
)

// In-image files written for the proxy configuration.
const (
	environmentFile  = "etc/environment"
	aptProxyFile     = "etc/apt/apt.conf.d/95ict-proxy"
	systemdProxyFile = "etc/systemd/system.conf.d/90-ict-proxy.conf"
)

// aptHostRe matches the plain host names and IPv4 addresses that can be
// written as an unquoted apt.conf key.
var aptHostRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

type proxyVar struct {
	name, value string
}

// proxyVars returns the configured variables in a fixed order.
func proxyVars(p config.ProxyConfig) []proxyVar {
	all := []proxyVar{
		{"http_proxy", p.HTTPProxy},
		{"https_proxy", p.HTTPSProxy},
		{"ftp_proxy", p.FTPProxy},
		{"no_proxy", p.NoProxy},
	}
	vars := make([]proxyVar, 0, len(all))
	for _, v := range all {
		if v.value != "" {
			vars = append(vars, v)
		}
	}
	return vars
}

// WriteProxyConfig persists the proxy for login sessions (/etc/environment,
// read by pam_env), for apt (Acquire::*::Proxy), and for every systemd
// service (DefaultEnvironment), which covers cloud-init and the ICT
// provisioning units. Both lower- and upper-case variable names are set
// because tools disagree on which they read.
func WriteProxyConfig(installRoot string, p config.ProxyConfig) error {
	if p.IsEmpty() {
		return nil
	}
	vars := proxyVars(p)
	return withRoot(installRoot, func(root *os.Root) error {
		existing, err := readFileIfExists(root, environmentFile)
		if err != nil {
			return err
		}
		if err := writeFile(root, environmentFile, []byte(renderEnvironment(existing, vars)), 0644); err != nil {
			return err
		}
		if apt := renderAptProxy(p); apt != "" {
			if err := writeFile(root, aptProxyFile, []byte(apt), 0644); err != nil {
				return err
			}
		}
		if err := writeFile(root, systemdProxyFile, []byte(renderSystemdProxy(vars)), 0644); err != nil {
			return err
		}
		log.Infof("Configured system proxy settings")
		return nil
	})
}

// renderEnvironment replaces any existing proxy assignments in
// /etc/environment (e.g. PATH stays untouched) and appends the configured ones.
func renderEnvironment(existing string, vars []proxyVar) string {
	var lines []string
	for _, line := range strings.Split(existing, "\n") {
		if line == "" || isProxyAssignment(line) {
			continue
		}
		lines = append(lines, line)
	}
	for _, v := range vars {
		lines = append(lines,
			fmt.Sprintf("%s=\"%s\"", v.name, v.value),
			fmt.Sprintf("%s=\"%s\"", strings.ToUpper(v.name), v.value))
	}
	return strings.Join(lines, "\n") + "\n"
}

func isProxyAssignment(line string) bool {
	name, _, ok := strings.Cut(strings.TrimSpace(line), "=")
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimPrefix(name, "export ")) {
	case "http_proxy", "https_proxy", "ftp_proxy", "no_proxy":
		return true
	}
	return false
}

// renderAptProxy returns the apt.conf fragment. apt ignores no_proxy, so
// plain host names from noProxy get explicit DIRECT entries; domain
// suffixes and CIDRs cannot be expressed in apt.conf and are skipped.
func renderAptProxy(p config.ProxyConfig) string {
	var b strings.Builder
	for _, e := range []struct{ scheme, url string }{
		{"http", p.HTTPProxy}, {"https", p.HTTPSProxy}, {"ftp", p.FTPProxy},
	} {
		if e.url != "" {
			fmt.Fprintf(&b, "Acquire::%s::Proxy \"%s\";\n", e.scheme, e.url)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	// apt's https method falls back to the http proxy, so both always get
	// exclusions; ftp does not, so it gets them only when it has a proxy.
	schemes := []string{"http", "https"}
	if p.FTPProxy != "" {
		schemes = append(schemes, "ftp")
	}
	for _, host := range strings.Split(p.NoProxy, ",") {
		host = strings.TrimSpace(host)
		if !aptHostRe.MatchString(host) {
			continue
		}
		for _, scheme := range schemes {
			fmt.Fprintf(&b, "Acquire::%s::Proxy::%s \"DIRECT\";\n", scheme, host)
		}
	}
	return b.String()
}

func renderSystemdProxy(vars []proxyVar) string {
	var assignments []string
	for _, v := range vars {
		assignments = append(assignments,
			fmt.Sprintf("\"%s=%s\"", v.name, v.value),
			fmt.Sprintf("\"%s=%s\"", strings.ToUpper(v.name), v.value))
	}
	return "[Manager]\nDefaultEnvironment=" + strings.Join(assignments, " ") + "\n"
}
