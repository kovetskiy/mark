package mark

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/export"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// An OKF export of one page is a concept: the page's body below front matter
// naming what it is, where it is, its labels as tags, and the page as its
// source, with who last edited it and when. A link to another page, in this
// space or another, is a link to that page's address; one to an anchor on the
// page itself stays on the page. An attachment ending in .md is not left to
// read as a concept.
func TestExportOKFPage(t *testing.T) {
	server, pages := exportTreeSpace(t)
	guide := pages["Guide"]
	guide.When = "2026-05-01T12:30:00.000+02:00"
	server.AddLabel(guide.ID, "zeta")
	server.AddLabel(guide.ID, "alpha")
	other := server.AddPage("OPS", "Runbook", "page", "")
	server.EditPage(guide.ID, `<p>See `+pageLink("C++")+`, `+pageLink("Nowhere")+`.</p>`+
		`<p><ac:link ac:anchor="Install"><ri:page ri:content-title="Guide"/><ac:plain-text-link-body><![CDATA[up]]></ac:plain-text-link-body></ac:link>`+
		` <ac:link ac:anchor="x"><ri:page ri:content-title="C"/><ac:plain-text-link-body><![CDATA[C]]></ac:plain-text-link-body></ac:link>`+
		` <ac:link><ri:page ri:space-key="OPS" ri:content-title="Runbook"/><ac:plain-text-link-body><![CDATA[ops]]></ac:plain-text-link-body></ac:link>`+
		` <ac:link><ri:page ri:space-key="OPS" ri:content-title="Gone"/><ac:plain-text-link-body><![CDATA[gone]]></ac:plain-text-link-body></ac:link></p>`+
		`<p><ac:link><ri:attachment ri:filename="notes.md"/></ac:link></p>`)
	server.AddAttachmentData(guide.ID, "notes.md", []byte("# notes"))
	dir := t.TempDir()

	result, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: guide.ID, Output: filepath.Join(dir, "guide.md"),
		Format: export.FormatOKF, Generator: "mark/v1.2.3",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "zeta"}, result.Labels, "the result names the labels the tags do")
	assert.Nil(t, result.DeclaredAttachments, "OKF declares no attachments")

	data, err := os.ReadFile(filepath.Join(dir, "guide.md"))
	require.NoError(t, err)
	document := string(data)

	url := server.URL + "/display/DOCS/" + guide.ID
	assert.True(t, strings.HasPrefix(document, `---
type: Confluence Page
title: Guide
resource: `+url+`
tags:
  - alpha
  - zeta
generated:
  by: mark/v1.2.3
sources:
  - id: confluence
    resource: `+url+`
    title: Guide
    author: human:`), document)
	assert.Contains(t, document, "    last_modified: \"2026-05-01T10:30:00Z\"\n---\n")
	assert.NotContains(t, document, "<!--", "none of mark's headers")

	assert.Contains(t, document, "See [C++]("+server.URL+"/display/DOCS/"+pages["C++"].ID+"), [Nowhere](ac:Nowhere).",
		"a page is linked by its address; one that is not there keeps its title")
	assert.Contains(t, document, "[up](#Install)", "an anchor on the page itself stays on the page")
	assert.Contains(t, document, "[C]("+server.URL+"/display/DOCS/"+pages["C"].ID+")",
		"a page outside the bundle is linked by its address, which has no anchor")
	assert.Contains(t, document, "[ops]("+server.URL+"/display/OPS/"+other.ID+")", "a page in another space too")
	assert.Contains(t, document, `<ri:page ri:space-key="OPS" ri:content-title="Gone"/>`,
		"one there is not stays as it was")
	assert.Contains(t, document, "(notes.md.txt)")
	_, err = os.Stat(filepath.Join(dir, "notes.md.txt"))
	assert.NoError(t, err)
}

// A page an app last edited, such as Automation for Confluence, names it as a
// process: human: is what consumers trust as written by a person.
func TestExportOKFPageByAnAppIsAProcess(t *testing.T) {
	server, pages := exportTreeSpace(t)
	server.SetCurrentUser(confluencetest.User{AccountID: "557058:automation", AccountType: "app"})
	rule := server.AddPage("DOCS", "Rule", "page", pages["Home"].ID)
	dir := t.TempDir()

	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: rule.ID, Output: filepath.Join(dir, "guide.md"),
		Format: export.FormatOKF, Generator: "mark/v1.2.3",
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "guide.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "    author: process:557058:automation\n")
	assert.NotContains(t, string(data), "human:")
}

// An OKF tree is a bundle: each page a concept beside the directory holding
// the pages beneath it, an index.md in every directory, attachments under
// references/, and links between the pages relative links, anchors kept.
func TestExportOKFTree(t *testing.T) {
	server, pages := exportTreeSpace(t)
	server.EditPage(pages["Deep dive"].ID, `<p>Back to `+pageLink("Guide")+` or `+pageLink("C")+`.</p>`+
		`<p>See <ac:link ac:anchor="Setup"><ri:page ri:content-title="Guide"/><ac:plain-text-link-body><![CDATA[setup]]></ac:plain-text-link-body></ac:link>.</p>`+
		`<p><ac:image><ri:attachment ri:filename="image.png"/></ac:image></p>`)
	out := filepath.Join(t.TempDir(), "bundle")

	results, err := ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: pages["Guide"].ID, Output: out, Format: export.FormatOKF,
	}, export.TreeOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 4)

	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(path)))
		require.NoError(t, err, path)
		return string(data)
	}

	c := "c-" + pages["C"].ID
	assert.Equal(t, `---
okf_version: "0.2"
---

# Guide

* [Guide](guide.md)
* [Pages beneath Guide](guide/)
`, read("index.md"))
	assert.Equal(t, "# Guide\n\n* [C++](c.md)\n* [C]("+c+".md)\n* [Pages beneath C++](c/)\n", read("guide/index.md"))
	assert.Equal(t, "# C++\n\n* [Deep dive](deep-dive.md)\n", read("guide/c/index.md"))

	assert.Contains(t, read("guide.md"), "title: Guide\n")
	assert.Contains(t, read("guide.md"), "generated:\n  by: mark/unknown\n", "a producer with no version")
	assert.Contains(t, read("guide.md"), "![](references/attachments/"+pages["Guide"].ID+"/image.png)")
	assert.Equal(t, "image of Guide", read("references/attachments/"+pages["Guide"].ID+"/image.png"))

	deep := read("guide/c/deep-dive.md")
	assert.Contains(t, deep, "Back to [Guide](../../guide.md) or [C](../"+c+".md).")
	assert.Contains(t, deep, "See [setup](../../guide.md#Setup).", "a page in the bundle keeps the anchor")
	assert.Contains(t, deep, "![](../../references/attachments/"+pages["Deep dive"].ID+"/image.png)")

	assertOKFBundle(t, out)

	err = exportTreeOKF(t, server, pages["Guide"].ID, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(out, "index.md"), "an index is not overwritten either")
}

// A space exported as OKF has no document for its home page to be: the home
// page is a concept beside the space's other top-level pages, and a page
// whose title would take a name the bundle reserves takes its id as well.
func TestExportOKFSpace(t *testing.T) {
	server, pages := exportTreeSpace(t)
	index := server.AddPage("DOCS", "Index", "page", "")
	references := server.AddPage("DOCS", "References", "page", "")
	logPage := server.AddPage("DOCS", "Log", "page", pages["Guide"].ID)
	out := filepath.Join(t.TempDir(), "bundle")

	_, err := ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		Space: "DOCS", Output: out, Format: export.FormatOKF,
	}, export.TreeOptions{})
	require.NoError(t, err)

	for _, path := range []string{
		"home.md", "home/guide.md", "home/guide/c.md",
		"index-" + index.ID + ".md", "references-" + references.ID + ".md",
		"home/guide/log-" + logPage.ID + ".md",
	} {
		_, err := os.Stat(filepath.Join(out, filepath.FromSlash(path)))
		assert.NoError(t, err, path)
	}

	root, err := os.ReadFile(filepath.Join(out, "index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(root), "# DOCS\n")
	assert.Contains(t, string(root), "* [Home](home.md)\n")

	assertOKFBundle(t, out)
}

func TestExportRefusesAnUnknownFormat(t *testing.T) {
	server, pages := exportTreeSpace(t)

	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: pages["Guide"].ID, Stdout: &bytes.Buffer{}, Format: "html",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown export format "html"`)
}

func exportTreeOKF(t *testing.T, server *confluencetest.Server, pageID, out string) error {
	t.Helper()

	_, err := ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: pageID, Output: out, Format: export.FormatOKF,
	}, export.TreeOptions{})
	return err
}

// assertOKFBundle checks what OKF requires of a bundle: every .md file but
// index.md and log.md has front matter with a type, and an index.md has none,
// except the bundle's own, which may say the version.
func assertOKFBundle(t *testing.T, root string) {
	t.Helper()

	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		rel, _ := filepath.Rel(root, path)

		if entry.Name() == "index.md" {
			if rel != "index.md" {
				assert.False(t, strings.HasPrefix(string(data), "---"), "%s has front matter", rel)
			}
			return nil
		}

		front, _, ok := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
		require.True(t, ok && strings.HasPrefix(string(data), "---\n"), "%s has no front matter", rel)
		var concept map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(front), &concept), rel)
		assert.NotEmpty(t, concept["type"], "%s has no type", rel)
		return nil
	}))
}

// An OKF concept is read, not published back, so nothing mark's own syntax
// cannot say is lost or left as storage format: the article layout's sidebar
// stays in the body, and a page link is written whatever its text or anchor,
// to the page itself, since the anchors of another page are not carried into
// its document.
func TestExportOKFPageKeepsWhatMarkCannotSay(t *testing.T) {
	server, pages := exportTreeSpace(t)
	guide := pages["Guide"]
	server.EditPage(guide.ID, `<ac:layout><ac:layout-section ac:type="two_right_sidebar">`+
		`<ac:layout-cell><p>Body, see `+
		`<ac:link><ri:page ri:content-title="C++"/><ac:link-body><strong>C</strong>++</ac:link-body></ac:link>`+
		` and <ac:link ac:anchor="set up"><ri:page ri:content-title="C"/><ac:plain-text-link-body><![CDATA[C setup]]></ac:plain-text-link-body></ac:link>`+
		` and <ac:link ac:card-appearance="inline"><ri:page ri:content-title="Deep dive" ri:version-at-save="3"/></ac:link>.</p></ac:layout-cell>`+
		`<ac:layout-cell><p>Sidebar, see `+pageLink("Deep dive")+`.</p></ac:layout-cell>`+
		`</ac:layout-section></ac:layout>`)

	var stdout bytes.Buffer
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: guide.ID, Stdout: &stdout, Format: export.FormatOKF,
	})
	require.NoError(t, err)
	document := stdout.String()

	address := func(title string) string { return server.URL + "/display/DOCS/" + pages[title].ID }
	assert.Contains(t, document, "see [**C**++]("+address("C++")+")")
	assert.Contains(t, document, "[C setup]("+address("C")+")")
	assert.Contains(t, document, " and [Deep dive]("+address("Deep dive")+").",
		"an inline card is a link like any other")
	assert.Contains(t, document, "Sidebar, see [Deep dive]("+address("Deep dive")+").")
}

// Through the gateway a space that is not there, or that the token cannot see,
// is not found, and a link into it is left as it was rather than failing the
// export.
func TestExportOKFPageThroughTheGatewayLinksIntoAMissingSpace(t *testing.T) {
	server, pages := exportTreeSpace(t)
	server.EditPage(pages["Guide"].ID, `<p>See `+pageLink("C++")+`,`+
		` <ac:link><ri:page ri:space-key="GONE" ri:content-title="Old"/><ac:plain-text-link-body><![CDATA[old]]></ac:plain-text-link-body></ac:link>.</p>`)

	var stdout bytes.Buffer
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL + "/ex/confluence/cloud-id", Password: "token",
		PageID: pages["Guide"].ID, Stdout: &stdout, Format: export.FormatOKF,
	})
	require.NoError(t, err)
	document := stdout.String()
	assert.Contains(t, document, "See [C++](")
	assert.Contains(t, document, `<ri:page ri:space-key="GONE" ri:content-title="Old"/>`)
}

// A page link the export cannot look up is an error, not a link left pointing
// nowhere: the page may well be there.
func TestExportOKFPageReportsAFailedLookup(t *testing.T) {
	server, pages := exportTreeSpace(t)
	server.EditPage(pages["Guide"].ID, `<p>See `+pageLink("C++")+`.</p>`)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Query().Get("title") == "C++" {
			return http.StatusForbidden, `{"message":"no"}`, true
		}
		return 0, "", false
	})

	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: pages["Guide"].ID, Stdout: &bytes.Buffer{}, Format: export.FormatOKF,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unable to look up page "C++"`)
}

// OKF reads index.md and log.md as a directory's index and log, so a concept
// is never written to either.
func TestExportOKFPageRefusesAReservedName(t *testing.T) {
	server, pages := exportTreeSpace(t)
	dir := t.TempDir()

	for _, name := range []string{"index.md", "LOG.md"} {
		_, err := Export(context.Background(), ExportConfig{
			BaseURL: server.URL, Username: "user", Password: "token",
			PageID: pages["Guide"].ID, Output: filepath.Join(dir, name), Format: export.FormatOKF,
		})
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "OKF reserves the name "+name)
		_, err = os.Stat(filepath.Join(dir, name))
		assert.ErrorIs(t, err, fs.ErrNotExist)
	}
}

// The bundle's own index is found however --output is spelled; titles in an
// index are text, not markup; a name taken by another page's title with an id
// is not given out twice; and an attachment in storage format kept as it is
// names the file it was written to.
func TestExportOKFTreeNamesAndIndexes(t *testing.T) {
	server, pages := exportTreeSpace(t)
	guide := pages["Guide"]
	clash := server.AddPage("DOCS", "placeholder", "page", guide.ID)
	index := server.AddPage("DOCS", "Index", "page", guide.ID)
	clash.Title = "Index-" + index.ID
	server.AddPage("DOCS", "List<T> &copy;", "page", guide.ID)
	server.EditPage(guide.ID, `<ac:structured-macro ac:name="view-file"><ac:parameter ac:name="name">`+
		`<ri:attachment ri:filename="chart.png"/></ac:parameter></ac:structured-macro>`)
	server.AddAttachmentData(guide.ID, "chart.png", []byte("chart"))
	out := filepath.Join(t.TempDir(), "bundle")

	_, err := ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: guide.ID, Output: out + string(filepath.Separator), Format: export.FormatOKF,
	}, export.TreeOptions{Depth: 2})
	require.NoError(t, err)

	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(path)))
		require.NoError(t, err, path)
		return string(data)
	}

	assert.True(t, strings.HasPrefix(read("index.md"), "---\nokf_version: \"0.2\"\n---\n\n# Guide\n"), read("index.md"))

	sub := read("guide/index.md")
	assert.Contains(t, sub, "* [Index-"+index.ID+"](index-"+index.ID+".md)\n")
	assert.Contains(t, sub, "* [Index](index-"+index.ID+"-2.md)\n")
	assert.Contains(t, sub, `* [List\<T> \&copy;](list-t-copy.md)`+"\n")

	assert.Contains(t, read("guide.md"), `ri:filename="references/attachments/`+guide.ID+`/chart.png"`)
	assert.Equal(t, "chart", read("references/attachments/"+guide.ID+"/chart.png"))

	assertOKFBundle(t, out)
}

// The home page of a space exported as OKF is where Confluence lists it among
// the top-level pages, and a home page beneath another page is exported once,
// there.
func TestExportOKFSpaceHomePage(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	alpha := server.AddPage("DOCS", "Alpha", "page", "")
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	out := filepath.Join(t.TempDir(), "bundle")

	run := func(depth int) string {
		t.Helper()
		require.NoError(t, os.RemoveAll(out))
		_, err := ExportTree(context.Background(), ExportConfig{
			BaseURL: server.URL, Username: "user", Password: "token",
			Space: "DOCS", Output: out, Format: "okf",
		}, export.TreeOptions{Depth: depth})
		require.NoError(t, err)
		data, err := os.ReadFile(filepath.Join(out, "index.md"))
		require.NoError(t, err)
		return string(data)
	}

	assert.Contains(t, run(0), "* [Alpha](alpha.md)\n* [Home](home.md)\n")

	server.MovePage(home.ID, alpha.ID)
	root := run(0)
	assert.NotContains(t, root, "(home.md)")
	_, err := os.Stat(filepath.Join(out, "alpha", "home.md"))
	assert.NoError(t, err)

	// Beneath a page the export does not go down to, it is last at the top.
	assert.Contains(t, run(1), "* [Alpha](alpha.md)\n* [Home](home.md)\n")
}

func TestExportOKFEmptySpaceIsABundle(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("EMPTY")
	out := filepath.Join(t.TempDir(), "bundle")

	results, err := ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		Space: "EMPTY", Output: out, Format: export.FormatOKF,
	}, export.TreeOptions{})
	require.NoError(t, err)
	assert.Empty(t, results)

	root, err := os.ReadFile(filepath.Join(out, "index.md"))
	require.NoError(t, err)
	assert.Equal(t, "---\nokf_version: \"0.2\"\n---\n\n# EMPTY\n\n", string(root))
}

// A title as long as Confluence allows, in ASCII or in a script of three
// bytes a character, still makes a file name the file system takes, in
// either layout, and two long titles alike up to the cut keep apart.
func TestExportTreeLongTitles(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	server.SetHomepage("DOCS", root.ID)
	prefix := strings.Repeat("a", 250)
	first := server.AddPage("DOCS", prefix+"1111", "page", root.ID)
	second := server.AddPage("DOCS", prefix+"2222", "page", root.ID)
	cjk := server.AddPage("DOCS", strings.Repeat("文", 255), "page", root.ID)
	server.AddPage("DOCS", strings.Repeat("字", 255), "page", cjk.ID)
	for _, p := range []*confluencetest.Page{root, first, second, cjk} {
		server.EditPage(p.ID, "<p>"+p.ID+"</p>")
	}

	for _, format := range []string{export.FormatMark, export.FormatOKF} {
		t.Run(format, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			_, err := ExportTree(context.Background(), ExportConfig{
				BaseURL: server.URL, Username: "user", Password: "token",
				PageID: root.ID, Output: out, Format: format,
			}, export.TreeOptions{})
			require.NoError(t, err)

			var documents []string
			require.NoError(t, filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
				require.NoError(t, err)
				assert.LessOrEqual(t, len(d.Name()), 255, path)
				if !d.IsDir() && d.Name() != "index.md" {
					documents = append(documents, path)
				}
				return nil
			}))

			cut := prefix[:200]
			firstDoc, secondDoc := filepath.Join(out, cut, "index.md"), filepath.Join(out, cut+"-"+second.ID, "index.md")
			if format == export.FormatOKF {
				firstDoc = filepath.Join(out, "root", cut+".md")
				secondDoc = filepath.Join(out, "root", cut+"-"+second.ID+".md")
				assert.Len(t, documents, 5, documents)
			}
			for doc, id := range map[string]string{firstDoc: first.ID, secondDoc: second.ID} {
				data, err := os.ReadFile(doc)
				require.NoError(t, err)
				assert.Contains(t, string(data), id)
			}
		})
	}
}
