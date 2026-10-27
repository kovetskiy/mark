package main

import (
	"runtime/debug"
	"strings"
)

// buildVersion is what "mark --version" says: the version and commit the
// build was given with -ldflags, as the Makefile and the release builds give
// them. A "go install ...@v17.0.0" build is given neither, so the module
// version and the VCS revision Go records stand in for whichever the flags
// left at their defaults. "(devel)" is no version, and keeps "dev".
//
// A build in a checkout gets a pseudo-version, which ends in the commit
// already, so the commit is not repeated after it. A checkout with changes
// that are not committed ends in "+dirty", wherever Go put it.
func buildVersion(version, commit string, info *debug.BuildInfo) string {
	if info == nil {
		return version + "@" + commit
	}

	var revision string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	module := strings.TrimSuffix(info.Main.Version, "+dirty")
	fromInfo := false
	if version == "dev" && module != "" && module != "(devel)" {
		version = module
		fromInfo = true
	}
	if commit == "none" && revision != "" {
		commit = revision
		fromInfo = true
	}

	dirty := ""
	if fromInfo && modified {
		dirty = "+dirty"
	}

	// A pseudo-version ends in the commit's first 12 characters.
	if version == module && commit == revision && len(revision) >= 12 &&
		strings.HasSuffix(module, "-"+revision[:12]) {
		return version + dirty
	}

	return version + "@" + commit + dirty
}

// readBuildInfo is debug.ReadBuildInfo, as buildVersion takes it.
func readBuildInfo() *debug.BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}

	return info
}
