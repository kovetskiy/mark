package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLinksRootFlags: the staged and source roots reach the run from the
// command line, the environment and the configuration file alike.
func TestLinksRootFlags(t *testing.T) {
	source, staged := t.TempDir(), t.TempDir()
	doc := "<!-- Space: DOCS -->\n<!-- Title: Intro -->\n\nSee [the guide](./guide.md).\n"
	require.NoError(t, os.WriteFile(filepath.Join(source, "guide.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nGuide.\n"), 0o600))
	document := filepath.Join(staged, "intro.md")
	require.NoError(t, os.WriteFile(document, []byte(doc), 0o600))

	for _, tc := range []struct {
		name   string
		env    map[string]string
		config string
		args   []string
		fails  bool
	}{
		{name: "without them", fails: true},
		{name: "flags", args: []string{"--links-staged-root", staged, "--links-source-root", source}},
		{name: "environment", env: map[string]string{
			"MARK_LINKS_STAGED_ROOT": staged, "MARK_LINKS_SOURCE_ROOT": source,
		}},
		{name: "configuration file",
			config: "links-staged-root = \"" + filepath.ToSlash(staged) + "\"\n" +
				"links-source-root = \"" + filepath.ToSlash(source) + "\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noDefaultConfig(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			server.AddPage("DOCS", "Guide", "page", home.ID)

			config := writeConfig(t, t.TempDir(), "mark.toml", tc.config)
			args := []string{"-c", config, "--color", "never", "-u", "user", "-p", "token", "publish",
				"-b", server.URL, "--compile-only", "--check-links", "internal", "--files", document}

			out, _, err := runMark(t, append(args, tc.args...)...)
			if tc.fails {
				require.ErrorContains(t, err, "there is no such file")
				return
			}

			require.NoError(t, err)
			assert.Contains(t, out, "/x/", "the link is rewritten to the guide's page")
		})
	}
}

// TestLinksRootFlagRefusal: one root without the other is refused naming both
// flags and the fields they set.
func TestLinksRootFlagRefusal(t *testing.T) {
	noDefaultConfig(t)

	_, _, err := runMark(t, "--color", "never", "publish", "--compile-only",
		"--links-staged-root", t.TempDir(), "--files", "missing.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Links.StagedRoot (--links-staged-root) requires Links.SourceRoot (--links-source-root)")
}

// TestLinksSourceRootCheckOnlyFlag: the check-only switch reaches the run from
// the command line, the environment and the configuration file, and a link to
// a source file without headers then passes and is left as written.
func TestLinksSourceRootCheckOnlyFlag(t *testing.T) {
	source, staged := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, "guide.md"), []byte("No headers.\n"), 0o600))
	document := filepath.Join(staged, "intro.md")
	require.NoError(t, os.WriteFile(document,
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Intro -->\n\nSee [the guide](./guide.md).\n"), 0o600))

	for _, tc := range []struct {
		name   string
		env    map[string]string
		config string
		args   []string
		fails  bool
	}{
		{name: "without it", fails: true},
		{name: "flag", args: []string{"--links-source-root-check-only"}},
		{name: "environment", env: map[string]string{"MARK_LINKS_SOURCE_ROOT_CHECK_ONLY": "true"}},
		{name: "configuration file", config: "links-source-root-check-only = true\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noDefaultConfig(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)

			config := writeConfig(t, t.TempDir(), "mark.toml", tc.config)
			args := []string{"-c", config, "--color", "never", "-u", "user", "-p", "token", "publish",
				"-b", server.URL, "--compile-only", "--check-links", "internal", "--files", document,
				"--links-staged-root", staged, "--links-source-root", source}

			out, _, err := runMark(t, append(args, tc.args...)...)
			if tc.fails {
				require.ErrorContains(t, err, "it has no title")
				return
			}

			require.NoError(t, err)
			assert.Contains(t, out, `href="./guide.md"`)
		})
	}
}
