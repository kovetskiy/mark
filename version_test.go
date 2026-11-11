package mark

import (
	"runtime/debug"
	"testing"
)

func TestVersionOf(t *testing.T) {
	const path = "github.com/kovetskiy/mark/v17"
	const pseudo = "v17.1.1-0.20261021233951-da9c2cc56bfd"

	if got := modulePath(); got != path {
		t.Fatalf("modulePath() = %q, want %q", got, path)
	}

	dependency := func(version string, replace *debug.Module) *debug.BuildInfo {
		return &debug.BuildInfo{
			Main: debug.Module{Path: "example.com/scaffold", Version: "(devel)"},
			Deps: []*debug.Module{
				{Path: "golang.org/x/mod", Version: "v0.42.0"},
				{Path: path, Version: version, Replace: replace},
			},
		}
	}
	main := func(version string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: path, Version: version}}
	}

	for _, test := range []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"a release", dependency("v17.1.0", nil), "v17.1.0"},
		{"a pre-release", dependency("v17.2.0-rc.1", nil), "v17.2.0-rc.1"},
		{"a pseudo-version", dependency(pseudo, nil), ""},
		{"a pseudo-version with no tag before it", dependency("v17.0.0-20261021233951-da9c2cc56bfd", nil), ""},
		{"a pseudo-version after a pre-release", dependency("v17.2.0-rc.1.0.20261021233951-da9c2cc56bfd", nil), ""},
		{"build metadata", dependency("v17.1.0+incompatible", nil), ""},
		{"a shorthand version", dependency("v17.1", nil), ""},
		{"replaced by another release of mark", dependency("v17.0.0", &debug.Module{Path: path, Version: "v17.1.0"}), "v17.1.0"},
		{"replaced by a pseudo-version of mark", dependency("v17.0.0", &debug.Module{Path: path, Version: pseudo}), ""},
		{"replaced by a directory", dependency("v17.1.0", &debug.Module{Path: "../mark"}), ""},
		{"replaced by a fork", dependency("v17.1.0", &debug.Module{Path: "github.com/someone/mark/v17", Version: "v17.1.0"}), ""},
		{"not a dependency", &debug.BuildInfo{Main: debug.Module{Path: "example.com/scaffold"}}, ""},
		{"no build info", nil, ""},
		{"mark itself, installed at a release", main("v17.1.0"), "v17.1.0"},
		{"mark itself, built in a checkout", main(pseudo), ""},
		{"mark itself, built at a tag with changes", main("v17.1.0+dirty"), ""},
		{"mark itself, with no version recorded", main("(devel)"), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := versionOf(test.info); got != test.want {
				t.Errorf("versionOf() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestImageOf(t *testing.T) {
	if got, want := imageOf("v17.1.0"), "docker.io/kovetskiy/mark:v17.1.0"; got != want {
		t.Errorf("imageOf(%q) = %q, want %q", "v17.1.0", got, want)
	}
	if got := imageOf(""); got != "" {
		t.Errorf(`imageOf("") = %q, want ""`, got)
	}
}

// mark's own tests are built with mark as the main module, which go test
// records as "(devel)".
func TestVersionInOwnTests(t *testing.T) {
	if got := Version(); got != "" {
		t.Errorf("Version() = %q, want \"\"", got)
	}
	if got := Image(); got != "" {
		t.Errorf("Image() = %q, want \"\"", got)
	}
}
