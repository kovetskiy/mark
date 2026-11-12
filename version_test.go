package mark

import (
	"reflect"
	"runtime/debug"
	"testing"
)

func TestVersionOf(t *testing.T) {
	const pseudo = "v17.1.1-0.20261021233951-da9c2cc56bfd"

	// A major version changes the path; this package sits at the module root.
	if got := reflect.TypeFor[Config]().PkgPath(); got != modulePath {
		t.Fatalf("modulePath = %q, but this package is %q", modulePath, got)
	}

	dependency := func(version string, replace *debug.Module) *debug.BuildInfo {
		return &debug.BuildInfo{
			Main: debug.Module{Path: "example.com/scaffold", Version: "(devel)"},
			Deps: []*debug.Module{
				{Path: "golang.org/x/mod", Version: "v0.42.0"},
				{Path: modulePath, Version: version, Replace: replace},
			},
		}
	}
	main := func(version string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: version}}
	}

	for _, test := range []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"a release", dependency("v17.1.0", nil), "v17.1.0"},
		{"a pre-release", dependency("v17.2.0-rc.1", nil), "v17.2.0-rc.1"},
		{"a pseudo-version", dependency(pseudo, nil), pseudo},
		{"replaced by another version of mark", dependency("v17.0.0", &debug.Module{Path: modulePath, Version: "v17.1.0"}), "v17.1.0"},
		{"replaced by a directory", dependency("v17.1.0", &debug.Module{Path: "../mark"}), ""},
		{"replaced by a fork", dependency("v17.1.0", &debug.Module{Path: "github.com/someone/mark/v17", Version: "v17.1.0"}), ""},
		{"not a dependency", &debug.BuildInfo{Main: debug.Module{Path: "example.com/scaffold"}}, ""},
		{"mark itself, installed at a release", main("v17.1.0"), "v17.1.0"},
		{"mark itself, with no version recorded", main("(devel)"), "(devel)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := versionOf(test.info); got != test.want {
				t.Errorf("versionOf() = %q, want %q", got, test.want)
			}
		})
	}
}
