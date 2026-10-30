// Package oepinstaller parses and validates the output of
// `openedge-cli list --sbom`, the interface through which the OEP Installer
// reports the Debian packages and apt repositories its modules need. See
// docs/architecture-decision-record/oep-installer-sbom-interface.md.
package oepinstaller

import "strings"

// SchemaVersion is the output schema version this package supports.
const SchemaVersion = "1"

// PackageKind classifies a pkg-list entry by its syntax.
type PackageKind int

const (
	// PackageName is a plain package name, e.g. "curl".
	PackageName PackageKind = iota
	// PackageExactVersion is name=version, e.g. "libze1=1.32.0-1".
	PackageExactVersion
	// PackageVersionPrefix is name=version*: the newest version starting with
	// version, e.g. "intel-edge-base-standard=2026.3*".
	PackageVersionPrefix
	// PackageGlob is a glob over package names, e.g. "intel-oneapi-runtime-*".
	PackageGlob
	// PackageURL is an http(s) URL of an artifact outside apt repositories,
	// which openedge-cli installs on first boot.
	PackageURL
	// PackageRemoval is -name: the module needs the package not installed.
	PackageRemoval
)

// kernelPackagePrefixes identify kernel packages, which the image template
// selects rather than the OEP Installer.
var kernelPackagePrefixes = []string{"linux-image-", "linux-headers-", "linux-modules-", "linux-generic"}

// Package is one pkg-list entry.
type Package struct {
	Entry   string      // the entry as printed
	Name    string      // package name or glob; empty for PackageURL
	Version string      // version for PackageExactVersion, prefix for PackageVersionPrefix
	Kind    PackageKind // syntax of the entry
}

// IsKernel reports whether the entry names kernel packages.
func (p Package) IsKernel() bool {
	if p.Kind == PackageURL || p.Kind == PackageRemoval {
		return false
	}
	for _, prefix := range kernelPackagePrefixes {
		if strings.HasPrefix(p.Name, prefix) {
			return true
		}
	}
	return false
}

// Repository is an apt repository group from a component document.
type Repository struct {
	Name              string   // repo-name, may be empty
	GPGKey            string   // URL of the signing key
	GPGKeyFingerprint string   // optional OpenPGP fingerprint of the key
	KeyFile           string   // in-image path of the dearmored keyring
	Source            string   // the deb line, as printed
	SourceSrc         string   // optional deb-src line, as printed
	ListFile          string   // in-image path of the sources list file
	Preferences       []string // apt_preferences(5) lines, may be empty
	PreferencesFile   string   // in-image path of the preferences file
	URI               string   // repository URI from Source
	Suite             string   // suite (codename) from Source
	Component         string   // single component from Source
	Architectures     []string // arch= option from Source, may be empty
}

// Output is the parsed and validated `openedge-cli list --sbom` output.
type Output struct {
	CLIVersion    string       // header cli-version
	CLIURL        string       // header cli-url, may be empty
	Modules       []string     // header module-list
	Arguments     []string     // header arguments, nil when absent
	Packages      []Package    // pkg-list entries, in output order
	Repositories  []Repository // repository groups, in output order
	KernelCmdline []string     // kernel-cmdline entries, in output order
}
