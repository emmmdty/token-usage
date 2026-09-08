// Package version reports build information for the binary.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// These are overridden via ldflags by goreleaser. When building with
// `go install` (no ldflags), the defaults fall back to the module version
// embedded by the Go toolchain, so the reported version stays accurate.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func GetVersionInfo() string {
	return fmt.Sprintf("token-usage %s (commit: %s, built: %s)", Version, Commit, Date)
}

// init falls back to module build info when ldflags were not injected.
func init() {
	if Version != "dev" {
		return
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	version := info.Main.Version
	if version == "" || version == "(devel)" {
		return
	}

	// v0.2.2 -> 0.2.2
	version = strings.TrimPrefix(version, "v")
	Version = version

	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			Commit = s.Value
		}
		if s.Key == "vcs.time" && s.Value != "" {
			Date = s.Value
		}
	}
}
