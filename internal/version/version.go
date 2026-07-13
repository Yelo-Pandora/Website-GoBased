// Package version exposes build metadata injected with linker flags.
package version

// Build metadata defaults to development values.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)
