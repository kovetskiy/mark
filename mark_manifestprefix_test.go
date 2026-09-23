package mark

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManifestPrefixRequiresTrackPages: the flag names the manifest's
// properties, and nothing else keeps one. The default value is not a request.
func TestManifestPrefixRequiresTrackPages(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	dir := t.TempDir()
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		ManifestPrefix: "mark.manifest", Output: io.Discard,
	}
	require.NoError(t, Run(config), "the default prefix asks for nothing")

	config.ManifestPrefix = "team.docs"
	err := Run(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--manifest-prefix requires --track-pages")

	config.TrackPages = true
	config.ManifestPrefix = "not a key"
	err = Run(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot prefix a property key")
}
