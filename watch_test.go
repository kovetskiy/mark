package mark

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A publish creates the page and a parent nobody had made yet, and Confluence
// makes the publishing account watch both. --watch=false leaves it watching
// neither.
func TestUnwatchAfterPublish(t *testing.T) {
	for _, tc := range []struct {
		name    string
		unwatch bool
		want    []string
	}{
		{"with --watch=false", true, []string{}},
		{"without", false, []string{"acct-current"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, api := docsSpace(t)
			server.SetWatchOnWrite()

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

			for _, title := range []string{"New Parent", "Doc"} {
				page, err := api.FindPage(context.Background(), "DOCS", title, "page")
				require.NoError(t, err)
				require.NotNil(t, page, title)
				assert.Equal(t, tc.want, server.Watchers(page.ID), title)
			}
		})
	}
}
