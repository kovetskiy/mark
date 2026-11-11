package mark

import (
	"reflect"
	"runtime/debug"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// ImageRepository is the container image repository mark's releases are
// published to, on Docker Hub. It holds mark with the headless Chrome its
// diagrams and formulas are drawn in.
const ImageRepository = "docker.io/kovetskiy/mark"

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

// Image returns the container image published for the mark release linked
// into the running program, such as "docker.io/kovetskiy/mark:v17.1.0", or ""
// when Version returns "". A program that runs mark's image in CI can use it
// to run the image of the very release it was built with.
//
// CI publishes an image under each release tag, pre-releases included, so the
// image is ImageRepository tagged with Version.
func Image() string {
	return imageOf(Version())
}

// modulePath returns mark's module path, which a major version changes: this
// package sits at the module root.
func modulePath() string {
	return reflect.TypeFor[Config]().PkgPath()
}

// versionOf is Version for the given build info.
func versionOf(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}

	path := modulePath()
	mod := &info.Main
	if mod.Path != path {
		mod = nil
		for _, dep := range info.Deps {
			if dep.Path == path {
				mod = dep

				break
			}
		}
	}
	if mod == nil {
		return ""
	}

	version := mod.Version
	if mod.Replace != nil {
		// A directory has no version, and another module's releases are
		// not mark's.
		if mod.Replace.Path != path {
			return ""
		}
		version = mod.Replace.Version
	}

	if !isRelease(version) {
		return ""
	}

	return version
}

// isRelease tells whether version names a tag mark could have released:
// canonical semver, no build metadata, and not a pseudo-version, which names
// a commit and looks like a pre-release.
func isRelease(version string) bool {
	return semver.IsValid(version) &&
		semver.Canonical(version) == version &&
		!module.IsPseudoVersion(version)
}

// imageOf is Image for the given version.
func imageOf(version string) string {
	if version == "" {
		return ""
	}

	return ImageRepository + ":" + version
}
