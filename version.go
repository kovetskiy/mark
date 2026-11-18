package mark

import "runtime/debug"

// modulePath is mark's module path, which build info names mark by.
const modulePath = "github.com/kovetskiy/mark/v17"

// Version returns the version of mark linked into the running program, as Go
// recorded it in the build info: a release such as "v17.1.0", or for a build
// that is not one a pseudo-version, a version with build metadata, or
// "(devel)", which is also what a go.work workspace holding mark gives. A
// replacement by mark at another version gives that version.
//
// It returns "" when the program carries no build info or no mark, or
// replaces mark with a local directory or another module.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	return versionOf(info)
}

// versionOf is Version for the given build info.
func versionOf(info *debug.BuildInfo) string {
	mod := &info.Main
	if mod.Path != modulePath {
		mod = nil
		for _, dep := range info.Deps {
			if dep.Path == modulePath {
				mod = dep
				break
			}
		}
	}

	switch {
	case mod == nil:
		return ""
	case mod.Replace == nil:
		return mod.Version
	case mod.Replace.Path == modulePath:
		return mod.Replace.Version
	default:
		// A directory has no version, and another module's are not mark's.
		return ""
	}
}
