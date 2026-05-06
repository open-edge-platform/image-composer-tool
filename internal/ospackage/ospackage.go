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
}

// Checksum holds the algorithm and value of a checksum.
type Checksum struct {
	Algorithm string
	Value     string
}
