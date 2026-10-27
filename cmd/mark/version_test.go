package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	const revision = "da9c2cc56bfded06edaa5d19648c3f0c0d6668e5"
	const pseudo = "v17.0.0-20261021233951-da9c2cc56bfd"

	vcs := func(modified bool) []debug.BuildSetting {
		m := "false"
		if modified {
			m = "true"
		}
		return []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: revision},
			{Key: "vcs.modified", Value: m},
		}
	}

	// What the toolchain records: "go install ...@v17.0.0" has a module
	// version and no VCS settings; "go build" in a checkout has both, the
	// version a pseudo-version unless the commit is tagged, and "+dirty"
	// after it when the tree has changes; -buildvcs=false has "(devel)" and
	// no VCS settings.
	installed := &debug.BuildInfo{Main: debug.Module{Version: "v17.0.0"}}
	checkout := &debug.BuildInfo{Main: debug.Module{Version: pseudo}, Settings: vcs(false)}
	dirty := &debug.BuildInfo{Main: debug.Module{Version: pseudo + "+dirty"}, Settings: vcs(true)}
	tagged := &debug.BuildInfo{Main: debug.Module{Version: "v17.0.0"}, Settings: vcs(false)}
	taggedDirty := &debug.BuildInfo{Main: debug.Module{Version: "v17.0.0+dirty"}, Settings: vcs(true)}
	noVCS := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}

	for _, test := range []struct {
		name            string
		version, commit string
		info            *debug.BuildInfo
		want            string
	}{
		{"the flags win", "v17.1.0", "abc", dirty, "v17.1.0@abc"},
		{"go install", "dev", "none", installed, "v17.0.0@none"},
		{"a checkout", "dev", "none", checkout, pseudo},
		{"a checkout with changes", "dev", "none", dirty, pseudo + "+dirty"},
		{"a tagged checkout", "dev", "none", tagged, "v17.0.0@" + revision},
		{"a tagged checkout with changes", "dev", "none", taggedDirty, "v17.0.0@" + revision + "+dirty"},
		{"a commit from the flags", "dev", "abc", checkout, pseudo + "@abc"},
		{"no VCS information", "dev", "none", noVCS, "dev@none"},
		{"no build info", "dev", "none", nil, "dev@none"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := buildVersion(test.version, test.commit, test.info); got != test.want {
				t.Errorf("buildVersion() = %q, want %q", got, test.want)
			}
		})
	}
}
