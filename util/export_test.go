package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportResolvesFlags: the export flags come from the command line, the
// environment and the configuration file, in that order, like publish's.
func TestExportResolvesFlags(t *testing.T) {
	dir := t.TempDir()
	config := writeConfig(t, dir, "mark.toml",
		"username = \"cfguser\"\nbase-url = \"http://cfg.invalid\"\n"+
			"space = \"CFG\"\ntitle = \"From config\"\noutput = \"out.md\"\n"+
			"attachments-dir = \"files\"\noverwrite = true\nno-attachments = true\n")

	names := []string{"username", "base-url", "space", "title", "output", "attachments-dir", "overwrite", "no-attachments"}
	fromFile := map[string]string{
		"username":        "cfguser",
		"base-url":        "http://cfg.invalid",
		"space":           "CFG",
		"title":           "From config",
		"output":          "out.md",
		"attachments-dir": "files",
		"overwrite":       "true",
		"no-attachments":  "true",
	}

	for _, test := range []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"file named after export", nil, []string{"export", "--config", config}},
		{"file named before export", nil, []string{"--config", config, "export"}},
		{"file named in the environment", map[string]string{"MARK_CONFIG": config}, []string{"export"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolved := resolveFlags(t, test.env, names, test.args...)
			for name, want := range fromFile {
				assert.Equal(t, want, resolved[name], name)
			}
		})
	}

	t.Run("the environment wins over the file, and the command line over both", func(t *testing.T) {
		resolved := resolveFlags(t,
			map[string]string{"MARK_TITLE": "From env", "MARK_OUTPUT": "env.md"},
			[]string{"title", "output", "space"},
			"export", "-c", config, "-o", "cli.md", "--space", "CLI")
		assert.Equal(t, "From env", resolved["title"])
		assert.Equal(t, "cli.md", resolved["output"])
		assert.Equal(t, "CLI", resolved["space"])
	})
}

// TestExportNamesExactlyOnePage: a page is named by its id, its URL, or its
// space and title, and saying two of those is refused rather than one of them
// silently winning.
func TestExportNamesExactlyOnePage(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"nothing", nil, "--page-id, --url, or --space and --title"},
		{"two ways", []string{"--page-id", "1", "--url", "https://example.com/display/A/B"}, "--page-id and --url"},
		{"a title with no space", []string{"--title", "T"}, "--title needs --space"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := runMark(t, append([]string{"-c", config, "--color", "never", "export"}, test.args...)...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

// TestExportCommand runs "mark export" against a fake Confluence, naming the
// page each of the ways it can be named.
func TestExportCommand(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	page := server.AddPage("DOCS", "Exported page", "page", home.ID)
	server.EditPage(page.ID, `<h1>Hello</h1><p>From <strong>Confluence</strong>.</p>`)
	server.AddLabel(page.ID, "exported")

	config := writeConfig(t, t.TempDir(), "mark.toml", "")
	credentials := []string{"-c", config, "--color", "never", "-u", "user", "-p", "token"}

	const want = "<!-- Space: DOCS -->\n<!-- Title: Exported page -->\n<!-- Label: exported -->\n" +
		"<!-- Content-Appearance: default -->\n\n# Hello\n\nFrom **Confluence**.\n"

	t.Run("by id, to a file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "page.md")
		stdout, _, err := runMark(t, append(credentials, "export", "-b", server.URL, "--page-id", page.ID, "-o", out)...)
		require.NoError(t, err)
		assert.Empty(t, stdout, "the document goes to the file")

		written, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.Equal(t, want, string(written))
	})

	t.Run("by URL, to standard output", func(t *testing.T) {
		stdout, _, err := runMark(t, append(credentials, "export",
			"--url", server.URL+"/pages/viewpage.action?pageId="+page.ID)...)
		require.NoError(t, err)
		assert.Equal(t, want, stdout, "the URL gives the base URL too")
	})

	t.Run("by space and title", func(t *testing.T) {
		stdout, _, err := runMark(t, append(credentials, "export", "-b", server.URL,
			"--space", "DOCS", "--title", "Exported page")...)
		require.NoError(t, err)
		assert.Equal(t, want, stdout)
	})

	t.Run("by a URL naming the title", func(t *testing.T) {
		stdout, _, err := runMark(t, append(credentials, "export",
			"--url", server.URL+"/display/DOCS/Exported+page")...)
		require.NoError(t, err)
		assert.Equal(t, want, stdout)
	})
}
