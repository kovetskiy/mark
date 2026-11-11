package mark

import (
	"runtime/debug"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// Version returns the mark release linked into the running program, such as
// "v17.1.0" or "v17.2.0-rc.1", read from its build info.
//
// It returns "" when the program carries no release of mark: a
// pseudo-version, a version with build metadata ("+dirty", "+incompatible"),
// a replacement by a local directory or by another module, or a build that
// records no version, as mark's own go build or go test does ("(devel)").
// A replacement by mark itself at another version counts as that version.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	return versionOf(info)
}

// modulePath is mark's module path. A fork that renames the module is not
// mark, and its releases are not mark's.
const modulePath = "github.com/kovetskiy/mark/v17"

// versionOf is Version for the given build info.
func versionOf(info *debug.BuildInfo) string {
	mod := linked(info)
	if mod == nil {
		return ""
	}

	version := mod.Version
	if mod.Replace != nil {
		// A directory has no version, and another module's releases are
		// not mark's.
		if mod.Replace.Path != modulePath {
			return ""
		}
		version = mod.Replace.Version
	}

	if !isRelease(version) {
		return ""
	}

	return version
}

// linked returns mark's module in the build info, as the main module or as
// a dependency, or nil.
func linked(info *debug.BuildInfo) *debug.Module {
	if info.Main.Path == modulePath {
		return &info.Main
	}
	for _, dep := range info.Deps {
		if dep.Path == modulePath {
			return dep
		}
	}

	return nil
}

// isRelease tells whether version names a tag mark could have released:
// canonical semver, no build metadata, and not a pseudo-version, which names
// a commit and looks like a pre-release. Canonical gives "" for anything that
// is not semver.
func isRelease(version string) bool {
	return version != "" &&
		semver.Canonical(version) == version &&
		!module.IsPseudoVersion(version)
}
