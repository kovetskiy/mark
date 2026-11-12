package mark

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spaceSpelledTwoWays is two documents of one space, DOCS, one of which
// spells its key in lower case, as Confluence allows.
func spaceSpelledTwoWays(t *testing.T, dataCenter bool) (*confluencetest.Server, Config) {
	t.Helper()

	server := confluencetest.New(t)
	if dataCenter {
		server.SetDataCenter()
	}
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "upper.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Upper -->\n\nUpper.\n")
	writeFile(t, dir, "lower.md", "<!-- Space: docs -->\n<!-- Parent: Parent -->\n<!-- Title: Lower -->\n\nLower.\n")

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Tracking.OnOrphan = "delete"

	return server, config
}

func TestSpaceKeySpelledTwoWaysIsOneManifest(t *testing.T) {
	for name, dataCenter := range map[string]bool{"cloud": false, "data center": true} {
		t.Run(name, func(t *testing.T) {
			server, config := spaceSpelledTwoWays(t, dataCenter)

			require.NoError(t, Run(context.Background(), config))
			upper, lower := pageIDTitled(t, server, "Upper"), pageIDTitled(t, server, "Lower")

			// Neither file is gone, so later runs have nothing to remove.
			for range 2 {
				require.NoError(t, Run(context.Background(), config))
				assert.False(t, server.Page(upper).Trashed, "the page of upper.md")
				assert.False(t, server.Page(lower).Trashed, "the page of lower.md")
			}
		})
	}
}

// TestSpaceKeySpelledTwoWaysSharesItsTitles: with --parents-from-path a space
// holds one document per title, however the documents spell its key.
func TestSpaceKeySpelledTwoWaysSharesItsTitles(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "one.md", "<!-- Space: DOCS -->\n<!-- Title: Same -->\n\nOne.\n")
	writeFile(t, dir, "two.md", "<!-- Space: docs -->\n<!-- Title: Same -->\n\nTwo.\n")

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.ParentsFromPath = true
	config.ParentsFromPathRoot = dir

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already publishes")
}
