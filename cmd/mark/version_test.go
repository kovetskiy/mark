package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	const revision = "da9c2cc56bfded06edaa5d19648c3f0c0d6668e5"
	const pseudo = "v17.0.0-20261021233951-da9c2cc56bfd"

	vcs := []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: revision},
	}

	// go install records no VCS settings, and -buildvcs=false "(devel)".
	installed := &debug.BuildInfo{Main: debug.Module{Version: "v17.0.0"}}
	checkout := &debug.BuildInfo{Main: debug.Module{Version: pseudo}, Settings: vcs}
	dirty := &debug.BuildInfo{Main: debug.Module{Version: pseudo + "+dirty"}, Settings: vcs}
	tagged := &debug.BuildInfo{Main: debug.Module{Version: "v17.0.0"}, Settings: vcs}
	taggedDirty := &debug.BuildInfo{Main: debug.Module{Version: "v17.0.0+dirty"}, Settings: vcs}
	noVCS := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}

	for _, test := range []struct {
		name            string
		version, commit string
		info            *debug.BuildInfo
		want            string
	}{
		{"the flags win", "v17.1.0", "abc", dirty, "v17.1.0@abc"},
		{"go install", "dev", "none", installed, "v17.0.0"},
		{"a checkout", "dev", "none", checkout, pseudo},
		{"a checkout with changes", "dev", "none", dirty, pseudo + "+dirty"},
		{"a tagged checkout", "dev", "none", tagged, "v17.0.0@" + revision},
		{"a tagged checkout with changes", "dev", "none", taggedDirty, "v17.0.0@" + revision + "+dirty"},
		{"a commit from the flags", "dev", "abc", checkout, pseudo + "@abc"},
		{"make build with no tags", "dev", revision, checkout, pseudo},
		{"make build with no tags and changes", "dev", revision + "+dirty", dirty, pseudo + "+dirty"},
		{"make build with changes", "v17.0.0", revision + "+dirty", dirty, "v17.0.0@" + revision + "+dirty"},
		{"no VCS information, as a source archive has", "dev", "none", noVCS, "dev@none"},
		{"no build info", "dev", "none", nil, "dev@none"},
		{"empty flags", "", "", checkout, pseudo},
		{"empty flags and no build info", "", "", nil, "dev@none"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := buildVersion(test.version, test.commit, test.info); got != test.want {
				t.Errorf("buildVersion() = %q, want %q", got, test.want)
			}
		})
	}
}
