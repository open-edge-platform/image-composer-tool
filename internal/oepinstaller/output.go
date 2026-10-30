// Package oepinstaller parses and validates the output of
// `openedge-cli list --sbom`, the interface through which the OEP Installer
// reports the Debian packages and apt repositories its modules need. See
// docs/architecture-decision-record/oep-installer-sbom-interface.md.
package oepinstaller

import (
	"regexp"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/ospackage/debutils"
)

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

// KernelRole classifies a kernel-related pkg-list entry. The image template
// selects the kernel, so ICT checks these entries against it instead of
// installing them as ordinary packages.
type KernelRole int

const (
	// KernelNone is not a kernel package.
	KernelNone KernelRole = iota
	// KernelImage is a kernel image for a release, e.g. linux-image-7.0.0-38-generic.
	KernelImage
	// KernelMeta is a meta-package without a full release, e.g.
	// linux-image-generic-7.0, which would pull in the newest kernel it names.
	KernelMeta
	// KernelExtra is headers, modules, or tools for a release, e.g.
	// linux-headers-7.0.0-38-generic, installed for the template's kernel.
	KernelExtra
)

// kernelReleaseRe matches a name ending in a kernel release, such as
// "7.0.0-38-generic" or "6.18-intel": major.minor[.patch] and a suffix.
var kernelReleaseRe = regexp.MustCompile(`(?:^|-)(\d+\.\d+(?:\.\d+)?-[A-Za-z0-9.+~-]+)$`)

// kernelSeriesRe extracts the series from a meta-package such as
// linux-image-generic-7.0 (flavour, then major.minor).
var kernelSeriesRe = regexp.MustCompile(`^linux-(?:image|headers|modules|tools)-[a-z][a-z0-9]*-(\d+\.\d+)$`)

// Package is one pkg-list entry.
type Package struct {
	Entry   string      // the entry as printed
	Name    string      // package name or glob; empty for PackageURL
	Version string      // version for PackageExactVersion, prefix for PackageVersionPrefix
	Kind    PackageKind // syntax of the entry
}

// IsKernel reports whether the entry names kernel packages.
func (p Package) IsKernel() bool {
	return p.KernelRole() != KernelNone
}

// KernelRole classifies the entry. linux-image-* and linux-generic* names are
// kernel images when they end in a release and meta-packages otherwise.
// linux-headers-*, linux-modules-*, and linux-tools-* names are extras when
// they end in a release, meta-packages when they name a flavour (for example
// linux-headers-generic), and ordinary packages otherwise (linux-tools-common).
func (p Package) KernelRole() KernelRole {
	if p.Kind == PackageURL || p.Kind == PackageRemoval {
		return KernelNone
	}
	release := kernelReleaseRe.MatchString(p.Name)
	switch {
	case strings.HasPrefix(p.Name, "linux-image-"):
		if release {
			return KernelImage
		}
		return KernelMeta
	case strings.HasPrefix(p.Name, "linux-generic"):
		return KernelMeta
	case strings.HasPrefix(p.Name, "linux-headers-"), strings.HasPrefix(p.Name, "linux-modules-"),
		strings.HasPrefix(p.Name, "linux-tools-"):
		if release {
			return KernelExtra
		}
		if strings.HasSuffix(p.Name, "-common") {
			return KernelNone
		}
		return KernelMeta
	}
	return KernelNone
}

// KernelRelease returns the release a KernelImage or KernelExtra entry names,
// for example "7.0.0-38-generic".
func (p Package) KernelRelease() string {
	if role := p.KernelRole(); role != KernelImage && role != KernelExtra {
		return ""
	}
	return kernelReleaseRe.FindStringSubmatch(p.Name)[1]
}

// KernelSeries returns the series a KernelMeta entry names, for example "7.0"
// for linux-image-generic-7.0, or "" when the name has none.
func (p Package) KernelSeries() string {
	if p.KernelRole() != KernelMeta {
		return ""
	}
	if m := kernelSeriesRe.FindStringSubmatch(p.Name); m != nil {
		return m[1]
	}
	return ""
}

// Repository is an apt repository group from a component document.
type Repository struct {
	Name              string         // repo-name, may be empty
	GPGKey            string         // URL of the signing key
	GPGKeyFingerprint string         // optional OpenPGP fingerprint of the key
	KeyFile           string         // in-image path of the dearmored keyring
	Source            string         // the deb line, as printed
	SourceSrc         string         // optional deb-src line, as printed
	ListFile          string         // in-image path of the sources list file
	Preferences       []string       // apt_preferences(5) lines, may be empty
	PreferencesFile   string         // in-image path of the preferences file
	URI               string         // repository URI from Source
	Suite             string         // suite (codename) from Source
	Component         string         // single component from Source
	Architectures     []string       // arch= option from Source, may be empty
	Pins              []debutils.Pin // stanzas of Preferences, applied when resolving packages
}

// Pins returns the apt preferences of every repository in out, or nil when out
// is nil.
func Pins(out *Output) []debutils.Pin {
	if out == nil {
		return nil
	}
	var pins []debutils.Pin
	for _, r := range out.Repositories {
		pins = append(pins, r.Pins...)
	}
	return pins
}

// Output is the parsed and validated `openedge-cli list --sbom` output.
type Output struct {
	CLIVersion     string       // header cli-version
	CLIURL         string       // header cli-url, may be empty
	Modules        []string     // header module-list
	InstallCmdline []string     // header install-cmdline, nil when absent
	Arguments      []string     // arguments of InstallCmdline after the modules, nil when absent
	Packages       []Package    // pkg-list entries, in output order
	Repositories   []Repository // repository groups, in output order
	KernelCmdline  []string     // kernel-cmdline entries, in output order
}
