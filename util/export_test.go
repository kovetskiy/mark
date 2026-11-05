package util

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// TestExportResolvesFlags: the export flags come from the command line, the
// environment and the configuration file, in that order, like publish's.
func TestExportResolvesFlags(t *testing.T) {
	dir := t.TempDir()
	config := writeConfig(t, dir, "mark.toml",
		"username = \"cfguser\"\nbase-url = \"http://cfg.invalid\"\n"+
			"space = \"CFG\"\ntitle = \"From config\"\noutput = \"out.md\"\n"+
			"attachments-dir = \"files\"\noverwrite = true\nno-attachments = true\nmetadata = \"frontmatter\"\n")

	names := []string{"username", "base-url", "space", "title", "output", "attachments-dir", "overwrite", "no-attachments", "metadata"}
	fromFile := map[string]string{
		"username":        "cfguser",
		"base-url":        "http://cfg.invalid",
		"space":           "CFG",
		"title":           "From config",
		"output":          "out.md",
		"attachments-dir": "files",
		"overwrite":       "true",
		"no-attachments":  "true",
		"metadata":        "frontmatter",
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
			map[string]string{"MARK_TITLE": "From env", "MARK_OUTPUT": "env.md", "MARK_METADATA": "comments"},
			[]string{"title", "output", "space", "metadata"},
			"export", "-c", config, "-o", "cli.md", "--space", "CLI")
		assert.Equal(t, "From env", resolved["title"])
		assert.Equal(t, "cli.md", resolved["output"])
		assert.Equal(t, "CLI", resolved["space"])
		assert.Equal(t, "comments", resolved["metadata"])
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
		{"--recursive to standard output", []string{"--page-id", "1", "--recursive"}, "name it with --output"},
		{"--recursive with --attachments-dir", []string{"--page-id", "1", "--recursive", "-o", "out", "--attachments-dir", "files"}, "does not apply"},
		{"--depth without --recursive", []string{"--page-id", "1", "--depth", "2"}, "does nothing without it"},
		{"a negative --depth", []string{"--page-id", "1", "--recursive", "-o", "out", "--depth", "-1"}, "cannot be negative"},
		{"--relative-links without --recursive", []string{"--page-id", "1", "--relative-links"}, "does nothing without it"},
		{"a space without --recursive", []string{"--space", "DOCS"}, "or the space alone with --recursive"},
		{"an unknown --format", []string{"--page-id", "1", "--format", "html"}, `--format: unknown export format "html"`},
		{"an unknown --metadata", []string{"--page-id", "1", "--metadata", "toml"}, `--metadata: unknown export metadata "toml"`},
		{"--relative-links with --format okf", []string{"--page-id", "1", "-r", "-o", "out", "--format", "okf", "--relative-links"}, "does already"},
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

	t.Run("as an OKF concept", func(t *testing.T) {
		stdout, _, err := runMark(t, append(credentials, "export", "-b", server.URL, "--page-id", page.ID, "--format", "okf")...)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(stdout, "---\ntype: Confluence Page\ntitle: Exported page\n"), stdout)
		assert.Contains(t, stdout, "tags:\n  - exported\n")
		assert.Contains(t, stdout, "generated:\n  by: mark/test\n", "mark and its version generated it")
		assert.True(t, strings.HasSuffix(stdout, "---\n\n# Hello\n\nFrom **Confluence**.\n"), stdout)
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

	t.Run("by URL, with the same base URL configured", func(t *testing.T) {
		stdout, _, err := runMark(t, append(credentials, "export", "-b", server.URL+"/",
			"--url", server.URL+"/pages/viewpage.action?pageId="+page.ID)...)
		require.NoError(t, err)
		assert.Equal(t, want, stdout)
	})

	t.Run("by URL, with another instance configured", func(t *testing.T) {
		_, _, err := runMark(t, append(credentials, "export", "-b", "http://other.invalid",
			"--url", server.URL+"/pages/viewpage.action?pageId="+page.ID)...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "other.invalid")
		assert.Contains(t, err.Error(), "--page-id")
	})
}

// TestExportBaseURL: a URL gives the instance unless one is configured, and a
// configured instance on another host is refused, since a page id names a page
// of one instance only. The gateway fronts every Cloud site.
func TestExportBaseURL(t *testing.T) {
	for _, test := range []struct {
		configured, fromURL, want, err string
	}{
		{"", "https://a.atlassian.net/wiki", "https://a.atlassian.net/wiki", ""},
		{"https://a.atlassian.net/wiki", "https://a.atlassian.net/wiki", "https://a.atlassian.net/wiki", ""},
		{"https://A.atlassian.net:443", "https://a.atlassian.net/wiki", "https://A.atlassian.net:443", ""},
		{"https://api.atlassian.com/ex/confluence/id", "https://a.atlassian.net/wiki", "https://api.atlassian.com/ex/confluence/id", ""},
		{"https://b.atlassian.net/wiki", "https://a.atlassian.net/wiki", "", "b.atlassian.net"},
		{"https://api.atlassian.com/ex/confluence/id", "https://confluence.example.com", "", "api.atlassian.com"},
	} {
		got, err := exportBaseURL(test.configured, test.fromURL)
		if test.err != "" {
			assert.ErrorContains(t, err, test.err, test.configured)
			continue
		}
		require.NoError(t, err, test.configured)
		assert.Equal(t, test.want, got)
	}
}

// TestAPublishFlagBeforeExportIsRefused: a publish flag before "export" says
// whose flag it is, as well as that the root does not have it.
func TestAPublishFlagBeforeExportIsRefused(t *testing.T) {
	noDefaultConfig(t)

	_, _, err := dispatch(t, "-f", "doc.md", "export", "--page-id", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `-f: it is a flag of "publish"`)
}

// TestExportCommandLineWins: a page named on the command line sets aside one
// that the environment or the configuration file names -- a MARK_PAGE_URL a CI
// job sets for publishing, say -- rather than colliding with it.
func TestExportCommandLineWins(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	page := server.AddPage("DOCS", "Exported page", "page", home.ID)
	server.EditPage(page.ID, `<p>Wanted.</p>`)
	other := server.AddPage("DOCS", "Other page", "page", home.ID)
	server.EditPage(other.ID, `<p>Not wanted.</p>`)

	address := server.URL + "/pages/viewpage.action?pageId="
	empty := writeConfig(t, t.TempDir(), "mark.toml", "")
	withURL := writeConfig(t, t.TempDir(), "mark.toml", "url = \""+address+other.ID+"\"\n")

	for _, test := range []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"--page-id over MARK_PAGE_URL", map[string]string{"MARK_PAGE_URL": address + other.ID},
			[]string{"-c", empty, "export", "-b", server.URL, "--page-id", page.ID}},
		{"--page-id over url in the file", nil,
			[]string{"-c", withURL, "export", "-b", server.URL, "--page-id", page.ID}},
		{"--space and --title over MARK_PAGE_URL", map[string]string{"MARK_PAGE_URL": address + other.ID},
			[]string{"-c", empty, "export", "-b", server.URL, "--space", "DOCS", "--title", "Exported page"}},
		{"--url over MARK_PAGE_ID", map[string]string{"MARK_PAGE_ID": other.ID},
			[]string{"-c", empty, "export", "--url", address + page.ID}},
		{"--page-id over MARK_TITLE", map[string]string{"MARK_TITLE": "Other page", "MARK_SPACE": "DOCS"},
			[]string{"-c", empty, "export", "-b", server.URL, "--page-id", page.ID}},
		{"--title over MARK_PAGE_ID", map[string]string{"MARK_PAGE_ID": other.ID},
			[]string{"-c", empty, "export", "-b", server.URL, "--space", "DOCS", "--title", "Exported page"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			noDefaultConfig(t)
			for name, value := range test.env {
				t.Setenv(name, value)
			}

			stdout, _, err := runMark(t, append([]string{"--color", "never", "-u", "user", "-p", "token"}, test.args...)...)
			require.NoError(t, err)
			assert.Contains(t, stdout, "Wanted.")
		})
	}
}

// exported runs "mark export" with exporting swapped out, and says which page
// it would have exported, and into which attachments directory.
func exported(t *testing.T, args ...string) (map[string]string, error) {
	t.Helper()

	var got map[string]string
	cmd := buildCommand("test", nil, func(_ context.Context, c *cli.Command) error {
		got = exportPage(c)
		got["space"] = c.String("space")
		return nil
	})
	cmd.Writer, cmd.ErrWriter = &bytes.Buffer{}, &bytes.Buffer{}

	_, _, err := captureOutput(t, func() error {
		return cmd.Run(context.Background(), append([]string{"mark", "export"}, args...))
	})

	return got, err
}

// TestExportSettingSources: the page named on the command line is the one
// exported; the environment and the configuration file name it only when the
// command line does not. Two names from the same place are still refused,
// and the refusal says where each came from.
func TestExportSettingSources(t *testing.T) {
	const address = "https://example.invalid/wiki/spaces/DOCS/pages/2/Other"
	withURL := writeConfig(t, t.TempDir(), "mark.toml", "url = \""+address+"\"\nspace = \"CFG\"\n")
	withTitle := writeConfig(t, t.TempDir(), "mark.toml", "title = \"From config\"\n")

	for _, test := range []struct {
		name string
		env  map[string]string
		args []string
		want map[string]string
		err  string
	}{
		{
			name: "the whole space over MARK_PAGE_URL",
			env:  map[string]string{"MARK_PAGE_URL": address},
			args: []string{"--space", "DOCS", "-r", "-o", "out"},
			want: map[string]string{"space": "DOCS"},
		},
		{
			name: "the whole space over MARK_PAGE_ID",
			env:  map[string]string{"MARK_PAGE_ID": "2"},
			args: []string{"--space", "DOCS", "-r", "-o", "out"},
			want: map[string]string{"space": "DOCS"},
		},
		{
			name: "--space keeps a title from the file",
			args: []string{"-c", withTitle, "--space", "DOCS"},
			want: map[string]string{"title": "From config", "space": "DOCS"},
		},
		{
			name: "the file's url when the command line names no page",
			args: []string{"-c", withURL},
			want: map[string]string{"url": address, "space": "CFG"},
		},
		{
			name: "--url over the file's url and MARK_PAGE_ID",
			env:  map[string]string{"MARK_PAGE_ID": "2"},
			args: []string{"-c", withURL, "--url", "https://example.invalid/wiki/spaces/DOCS/pages/1/Page"},
			want: map[string]string{"url": "https://example.invalid/wiki/spaces/DOCS/pages/1/Page", "space": "CFG"},
		},
		{
			name: "an empty --url clears MARK_PAGE_URL",
			env:  map[string]string{"MARK_PAGE_URL": address},
			args: []string{"--url", "", "--page-id", "1"},
			want: map[string]string{"page-id": "1"},
		},
		{
			name: "--page-id=1 over MARK_PAGE_URL",
			env:  map[string]string{"MARK_PAGE_URL": address},
			args: []string{"--page-id=1"},
			want: map[string]string{"page-id": "1"},
		},
		{
			name: "MARK_ATTACHMENTS_DIR leaves --recursive alone",
			env:  map[string]string{"MARK_ATTACHMENTS_DIR": "files"},
			args: []string{"--page-id", "1", "-r", "-o", "out"},
			want: map[string]string{"page-id": "1"},
		},
		{
			name: "two on the command line",
			env:  map[string]string{"MARK_PAGE_URL": address},
			args: []string{"--page-id", "1", "--title", "T", "--space", "DOCS"},
			err:  "--page-id and --title each name a page",
		},
		{
			name: "--url on the command line as MARK_PAGE_URL has it",
			env:  map[string]string{"MARK_PAGE_URL": address},
			args: []string{"--url", address, "--page-id", "1"},
			err:  "--page-id and --url each name a page",
		},
		{
			name: "two from the environment",
			env:  map[string]string{"MARK_PAGE_URL": address, "MARK_PAGE_ID": "1"},
			err:  `--page-id (from environment variable "MARK_PAGE_ID") and --url (from environment variable "MARK_PAGE_URL") each name a page`,
		},
		{
			name: "one from each",
			env:  map[string]string{"MARK_PAGE_ID": "1"},
			args: []string{"-c", withURL},
			err:  `--page-id (from environment variable "MARK_PAGE_ID") and --url (from toml file`,
		},
		{
			name: "--attachments-dir with --recursive on the command line",
			args: []string{"--page-id", "1", "-r", "-o", "out", "--attachments-dir", "files"},
			err:  "does not apply",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			noDefaultConfig(t)
			for name, value := range test.env {
				t.Setenv(name, value)
			}

			got, err := exported(t, test.args...)
			if test.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.err)
				return
			}
			require.NoError(t, err)
			if _, ok := test.want["space"]; !ok {
				test.want["space"] = ""
			}
			assert.Equal(t, test.want, got)
		})
	}
}
