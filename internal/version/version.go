// Package version holds build-time version information injected via
// ldflags. The Makefile sets buildVersion, buildCommit, and buildDate
// using -X linker flags; downstream builds and CI can substitute any
// values they choose.
package version

import (
	"runtime"
	"strings"
)

// buildVersion, buildCommit, and buildDate are set at build time via
// -ldflags -X.
var (
	buildVersion string
	buildCommit  string
	buildDate    string
)

// Version returns the build version, or "(devel)" if unset.
func Version() string {
	if buildVersion == "" {
		return "(devel)"
	}
	return buildVersion
}

// Commit returns the full commit SHA, or "unknown" if unset.
func Commit() string {
	if buildCommit == "" {
		return "unknown"
	}
	return buildCommit
}

// Date returns the build date, or "unknown" if unset.
func Date() string {
	if buildDate == "" {
		return "unknown"
	}
	return buildDate
}

// GoVersion returns the Go toolchain version used for the build.
func GoVersion() string {
	return runtime.Version()
}

// Platform returns the OS/architecture pair.
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// String returns human-readable build information suitable for
// --version output. Components that were not set via ldflags are
// omitted rather than showing placeholder values.
func String() string {
	var parts []string

	parts = append(parts, Version())

	if buildCommit != "" {
		parts = append(parts, buildCommit)
	}
	if buildDate != "" {
		parts = append(parts, buildDate)
	}

	parts = append(parts, GoVersion(), Platform())

	return strings.Join(parts, " ")
}
