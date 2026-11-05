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
	// The Makefile passes "+dirty" on the commit. An empty value, as
	// "-X main.version=$(git describe)" gives where git names nothing, is
	// taken as unset.
	commit, flagDirty := strings.CutSuffix(commit, "+dirty")
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "none"
	}

	var module, revision string
	modified := false
	if info != nil {
		module, modified = strings.CutSuffix(info.Main.Version, "+dirty")
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				revision = setting.Value
			}
		}
	}

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
	if flagDirty || fromInfo && modified {
		dirty = "+dirty"
	}

	// A "go install ...@v17.0.0" build knows no commit to add.
	if fromInfo && commit == "none" {
		return version + dirty
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
