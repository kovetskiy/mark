package mark

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/export"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportDocument is a page that uses most of what export converts: it is
// published, exported, and published again from the export.
const exportDocument = `<!-- Space: DOCS -->
<!-- Parent: Guides -->
<!-- Parent: Setup -->
<!-- Title: Installing -->
<!-- Label: install -->
<!-- Label: how-to -->

# Installing

Some *emphasis*, **strong** text, ~~struck~~ text and ` + "`code`" + `, with a
[link](https://example.com), a [page link](ac:Guides) and an
[anchor link](#requirements).

## Requirements

1. First
2. Second
   - nested

- [x] done
- [ ] open

| Name | Value |
| --- | :---: |
| a | b |

> [!WARNING]
>
> Careful.

<details>
<summary>More</summary>

Hidden **text**.

</details>

` + "```bash title Install it\nmake install\n\n```" + `

![Logo](logo.png)

See [the report](report.pdf).

<ac:structured-macro ac:name="toc"><ac:parameter ac:name="maxLevel">2</ac:parameter></ac:structured-macro>
`

// exportFixture publishes exportDocument to a fresh fake Confluence and
// returns the server, the page's id and the files the document was published
// from.
func exportFixture(t *testing.T) (*confluencetest.Server, string, string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	guides := server.AddPage("DOCS", "Guides", "page", home.ID)
	server.AddPage("DOCS", "Setup", "page", guides.ID)

	dir := t.TempDir()
	logo, err := os.ReadFile(filepath.Join("testdata", "test.png"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), logo, 0o600))
	writeFile(t, dir, "report.pdf", "%PDF-1.4 report")
	file := writeFile(t, dir, "installing.md", exportDocument)

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, AttachReferenced: true, Features: []string{"mention"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage(context.Background(), "DOCS", "Installing", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	return server, page.ID, dir
}

// setContentProperty writes a property of the page, over the one publishing
// left there if there is one: publishing writes the page's width itself.
func setContentProperty(t *testing.T, api *confluence.API, id, key, value string) {
	t.Helper()

	properties, err := api.ListContentProperties(context.Background(), id)
	require.NoError(t, err)

	var existing *confluence.Property
	for i := range properties {
		if properties[i].Key == key {
			existing = &properties[i]
		}
	}

	require.NoError(t, api.SetContentProperty(context.Background(), id, key, []byte(value), existing))
}

// TestExportRoundTrip exports a page mark published and publishes the export
// back: the page has to come out as it was, and the files beside the export
// have to be the ones that were attached.
func TestExportRoundTrip(t *testing.T) {
	server, id, source := exportFixture(t)
	published := server.Page(id).Body

	api := confluence.NewAPI(server.URL, "user", "token", false)
	setContentProperty(t, api, id, "content-appearance-published", `"fixed"`)
	setContentProperty(t, api, id, "emoji-title-published", `"1f680"`)

	out := filepath.Join(t.TempDir(), "export", "installing.md")
	result, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: id, Output: out,
	})
	require.NoError(t, err)
	assert.Equal(t, out, result.Output)

	exported, err := os.ReadFile(out)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(string(exported), strings.Join([]string{
		"<!-- Space: DOCS -->",
		"<!-- Parent: Guides -->",
		"<!-- Parent: Setup -->",
		"<!-- Title: Installing -->",
		"<!-- Label: how-to -->",
		"<!-- Label: install -->",
		"<!-- Content-Appearance: fixed -->",
		"<!-- Emoji: 🚀 -->",
		"",
		"# Installing",
	}, "\n")), "the headers name the page it came from:\n%s", exported)

	for _, name := range []string{"logo.png", "report.pdf"} {
		want, err := os.ReadFile(filepath.Join(source, name))
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(filepath.Dir(out), name))
		require.NoError(t, err, "the attachment %s is downloaded beside the document", name)
		assert.Equal(t, want, got, name)
	}

	body := string(exported)
	for _, want := range []string{
		"![Logo](logo.png)",
		"[the report](report.pdf)",
		"[page link](ac:Guides)",
		"[anchor link](#Requirements)",
		"> [!WARNING]",
		"<summary>More</summary>",
		"```bash title Install it\nmake install\n\n```",
		"| Name | Value |\n| --- | :---: |\n| a | b |",
		"- [x] done\n- [ ] open",
	} {
		assert.Contains(t, body, want)
	}

	// Published back, the page is what it was.
	server.EditPage(id, "<p>edited in Confluence</p>")
	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: out, AttachReferenced: true, Features: []string{"mention"},
		Output: io.Discard,
	}))

	assert.Equal(t, published, server.Page(id).Body)

	var names []string
	for _, attachment := range server.Attachments(id) {
		names = append(names, attachment.Filename)
	}
	assert.ElementsMatch(t, []string{"logo.png", "report.pdf"}, names,
		"publishing the export back uploads nothing under a new name")
}

// TestExportPageFromConfluence exports storage format Confluence's own editor
// writes rather than mark: what has no Markdown form is kept as it is, and the
// attachments it refers to are declared so that they are uploaded again.
func TestExportPageFromConfluence(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	post := server.AddPage("DOCS", "Release notes", "blogpost", "")
	server.EditPage(post.ID, `<p>Hi <ac:link><ri:user ri:account-id="abc123"/></ac:link>,</p>`+
		`<table><tbody><tr><td colspan="2"><ac:image><ri:attachment ri:filename="chart.png"/></ac:image></td></tr></tbody></table>`+
		`<ac:structured-macro ac:name="info" ac:schema-version="1"><ac:rich-text-body><p>Plain <em>info</em>.</p></ac:rich-text-body></ac:structured-macro>`+
		`<p><ac:image ac:align="center"><ri:attachment ri:filename="missing.png"/></ac:image></p>`)
	server.AddAttachmentData(post.ID, "chart.png", []byte("chart"))
	server.AddAttachmentData(post.ID, "unused.txt", []byte("not referred to"))

	dir := t.TempDir()
	out := filepath.Join(dir, "notes.md")
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		Space: "DOCS", Title: "Release notes",
		Output: out, AttachmentsDir: filepath.Join(dir, "files"),
	})
	require.NoError(t, err)

	exported, err := os.ReadFile(out)
	require.NoError(t, err)

	assert.Equal(t, "<!-- Space: DOCS -->\n"+
		"<!-- Type: blogpost -->\n"+
		"<!-- Title: Release notes -->\n"+
		"<!-- Content-Appearance: default -->\n"+
		"<!-- Attachment: files/chart.png -->\n"+
		"\n"+
		`Hi <ac:link><ri:user ri:account-id="abc123"/></ac:link>,`+"\n"+
		"\n"+
		`<table><tbody><tr><td colspan="2"><ac:image><ri:attachment ri:filename="files_chart.png"/></ac:image></td></tr></tbody></table>`+"\n"+
		"\n"+
		`<ac:structured-macro ac:name="info" ac:schema-version="1"><ac:rich-text-body>`+"\n"+
		"\n"+
		"Plain *info*.\n"+
		"\n"+
		"</ac:rich-text-body></ac:structured-macro>\n"+
		"\n"+
		"![](files/missing.png)\n", string(exported))

	chart, err := os.ReadFile(filepath.Join(dir, "files", "chart.png"))
	require.NoError(t, err)
	assert.Equal(t, "chart", string(chart))

	_, err = os.Stat(filepath.Join(dir, "files", "unused.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist, "an attachment the page does not refer to is not downloaded")

	// Published back, the attachment kept as storage format is uploaded under
	// the name the storage format now gives it.
	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: out, Features: []string{"mention"}, Output: io.Discard,
	}))

	var uploaded []string
	for _, attachment := range server.Attachments(post.ID) {
		uploaded = append(uploaded, attachment.Filename)
	}
	assert.Contains(t, uploaded, "files_chart.png")
	assert.Contains(t, server.Page(post.ID).Body, `<ri:attachment ri:filename="files_chart.png"/>`)
}

// TestExportRefusesToOverwrite: an export that would replace a file fails
// before it writes anything, unless told to replace it.
func TestExportRefusesToOverwrite(t *testing.T) {
	server, id, _ := exportFixture(t)

	dir := t.TempDir()
	out := writeFile(t, dir, "installing.md", "mine")

	config := ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: id, Output: out,
	}

	_, err := Export(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--overwrite")

	kept, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "mine", string(kept))
	_, err = os.Stat(filepath.Join(dir, "logo.png"))
	assert.ErrorIs(t, err, os.ErrNotExist, "nothing is written when the export is refused")

	config.Overwrite = true
	_, err = Export(context.Background(), config)
	require.NoError(t, err)

	replaced, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(replaced), "<!-- Title: Installing -->")
}

// TestExportOfAPageThatIsNotThere names the page it looked for.
func TestExportOfAPageThatIsNotThere(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		Space: "DOCS", Title: "Nowhere",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no page or blog post titled "Nowhere" in space DOCS`)
}

// TestExportWithoutAttachments writes the document alone, without asking for
// the page's attachments: a token that may not read them exports the page.
func TestExportWithoutAttachments(t *testing.T) {
	server, id, _ := exportFixture(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		return http.StatusForbidden, `{}`, strings.Contains(r.URL.Path, "/child/attachment")
	})

	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	result, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: id, NoAttachments: true, Stdout: &stdout,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Attachments)
	assert.Empty(t, result.Output, "without --output the document goes to standard output")

	assert.Contains(t, stdout.String(), "<!-- Title: Installing -->")
	assert.Contains(t, stdout.String(), "![Logo](logo.png)", "the document still refers to the image")

	_, err = os.Stat("logo.png")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// TestExportWithoutAttachmentsDeclaresThemAll: without the page's attachments
// there is no telling which it has, so an attachment storage format refers to
// is declared as the document refers to it.
func TestExportWithoutAttachmentsDeclaresThemAll(t *testing.T) {
	server, id := exportPage(t,
		`<table><tbody><tr><td colspan="2"><ac:image><ri:attachment ri:filename="kept.png"/></ac:image></td></tr></tbody></table>`)

	var stdout bytes.Buffer
	logged := captureLog(t, func() {
		_, err := Export(context.Background(), ExportConfig{
			BaseURL: server.URL, Username: "user", Password: "token", PageID: id,
			NoAttachments: true, Stdout: &stdout,
		})
		require.NoError(t, err)
	})
	assert.Contains(t, stdout.String(), "<!-- Attachment: kept.png -->")
	assert.NotContains(t, logged, "does not have")
	assert.Zero(t, server.CountRequests("GET", "/child/attachment"))
}

// TestExportThroughTheGateway exports through the api.atlassian.com gateway,
// where every read goes to v2 -- the space key and the labels included, which
// v2 hands over differently from v1.
func TestExportThroughTheGateway(t *testing.T) {
	server, id, _ := exportFixture(t)

	out := filepath.Join(t.TempDir(), "installing.md")
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL + "/ex/confluence/cloud-id", Password: "token",
		PageID: id, Output: out,
	})
	require.NoError(t, err)

	exported, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(exported), "<!-- Space: DOCS -->\n<!-- Parent: Guides -->\n<!-- Parent: Setup -->\n"+
		"<!-- Title: Installing -->\n<!-- Label: how-to -->\n<!-- Label: install -->\n")

	logo, err := os.ReadFile(filepath.Join(filepath.Dir(out), "logo.png"))
	require.NoError(t, err)
	assert.NotEmpty(t, logo)
}

// TestExportThroughTheGatewayWithoutTheSpace: a token that may read the page
// but not its space exports it into the space it is told.
func TestExportThroughTheGatewayWithoutTheSpace(t *testing.T) {
	server, id, _ := exportFixture(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		return http.StatusUnauthorized, `{"code":401,"message":"Unauthorized; scope does not match"}`,
			strings.Contains(r.URL.Path, "/api/v2/spaces/")
	})

	config := ExportConfig{
		BaseURL: server.URL + "/ex/confluence/cloud-id", Password: "token",
		PageID: id, NoAttachments: true, Stdout: io.Discard,
	}
	_, err := Export(context.Background(), config)
	require.ErrorContains(t, err, "unable to tell which space")

	var stdout bytes.Buffer
	config.Space, config.Stdout = "DOCS", &stdout
	_, err = Export(context.Background(), config)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "<!-- Space: DOCS -->\n")
}

// TestExportOfAPageInAFolder: v1's ancestors leave folders out, so the folders
// above a page are read from v2 and written as Folder headers, which publish the
// page back into the folder it was in.
func TestExportOfAPageInAFolder(t *testing.T) {
	for _, test := range []struct {
		name    string
		guides  bool
		headers string
	}{
		{"below a page", true, "<!-- Parent: Guides -->\n<!-- Folder: Area -->\n"},
		// Folder headers alone look for the folder at the root of the space.
		{"below the home page", false, "<!-- Parent: Home -->\n<!-- Folder: Area -->\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			above := home
			if test.guides {
				above = server.AddPage("DOCS", "Guides", "page", home.ID)
			}
			folder := server.AddFolder("DOCS", "Area", above.ID, "page")
			inside := server.AddPage("DOCS", "Inside", "page", folder.ID)
			server.EditPage(inside.ID, `<p>In the folder.</p>`)

			out := filepath.Join(t.TempDir(), "inside.md")
			_, err := Export(context.Background(), ExportConfig{
				BaseURL: server.URL, Username: "user", Password: "token",
				PageID: inside.ID, Output: out,
			})
			require.NoError(t, err)

			exported, err := os.ReadFile(out)
			require.NoError(t, err)
			assert.Contains(t, string(exported), "<!-- Space: DOCS -->\n"+test.headers+"<!-- Title: Inside -->\n")

			require.NoError(t, Run(context.Background(), Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: out, Output: io.Discard,
			}))
			assert.Equal(t, folder.ID, server.Page(inside.ID).ParentID, "the page stays in its folder")
			assert.Len(t, server.Folders(), 1, "no folder is created")
		})
	}
}

// TestExportOfAPageBelowAFolderSays: a folder above a page that is not directly
// above the exported one cannot be said with headers, and the export says so.
func TestExportOfAPageBelowAFolderSays(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	folder := server.AddFolder("DOCS", "Area", home.ID, "page")
	topic := server.AddPage("DOCS", "Topic", "page", folder.ID)
	inside := server.AddPage("DOCS", "Inside", "page", topic.ID)
	server.EditPage(inside.ID, `<p>Below the folder.</p>`)

	var stdout bytes.Buffer
	logged := captureLog(t, func() {
		_, err := Export(context.Background(), ExportConfig{
			BaseURL: server.URL, Username: "user", Password: "token",
			PageID: inside.ID, Stdout: &stdout,
		})
		require.NoError(t, err)
	})

	assert.Contains(t, stdout.String(), "<!-- Space: DOCS -->\n<!-- Parent: Topic -->\n<!-- Title: Inside -->\n")
	assert.NotContains(t, stdout.String(), "Folder:")
	assert.Contains(t, logged, `folder \"Area\"`)
}

// exportPage puts a page with body into a fresh fake Confluence.
func exportPage(t *testing.T, body string) (*confluencetest.Server, string) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	page := server.AddPage("DOCS", "Page", "page", home.ID)
	server.EditPage(page.ID, body)

	return server, page.ID
}

// TestExportDeclaresOnlyAttachmentsThePageHas: a reference kept as storage
// format to an attachment the page no longer has is left alone, but not
// declared: a header naming a file that is not there fails the publish.
func TestExportDeclaresOnlyAttachmentsThePageHas(t *testing.T) {
	server, id := exportPage(t,
		`<table><tbody><tr><td colspan="2"><ac:image><ri:attachment ri:filename="gone.png"/></ac:image>`+
			`<ac:image><ri:attachment ri:filename="kept.png"/></ac:image></td></tr></tbody></table>`)
	server.AddAttachmentData(id, "kept.png", []byte("kept"))

	out := filepath.Join(t.TempDir(), "page.md")
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token", PageID: id, Output: out,
	})
	require.NoError(t, err)

	exported, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(exported), "<!-- Attachment: kept.png -->")
	assert.NotContains(t, string(exported), "<!-- Attachment: gone.png -->")

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: out, Output: io.Discard,
	}), "the export publishes")
}

// TestExportGivesEveryAttachmentAFile: names that flatten to one local name,
// or differ only in case, would otherwise share a file, the second download
// replacing the first.
func TestExportGivesEveryAttachmentAFile(t *testing.T) {
	server, id := exportPage(t, `<p><ac:image><ri:attachment ri:filename="a/b.png"/></ac:image>`+
		`<ac:image><ri:attachment ri:filename="a_b.png"/></ac:image>`+
		`<ac:image><ri:attachment ri:filename="Image.png"/></ac:image>`+
		`<ac:image><ri:attachment ri:filename="image.png"/></ac:image></p>`)
	for _, name := range []string{"a/b.png", "a_b.png", "Image.png", "image.png"} {
		server.AddAttachmentData(id, name, []byte(name))
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "page.md")
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token", PageID: id, Output: out,
	})
	require.NoError(t, err)

	exported, err := os.ReadFile(out)
	require.NoError(t, err)
	for file, content := range map[string]string{
		"a_b.png": "a/b.png", "a_b-2.png": "a_b.png", "Image.png": "Image.png", "image-2.png": "image.png",
	} {
		assert.Contains(t, string(exported), "("+file+")")
		data, err := os.ReadFile(filepath.Join(dir, file))
		require.NoError(t, err, file)
		assert.Equal(t, content, string(data), file)
	}
}

// TestExportKeepsAttachmentsOffTheDocument: an attachment named like the
// document, or like it but for case, which is one file on macOS and Windows,
// gets a name of its own rather than replacing the document under --overwrite.
func TestExportKeepsAttachmentsOffTheDocument(t *testing.T) {
	server, id := exportPage(t, `<p><ac:link><ri:attachment ri:filename="page.md"/></ac:link>`+
		`<ac:link><ri:attachment ri:filename="Page.md"/></ac:link></p>`)
	server.AddAttachmentData(id, "page.md", []byte("lower"))
	server.AddAttachmentData(id, "Page.md", []byte("upper"))

	dir := t.TempDir()
	out := filepath.Join(dir, "page.md")
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token", PageID: id, Output: out, Overwrite: true,
	})
	require.NoError(t, err)

	exported, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(exported), "<!-- Title: Page -->")
	assert.Contains(t, string(exported), "(page-2.md)")
	assert.Contains(t, string(exported), "(Page-3.md)")

	for file, content := range map[string]string{"page-2.md": "lower", "Page-3.md": "upper"} {
		data, err := os.ReadFile(filepath.Join(dir, file))
		require.NoError(t, err, file)
		assert.Equal(t, content, string(data), file)
	}
}

// TestAFailedDownloadWritesNothing: the attachments are moved into place only
// once every one of them is downloaded, so a retry is not refused for the
// files a failed export left behind.
func TestAFailedDownloadWritesNothing(t *testing.T) {
	server, id := exportPage(t, `<p><ac:image><ri:attachment ri:filename="first.png"/></ac:image>`+
		`<ac:image><ri:attachment ri:filename="second.png"/></ac:image></p>`)
	server.AddAttachmentData(id, "first.png", []byte("first"))
	server.AddAttachmentData(id, "second.png", []byte("second"))
	server.SetFail(func(r *http.Request) (int, string, bool) {
		return http.StatusNotFound, `{}`, strings.HasSuffix(r.URL.Path, "/second.png")
	})

	dir := t.TempDir()
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token", PageID: id,
		Output: filepath.Join(dir, "page.md"),
	})
	require.Error(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// TestExportKeepsTheMaxAppearance: an appearance mark publishes is written
// back, so that publishing the export does not make the page full width.
func TestExportKeepsTheMaxAppearance(t *testing.T) {
	server, id := exportPage(t, `<p>Body.</p>`)
	setContentProperty(t, confluence.NewAPI(server.URL, "user", "token", false), id,
		"content-appearance-published", `"max"`)

	var stdout bytes.Buffer
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token", PageID: id, Stdout: &stdout,
	})
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "<!-- Content-Appearance: max -->")
}

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

	for title, p := range pages {
		server.EditPage(p.ID, `<p>`+title+`</p><p><ac:image><ri:attachment ri:filename="image.png"/></ac:image></p>`)
		server.AddAttachmentData(p.ID, "image.png", []byte("image of "+title))
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
	for title, p := range pages {
		before[title] = server.Page(p.ID).ParentID
		server.EditPage(p.ID, "<p>edited in Confluence</p>")
	}

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(out, "**", "*.md"), AttachReferenced: true, Features: []string{"mention"},
		Output: io.Discard,
	}))

	for title, p := range pages {
		if title == "Home" {
			continue
		}
		got := server.Page(p.ID)
		assert.Contains(t, got.Body, "<p>"+title+"</p>", "%s is published back to the page it came from", title)
		assert.Equal(t, before[title], got.ParentID, "%s keeps its parent", title)

		var names []string
		for _, attachment := range server.Attachments(p.ID) {
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

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(out, "**", "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}))

	body := server.Page(pages["Guide"].ID).Body
	assert.Contains(t, body, "/x/", "a relative link is published as a link to the page")
	assert.Contains(t, body, `ri:content-title="Home"`)
	assert.Len(t, server.ChildOrder(pages["Home"].ID), 1, "no page was created")
}

// TestExportTreeRelativeLinksFromURLs: with RelativeLinks, a URL of a page of
// the tree pasted into a body -- a short link, which is what mark publishes a
// relative link as, or the address a browser shows -- points at its document
// too, and one showing its own address shows the page's title. One with a
// fragment, one to another instance, one to a page outside the tree, one to
// the page's editor and a dated /display/ one, which names a blog post, stay
// URLs.
func TestExportTreeRelativeLinksFromURLs(t *testing.T) {
	server, pages := exportTreeSpace(t)
	tiny, err := page.GenerateTinyLink(server.URL, pages["C++"].ID)
	require.NoError(t, err)
	a := func(href, text string) string { return `<a href="` + href + `">` + text + `</a>` }

	server.EditPage(pages["Guide"].ID, `<p>`+
		a(tiny, "short")+` `+
		a(server.URL+"/spaces/DOCS/pages/"+pages["C"].ID+"/C", "browser")+` `+
		a(server.URL+"/pages/viewpage.action?pageId="+pages["Deep dive"].ID, "server")+` `+
		a(server.URL+"/display/DOCS/Deep+dive", "display")+` `+
		a(tiny+"#Section", "fragment")+` `+
		a("https://elsewhere.example.com/x/"+strings.TrimPrefix(tiny, server.URL+"/x/"), "elsewhere")+` `+
		a(server.URL+"/spaces/DOCS/pages/"+pages["Home"].ID+"/Home", "outside")+` `+
		a(server.URL+"/pages/editpage.action?pageId="+pages["C"].ID, "edit")+` `+
		a(server.URL+"/display/DOCS/2024/01/31/Deep+dive", "blog")+` `+
		a(server.URL+"/pages/viewpage.action?pageId="+pages["Deep dive"].ID,
			server.URL+"/pages/viewpage.action?pageId="+pages["Deep dive"].ID)+
		`</p>`)
	out := filepath.Join(t.TempDir(), "guide")

	_, err = ExportTree(context.Background(), ExportConfig{
		BaseURL: server.URL, Username: "user", Password: "token",
		PageID: pages["Guide"].ID, Output: out,
	}, export.TreeOptions{RelativeLinks: true})
	require.NoError(t, err)

	guide, err := os.ReadFile(filepath.Join(out, "index.md"))
	require.NoError(t, err)
	assert.Contains(t, string(guide), "[short](c/index.md) "+
		"[browser](c-"+pages["C"].ID+"/index.md) "+
		"[server](c/deep-dive/index.md) "+
		"[display](c/deep-dive/index.md) "+
		"[fragment]("+tiny+"#Section) "+
		"[elsewhere](https://elsewhere.example.com/x/")
	assert.Contains(t, string(guide), "[outside]("+server.URL+"/spaces/DOCS/pages/"+pages["Home"].ID+"/Home) "+
		"[edit]("+server.URL+"/pages/editpage.action?pageId="+pages["C"].ID+") "+
		"[blog]("+server.URL+"/display/DOCS/2024/01/31/Deep+dive) "+
		"[Deep dive](c/deep-dive/index.md)")

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(out, "**", "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}))
	assert.Contains(t, server.Page(pages["Guide"].ID).Body, tiny, "published back as a link to the page")
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
