package mark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagesIn is the number of pages in the tree under id, id included.
func pagesIn(server *confluencetest.Server, id string) int {
	count := 1
	for _, child := range server.ChildOrder(id) {
		count += pagesIn(server, child)
	}

	return count
}

// TestGeneratedHashKeepsThePageWhenTheParentIsRecased: Confluence takes
// "Parent" and "parent" for one page, and so does mark without the hash. With
// it, a recased Parent header must hash alike, or the next run publishes a
// second copy of the page beside the first.
func TestGeneratedHashKeepsThePageWhenTheParentIsRecased(t *testing.T) {
	server, _ := docsSpace(t)
	home := pageIDTitled(t, server, "Home")
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.Page.TitleAppendGeneratedHash = true
	require.NoError(t, Run(context.Background(), config))

	parent := pageIDTitled(t, server, "Parent")
	published := server.ChildOrder(parent)
	require.Len(t, published, 1)
	before := pagesIn(server, home)

	writeFile(t, dir, "doc.md", strings.Replace(markdownWithTitle("Doc"), "Parent: Parent", "Parent: parent", 1))
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, published, server.ChildOrder(parent), "the same page under Parent")
	assert.Equal(t, before, pagesIn(server, home), "no second page")
	assert.Equal(t, metadata.CaseFoldedHashTitle([]string{"Parent"}, "DOCS", "Doc"), server.Page(published[0]).Title)
}

// TestGeneratedHashGivesADirectoryOnePageForEitherSpaceSpelling: under
// --parents-from-path, documents spelling the space key two ways sit in one
// directory, and that directory is one page.
func TestGeneratedHashGivesADirectoryOnePageForEitherSpaceSpelling(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "guides"), 0o755))
	writeFile(t, filepath.Join(dir, "guides"), "one.md", "<!-- Space: DOCS -->\n<!-- Title: One -->\n\nOne.\n")
	writeFile(t, filepath.Join(dir, "guides"), "two.md", "<!-- Space: docs -->\n<!-- Title: Two -->\n\nTwo.\n")

	config := publishConfig(server.URL, filepath.Join(dir, "**", "*.md"))
	config.ParentsFromPath = true
	config.ParentsFromPathRoot = dir
	config.Page.TitleAppendGeneratedHash = true
	require.NoError(t, Run(context.Background(), config))

	children := server.ChildOrder(home.ID)
	require.Len(t, children, 1, "one page for the directory")
	directory := server.Page(children[0])
	assert.Equal(t, metadata.CaseFoldedHashTitle(nil, "DOCS", "Guides"), directory.Title)
	assert.Len(t, server.ChildOrder(directory.ID), 2, "both documents beneath it")
}
