package main

import (
	"runtime/debug"
	"strings"
)

// buildVersion is what "mark --version" says. The version and commit given
// with -ldflags win; Go's build info stands in for whichever the flags left
// unset, so a go install or plain go build still names its release. A
// pseudo-version already ends in the commit, so the commit is not repeated.
func buildVersion(version, commit string, info *debug.BuildInfo) string {
	if info == nil {
		return version + "@" + commit
	}

	var revision string
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			revision = setting.Value
		}
	}

	module, modified := strings.CutSuffix(info.Main.Version, "+dirty")
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
	if len(commit) >= 12 && strings.HasSuffix(version, "-"+commit[:12]) {
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
