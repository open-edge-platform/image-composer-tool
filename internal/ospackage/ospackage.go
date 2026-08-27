package ospackage

// PackageInfo holds everything you need to fetch + verify one artifact.
type PackageInfo struct {
	Name             string // e.g. file name "abseil-cpp.rpm"
	Type             string // e.g. "rpm", "deb", "apk"
	Description      string // e.g. "Abseil C++ Common Libraries"
	Origin           string // e.g. "Intel", the vendor or supplier of the package
	License          string // e.g. "Apache-2.0"
	Version          string // e.g. "7.88.1-10+deb12u5"
	Arch             string // e.g. "x86_64", "noarch", "src"
	URL              string // download URL
	Checksums        []Checksum
	Provides         []string // capabilities this package provides (rpm:entry names)
	Requires         []string // capabilities this package requires
	RequiresVer      []string // version constraints for the required capabilities
	RequiresPkgNames []string // canonical package names of dependencies (extracted from Requires)
	Files            []string // list of files in this package (rpm:files)
	PkgName          string   // name of the package
	Breaks           []string // deb Breaks: field terms (raw "name (op ver)" strings)
	// InstalledSizeBytes is the estimated on-disk footprint of the package once
	// unpacked, in bytes (deb Installed-Size × 1024, rpm <size installed=…>).
	// Meaningful only when HasInstalledSize is true — the repository metadata may
	// legitimately report a real zero footprint, which is not the same as not
	// reporting a size at all. Used to estimate the disk space an overlay build
	// needs before growing the baseline.
	InstalledSizeBytes int64
	// HasInstalledSize reports whether the repository metadata included a parsable
	// installed size for this package. False for a missing, malformed, or
	// overflow-guarded value; InstalledSizeBytes is meaningless when false.
	HasInstalledSize bool
}

// Checksum holds the algorithm and value of a checksum.
type Checksum struct {
	Algorithm string
	Value     string
}
