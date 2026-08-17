package mark

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func synchronizedServer(t *testing.T) *confluencetest.Server {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	return server
}

// TestSynchronizedFalseHeaderSkipsTheFile covers the header form.
func TestSynchronizedFalseHeaderSkipsTheFile(t *testing.T) {
	server := synchronizedServer(t)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Draft -->\n"+
			"<!-- Synchronized: false -->\n\nNot ready.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage("DOCS", "Draft", "page")
	require.NoError(t, err)
	assert.Nil(t, page, "an unsynchronized document must not be published")
}

// TestSynchronizedFalseFrontMatterSkipsTheFile covers the front matter form.
func TestSynchronizedFalseFrontMatterSkipsTheFile(t *testing.T) {
	server := synchronizedServer(t)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"---\ntitle: Draft\nspace: DOCS\nparents:\n  - Parent\nsynchronized: false\n---\n\nNot ready.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention", "frontmatter"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage("DOCS", "Draft", "page")
	require.NoError(t, err)
	assert.Nil(t, page, "an unsynchronized document must not be published")
}

// TestSynchronizedTrueAndAbsentBothPublish: opting out has to be deliberate,
// and saying nothing is the ordinary case.
func TestSynchronizedTrueAndAbsentBothPublish(t *testing.T) {
	for name, header := range map[string]string{
		"stated true": "<!-- Synchronized: true -->\n",
		"not stated":  "",
	} {
		t.Run(name, func(t *testing.T) {
			server := synchronizedServer(t)

			dir := t.TempDir()
			writeFile(t, dir, "doc.md",
				"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n"+
					header+"\nBody.\n")

			require.NoError(t, Run(Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
				Output: io.Discard,
			}))

			api := confluence.NewAPI(server.URL, "user", "token", false)
			page, err := api.FindPage("DOCS", "Doc", "page")
			require.NoError(t, err)
			assert.NotNil(t, page)
		})
	}
}

// TestSynchronizedFalseKeepsTheMapping is the case that would hurt. Opting a
// document out must not read as the file being gone: mark drops the page from
// the manifest, and synchronising it again later would no longer know which
// page was already its own -- publishing a second copy beside it.
//
// A second document in the same space is what makes this bite. Skipping every
// file in a space leaves the manifest for that space unread, so nothing is
// judged missing; it takes one page that does publish to load the mapping and
// expose the skipped one as unaccounted for.
func TestSynchronizedFalseKeepsTheMapping(t *testing.T) {
	server := synchronizedServer(t)

	dir := t.TempDir()
	draft := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Draft -->\n"
	writeFile(t, dir, "draft.md", draft+"\nBody.\n")
	writeFile(t, dir, "other.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Other -->\n\nBody.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	published, err := api.FindPage("DOCS", "Draft", "page")
	require.NoError(t, err)
	require.NotNil(t, published)

	// Now the draft opts out, while its neighbour keeps publishing.
	writeFile(t, dir, "draft.md", draft+"<!-- Synchronized: false -->\n\nBody.\n")
	require.NoError(t, Run(config))

	// The page is never deleted either way; a forgotten mapping is what causes
	// a duplicate the next time it publishes.
	require.NotNil(t, server.Page(published.ID))

	entry, ok, err := manifest.NewStore(api).Lookup("DOCS", filepath.Join(dir, "draft.md"))
	require.NoError(t, err)
	require.True(t, ok, "the page mapping must survive a document opting out")
	assert.Equal(t, published.ID, entry.PageID)
}

func TestSynchronizedRejectsANonBoolean(t *testing.T) {
	server := synchronizedServer(t)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n<!-- Synchronized: maybe -->\n\nBody.\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "true or false")
}
