// Package buildinfo holds what the linker stamps into the controller binary.
package buildinfo

// Set with -ldflags "-X github.com/this-is-tobi/ohmlab/utils/internal/buildinfo.Version=...".
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
