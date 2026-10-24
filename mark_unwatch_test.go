package mark

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A publish creates the page and a parent nobody had made yet, and Confluence
// makes the publishing account watch both. --unwatch leaves it watching
// neither.
func TestUnwatchAfterPublish(t *testing.T) {
	for _, tc := range []struct {
		name    string
		unwatch bool
		want    []string
	}{
		{"with --unwatch", true, []string{}},
		{"without", false, []string{"acct-current"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := confluencetest.New(t)
			server.SetWatchOnWrite()
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)

			dir := t.TempDir()
			writeFile(t, dir, "doc.md",
				"<!-- Space: DOCS -->\n<!-- Parent: New Parent -->\n<!-- Title: Doc -->\n\nBody.\n")

			require.NoError(t, Run(Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files:    filepath.Join(dir, "doc.md"),
				Features: []string{"mention"},
				Unwatch:  tc.unwatch,
				Output:   io.Discard,
			}))

			api := confluence.NewAPI(server.URL, "user", "token", false)
			for _, title := range []string{"New Parent", "Doc"} {
				page, err := api.FindPage("DOCS", title, "page")
				require.NoError(t, err)
				require.NotNil(t, page, title)
				assert.Equal(t, tc.want, server.Watchers(page.ID), title)
			}
		})
	}
}
