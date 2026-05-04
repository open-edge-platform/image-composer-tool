package version

// Package metadata information, used for versioning and metadata generation
// Earthly automatically replaces these variables during the build process.
var (
	Version      = "0.1.0"                   // Version of the Image Composer Tool
	Toolname     = "Image-Composer-Tool-dev" // Name of the tool
	Organization = "unknown"                 // Organization that built the tool
	BuildDate    = "unknown"                 // Date when the tool was built
	CommitSHA    = "unknown"                 // Commit SHA of the tool
)
