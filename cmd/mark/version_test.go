package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	installed := &debug.BuildInfo{Main: debug.Module{Version: "v17.0.0"}}
	checkout := &debug.BuildInfo{
		Main:     debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123abc"}},
	}

	for _, test := range []struct {
		name            string
		version, commit string
		info            *debug.BuildInfo
		want            string
	}{
		{"the flags win", "v17.1.0", "abc", installed, "v17.1.0@abc"},
		{"go install", "dev", "none", installed, "v17.0.0@none"},
		{"no module version", "dev", "none", checkout, "dev@0123abc"},
		{"a commit from the flags", "dev", "abc", checkout, "dev@abc"},
		{"no build info", "dev", "none", nil, "dev@none"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := buildVersion(test.version, test.commit, test.info); got != test.want {
				t.Errorf("buildVersion() = %q, want %q", got, test.want)
			}
		})
	}
}
