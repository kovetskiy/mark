package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublishTrimsParents: --parents "Parent / Sub" places the page under
// "Parent" and "Sub", and hashes over those titles, not " Sub" and "Parent ".
func TestPublishTrimsParents(t *testing.T) {
	noDefaultConfig(t)

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	document := filepath.Join(t.TempDir(), "doc.md")
	require.NoError(t, os.WriteFile(document, []byte("<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n"), 0o600))

	_, _, err := runMark(t, "--color", "never", "-u", "user", "-p", "token",
		"publish", "-b", server.URL, "--files", document,
		"--parents", "Parent / Sub", "--title-append-generated-hash")
	require.NoError(t, err)

	var titles []string
	for id := home.ID; ; {
		children := server.ChildOrder(id)
		if len(children) == 0 {
			break
		}
		require.Len(t, children, 1)
		id = children[0]
		titles = append(titles, server.Page(id).Title)
	}

	assert.Equal(t, []string{
		"Parent",
		"Sub",
		metadata.CaseFoldedHashTitle([]string{"Parent", "Sub"}, "DOCS", "Doc"),
	}, titles)
}
