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

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, AttachReferenced: true, Features: []string{"mention"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage("DOCS", "Installing", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	return server, page.ID, dir
}

// setContentProperty writes a property of the page, over the one publishing
// left there if there is one: publishing writes the page's width itself.
func setContentProperty(t *testing.T, api *confluence.API, id, key, value string) {
	t.Helper()

	properties, err := api.ListContentProperties(id)
	require.NoError(t, err)

	var existing *confluence.Property
	for i := range properties {
		if properties[i].Key == key {
			existing = &properties[i]
		}
	}

	require.NoError(t, api.SetContentProperty(id, key, []byte(value), existing))
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
	require.NoError(t, Run(Config{
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
	require.NoError(t, Run(Config{
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

// TestExportWithoutAttachments writes the document alone.
func TestExportWithoutAttachments(t *testing.T) {
	server, id, _ := exportFixture(t)

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

	require.NoError(t, Run(Config{
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
