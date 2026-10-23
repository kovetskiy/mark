package main

import (
	"fmt"
	"runtime/debug"
)

// buildVersion is what "mark --version" says: the version and commit the
// build was given with -ldflags, as the Makefile and the release builds give
// them. A "go install ...@v17.0.0" build is given neither, so the module
// version and the VCS revision Go records stand in for whichever the flags
// left at their defaults. "(devel)" is no version, and keeps "dev".
func buildVersion(version, commit string, info *debug.BuildInfo) string {
	if info != nil {
		if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		if commit == "none" {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" && setting.Value != "" {
					commit = setting.Value
				}
			}
		}
	}

	return fmt.Sprintf("%s@%s", version, commit)
}

// readBuildInfo is debug.ReadBuildInfo, as buildVersion takes it.
func readBuildInfo() *debug.BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}

	return info
}
