package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePageRef(t *testing.T) {
	for _, test := range []struct {
		value, space string
		want         page.Ref
		base         string
	}{
		{"123", "", page.Ref{ID: "123"}, ""},
		{"123", "DOCS", page.Ref{ID: "123"}, ""},
		{"Team Handbook", "DOCS", page.Ref{Space: "DOCS", Title: "Team Handbook"}, ""},
		{"https://example.atlassian.net/wiki/spaces/DOCS/pages/42/Title", "", page.Ref{ID: "42", Space: "DOCS"}, "https://example.atlassian.net/wiki"},
		{"https://confluence.example.com/display/DOCS/Team+Handbook", "", page.Ref{Space: "DOCS", Title: "Team Handbook"}, "https://confluence.example.com"},
	} {
		t.Run(test.value, func(t *testing.T) {
			ref, base, err := parsePageRef(test.value, test.space)
			require.NoError(t, err)
			assert.Equal(t, test.want, ref)
			assert.Equal(t, test.base, base)
		})
	}

	_, _, err := parsePageRef("Team Handbook", "")
	require.ErrorContains(t, err, "give --space")
}

// TestPageCommands runs each "mark page" command against a fake Confluence.
func TestPageCommands(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	a := server.AddPage("DOCS", "A", "page", home.ID)
	b := server.AddPage("DOCS", "B", "page", home.ID)
	c := server.AddPage("DOCS", "C", "page", b.ID)

	config := writeConfig(t, t.TempDir(), "mark.toml", "")
	mark := func(args ...string) (string, error) {
		t.Helper()
		stdout, _, err := runMark(t, append([]string{"-c", config, "--color", "never", "-u", "user", "-p", "token"}, args...)...)
		return stdout, err
	}

	t.Run("move, with its flags after the page", func(t *testing.T) {
		stdout, err := mark("page", "move", "-b", server.URL, c.ID, "--parent", a.ID)
		require.NoError(t, err)
		assert.Contains(t, stdout, `moved "C"`)
		assert.Equal(t, a.ID, server.Page(c.ID).ParentID)
	})

	t.Run("move by URL gives the base URL", func(t *testing.T) {
		_, err := mark("page", "move", server.URL+"/pages/viewpage.action?pageId="+c.ID,
			"--parent", server.URL+"/pages/viewpage.action?pageId="+b.ID)
		require.NoError(t, err)
		assert.Equal(t, b.ID, server.Page(c.ID).ParentID)
	})

	t.Run("move needs a destination before it signs in", func(t *testing.T) {
		_, err := mark("page", "move", c.ID)
		require.ErrorContains(t, err, "--parent, --before or --after")
		_, err = mark("page", "move", "-b", server.URL, c.ID, "--before", a.ID, "--after", b.ID)
		require.ErrorContains(t, err, "only one of them")
	})

	t.Run("rename by title", func(t *testing.T) {
		stdout, err := mark("page", "rename", "-b", server.URL, "--space", "DOCS", "A", "Renamed")
		require.NoError(t, err)
		assert.Contains(t, stdout, `to "Renamed"`)
		assert.Equal(t, "Renamed", server.Page(a.ID).Title)
	})

	t.Run("delete refuses children without --recursive", func(t *testing.T) {
		_, err := mark("page", "delete", "-b", server.URL, b.ID)
		require.ErrorContains(t, err, "--recursive")

		stdout, err := mark("page", "delete", "-b", server.URL, "--dry-run", "--recursive", b.ID)
		require.NoError(t, err)
		assert.Contains(t, stdout, `would trash "C"`)
		assert.False(t, server.Page(b.ID).Trashed)

		_, err = mark("page", "delete", "-b", server.URL, "--recursive", b.ID)
		require.NoError(t, err)
		assert.True(t, server.Page(b.ID).Trashed)
		assert.True(t, server.Page(c.ID).Trashed)
	})

	t.Run("archive", func(t *testing.T) {
		_, err := mark("page", "archive", "-b", server.URL, a.ID)
		require.NoError(t, err)
		assert.True(t, server.Page(a.ID).Archived)
	})

	t.Run("watch and unwatch, for someone else", func(t *testing.T) {
		server.AddUser(confluencetest.User{AccountID: "acct-jane", FullName: "Jane Doe"})
		stdout, err := mark("page", "watch", "-b", server.URL, home.ID, "--user", "Jane Doe")
		require.NoError(t, err)
		assert.Contains(t, stdout, `watched "Home"`)
		assert.Equal(t, []string{"acct-jane"}, server.Watchers(home.ID))

		_, err = mark("page", "unwatch", "-b", server.URL, home.ID, "--user", "accountid:acct-jane")
		require.NoError(t, err)
		assert.Empty(t, server.Watchers(home.ID))

		_, err = mark("page", "watch", "-b", server.URL, "--recursive", home.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{"acct-current"}, server.Watchers(a.ID), "--recursive reaches the children")
	})

	t.Run("tree and copy", func(t *testing.T) {
		stdout, err := mark("page", "copy", "-b", server.URL, a.ID, "--parent", home.ID, "--title", "A copy")
		require.NoError(t, err)
		assert.Contains(t, stdout, `as "A copy"`)

		stdout, err = mark("page", "tree", "-b", server.URL, home.ID, "--depth", "2")
		require.NoError(t, err)
		assert.Contains(t, stdout, "Home ("+home.ID+")\n")
		assert.Contains(t, stdout, "── A copy (")

		_, err = mark("page", "copy", "-b", server.URL, a.ID)
		require.ErrorContains(t, err, "--parent")
		_, err = mark("page", "tree", "-b", server.URL, home.ID, "--depth", "-1")
		require.ErrorContains(t, err, "cannot be negative")
	})

	t.Run("the wrong number of arguments", func(t *testing.T) {
		_, err := mark("page", "rename", "-b", server.URL, a.ID)
		require.ErrorContains(t, err, "takes 2 argument(s) and got 1")
	})
}

// TestWatchFlag: publishing leaves the account watching what it wrote, as
// Confluence makes it, unless --watch=false says otherwise.
func TestWatchFlag(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		watchers []string
	}{
		{"by default", nil, []string{"acct-current"}},
		{"--watch=false", []string{"--watch=false"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := confluencetest.New(t)
			server.SetWatchOnWrite()
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			existing := server.AddPage("DOCS", "Page", "page", home.ID)

			dir := t.TempDir()
			config := writeConfig(t, dir, "mark.toml", "")
			document := filepath.Join(dir, "page.md")
			require.NoError(t, os.WriteFile(document,
				[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

			_, _, err := runMark(t, append([]string{
				"-c", config, "--color", "never", "-u", "user", "-p", "token",
				"publish", "-b", server.URL, "--files", document,
			}, tc.args...)...)
			require.NoError(t, err)

			assert.Contains(t, server.Page(existing.ID).Body, "Body.", "the page was published")
			assert.ElementsMatch(t, tc.watchers, server.Watchers(existing.ID))
		})
	}
}
