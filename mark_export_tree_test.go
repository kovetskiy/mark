package mark

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/export"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportTreeSpace is a space with a page tree to export: Guide, beneath the
// homepage, with two children whose titles slug alike, and a grandchild.
func exportTreeSpace(t *testing.T) (*confluencetest.Server, map[string]*confluencetest.Page) {
	t.Helper()

	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	pages := map[string]*confluencetest.Page{"Home": home}
	pages["Guide"] = server.AddPage("DOCS", "Guide", "page", home.ID)
	pages["C++"] = server.AddPage("DOCS", "C++", "page", pages["Guide"].ID)
	pages["C"] = server.AddPage("DOCS", "C", "page", pages["Guide"].ID)
	pages["Deep dive"] = server.AddPage("DOCS", "Deep dive", "page", pages["C++"].ID)

	for title, page := range pages {
		server.EditPage(page.ID, `<p>`+title+`</p><p><ac:image><ri:attachment ri:filename="image.png"/></ac:image></p>`)
		server.AddAttachmentData(page.ID, "image.png", []byte("image of "+title))
	}

	return server, pages
}

func exportTree(t *testing.T, server *confluencetest.Server, pageID, output string, depth int, overwrite bool) error {
	t.Helper()

	_, err := ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: pageID, Output: output, Overwrite: overwrite,
	}, export.TreeOptions{Depth: depth})
	return err
}

func TestExportTree(t *testing.T) {
	server, pages := exportTreeSpace(t)
	out := filepath.Join(t.TempDir(), "guide")

	require.NoError(t, exportTree(t, server, pages["Guide"].ID, out, 0, false))

	read := func(path ...string) string {
		data, err := os.ReadFile(filepath.Join(append([]string{out}, path...)...))
		require.NoError(t, err)
		return string(data)
	}

	guide := read("index.md")
	assert.Contains(t, guide, "<!-- Title: Guide -->")
	assert.Contains(t, guide, "![](image.png)", "an attachment is referred to beside the document")
	assert.Equal(t, "image of Guide", read("image.png"))

	cpp := read("c", "index.md")
	assert.Contains(t, cpp, "<!-- Parent: Guide -->\n<!-- Title: C++ -->", "the headers publish it back where it was")
	assert.Equal(t, "image of C++", read("c", "image.png"), "each page's attachments in its own directory")

	assert.Contains(t, read("c-"+pages["C"].ID, "index.md"), "<!-- Title: C -->",
		"a title that slugs like a sibling's takes the page id")
	assert.Equal(t, "image of C", read("c-"+pages["C"].ID, "image.png"))

	deep := read("c", "deep-dive", "index.md")
	assert.Contains(t, deep, "<!-- Parent: Guide -->\n<!-- Parent: C++ -->\n<!-- Title: Deep dive -->")
}

func TestExportTreeDepth(t *testing.T) {
	server, pages := exportTreeSpace(t)
	out := filepath.Join(t.TempDir(), "guide")

	require.NoError(t, exportTree(t, server, pages["Guide"].ID, out, 2, false))

	var documents []string
	require.NoError(t, filepath.WalkDir(out, func(path string, _ os.DirEntry, err error) error {
		if strings.HasSuffix(path, "index.md") {
			rel, _ := filepath.Rel(out, path)
			documents = append(documents, filepath.ToSlash(rel))
		}
		return err
	}))
	assert.ElementsMatch(t, []string{"index.md", "c/index.md", "c-" + pages["C"].ID + "/index.md"}, documents,
		"two levels, counting the page itself")
}

func TestExportTreeWritesNothingItCannotFinish(t *testing.T) {
	t.Run("a folder in the tree", func(t *testing.T) {
		server, pages := exportTreeSpace(t)
		server.AddFolder("DOCS", "Folder", pages["C"].ID, "page")
		out := filepath.Join(t.TempDir(), "guide")

		err := exportTree(t, server, pages["Guide"].ID, out, 0, false)
		require.ErrorContains(t, err, "does not reach into folders")
		assert.NoDirExists(t, out)
	})

	t.Run("a document already there", func(t *testing.T) {
		server, pages := exportTreeSpace(t)
		out := filepath.Join(t.TempDir(), "guide")
		require.NoError(t, os.MkdirAll(filepath.Join(out, "c", "deep-dive"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(out, "c", "deep-dive", "index.md"), []byte("mine"), 0o600))

		err := exportTree(t, server, pages["Guide"].ID, out, 0, false)
		require.ErrorContains(t, err, "--overwrite")
		assert.NoFileExists(t, filepath.Join(out, "index.md"), "nothing was written")

		require.NoError(t, exportTree(t, server, pages["Guide"].ID, out, 0, true))
		assert.FileExists(t, filepath.Join(out, "index.md"))
	})

	t.Run("no directory to write to", func(t *testing.T) {
		server, pages := exportTreeSpace(t)
		require.ErrorContains(t, exportTree(t, server, pages["Guide"].ID, "", 0, false), "--output")
	})
}

// TestExportTreeRoundTrip publishes an exported tree back: every page has to
// land on the page it came from, under the parent it had, with no new pages
// and no attachment uploaded under a new name.
func TestExportTreeRoundTrip(t *testing.T) {
	server, pages := exportTreeSpace(t)
	out := filepath.Join(t.TempDir(), "guide")
	require.NoError(t, exportTree(t, server, pages["Guide"].ID, out, 0, false))

	before := map[string]string{}
	for title, page := range pages {
		before[title] = server.Page(page.ID).ParentID
		server.EditPage(page.ID, "<p>edited in Confluence</p>")
	}

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(out, "**", "*.md"), AttachReferenced: true, Features: []string{"mention"},
		Output: io.Discard,
	}))

	for title, page := range pages {
		if title == "Home" {
			continue
		}
		got := server.Page(page.ID)
		assert.Contains(t, got.Body, "<p>"+title+"</p>", "%s is published back to the page it came from", title)
		assert.Equal(t, before[title], got.ParentID, "%s keeps its parent", title)

		var names []string
		for _, attachment := range server.Attachments(page.ID) {
			names = append(names, attachment.Filename)
		}
		assert.Equal(t, []string{"image.png"}, names, title)
	}
	assert.Len(t, server.ChildOrder(pages["Home"].ID), 1, "no page was created")
}

func pageLink(title string) string {
	return `<ac:link><ri:page ri:content-title="` + title + `"/><ac:plain-text-link-body><![CDATA[` + title + `]]></ac:plain-text-link-body></ac:link>`
}

// TestExportTreeRelativeLinks: with RelativeLinks, a link to a page of the
// tree points at its document, a link to anything else stays ac:, and the
// directory still publishes back onto the pages it came from.
func TestExportTreeRelativeLinks(t *testing.T) {
	server, pages := exportTreeSpace(t)
	server.EditPage(pages["Guide"].ID, `<p>See `+pageLink("C++")+` and `+pageLink("Home")+`.</p>`)
	server.EditPage(pages["Deep dive"].ID, `<p>Back to `+pageLink("Guide")+` or `+pageLink("C")+`.</p>`)
	out := filepath.Join(t.TempDir(), "guide")

	_, err := ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: pages["Guide"].ID, Output: out,
	}, export.TreeOptions{RelativeLinks: true})
	require.NoError(t, err)

	guide, err := os.ReadFile(filepath.Join(out, "index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(guide), "See [C++](c/index.md) and [Home](ac:Home).",
		"a page outside the tree is still linked by title")

	deep, err := os.ReadFile(filepath.Join(out, "c", "deep-dive", "index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(deep), "Back to [Guide](../../index.md) or [C](../../c-"+pages["C"].ID+"/index.md).")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(out, "**", "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}))

	body := server.Page(pages["Guide"].ID).Body
	assert.Contains(t, body, "/x/", "a relative link is published as a link to the page")
	assert.Contains(t, body, `ri:content-title="Home"`)
	assert.Len(t, server.ChildOrder(pages["Home"].ID), 1, "no page was created")
}

// TestExportSpace exports a whole space: the home page at the top, and a page
// at the top of the space beside the home page's children.
func TestExportSpace(t *testing.T) {
	server, _ := exportTreeSpace(t)
	stray := server.AddPage("DOCS", "Stray", "page", "")
	server.EditPage(stray.ID, "<p>Stray</p>")
	out := filepath.Join(t.TempDir(), "docs")

	results, err := ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		Space: "DOCS", Output: out,
	}, export.TreeOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 6)

	for path, title := range map[string]string{
		"index.md":                   "Home",
		"guide/index.md":             "Guide",
		"guide/c/index.md":           "C++",
		"guide/c/deep-dive/index.md": "Deep dive",
		"stray/index.md":             "Stray",
	} {
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(path)))
		require.NoError(t, err, path)
		assert.Contains(t, string(data), "<!-- Title: "+title+" -->", path)
	}

	t.Run("one level", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "docs")
		results, err := ExportTree(context.Background(), ExportConfig{
			BaseURL: server.URL, Username: "user", Password: "token",
			Space: "DOCS", Output: out,
		}, export.TreeOptions{Depth: 1})
		require.NoError(t, err)

		var titles []string
		for _, result := range results {
			titles = append(titles, result.Page.Title)
		}
		assert.ElementsMatch(t, []string{"Home", "Stray"}, titles, "the top of the space")
	})

}
