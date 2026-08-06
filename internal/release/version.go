// Package release describes how a portly binary was produced.
//
// The two install channels build differently. A release build sets the version
// through linker flags. A `go install` build sets nothing, and its provenance
// has to be recovered from the module metadata Go embeds. Both must produce a
// truthful answer, and neither may invent a release number it does not have.
package release

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// DevelopmentVersion marks a binary built without a release version.
//
// It is the default value of the linker-set variable, so an unreleased local
// build reports itself as such rather than claiming to be a tagged release.
const DevelopmentVersion = "dev"

// shortCommitLength matches the abbreviated hash git and GitHub display.
const shortCommitLength = 7

// Build is the provenance of a running binary.
type Build struct {
	Version string
	Commit  string
}

// Describe works out what a binary was built from.
//
// linkerVersion and linkerCommit are the values set at release time and are
// empty or DevelopmentVersion otherwise. info is the embedded module metadata,
// which is nil when the binary was not built as a module.
//
// Linker values win when present because a release build knows its own tag. The
// module metadata is the fallback, which is what `go install` produces.
func Describe(linkerVersion, linkerCommit string, info *debug.BuildInfo) Build {
	build := Build{
		Version: strings.TrimSpace(linkerVersion),
		Commit:  shorten(strings.TrimSpace(linkerCommit)),
	}

	if build.Version != "" && build.Version != DevelopmentVersion {
		return build
	}

	moduleVersion, moduleCommit := fromBuildInfo(info)
	if build.Version == "" || build.Version == DevelopmentVersion {
		if moduleVersion != "" {
			build.Version = moduleVersion
		}
	}
	if build.Commit == "" {
		build.Commit = moduleCommit
	}
	if build.Version == "" {
		build.Version = DevelopmentVersion
	}
	return build
}

// fromBuildInfo reads the version and revision Go embeds at build time.
//
// Go records "(devel)" for a build from a working tree rather than from a
// tagged module, which is not a version and is discarded.
func fromBuildInfo(info *debug.BuildInfo) (version, commit string) {
	if info == nil {
		return "", ""
	}

	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			commit = shorten(setting.Value)
			break
		}
	}
	return version, commit
}

// String renders a build for `portly --version`.
func (b Build) String() string {
	if b.Commit == "" {
		return fmt.Sprintf("portly %s", b.Version)
	}
	return fmt.Sprintf("portly %s (%s)", b.Version, b.Commit)
}

// shorten abbreviates a commit hash, leaving anything shorter untouched.
func shorten(commit string) string {
	if len(commit) <= shortCommitLength {
		return commit
	}
	return commit[:shortCommitLength]
}
