package mark

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/export"
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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				AttachReferenced: true,
				Features:         []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: file,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}))

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
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

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	setContentProperty(t, api, id, "content-appearance-published", `"fixed"`)
	setContentProperty(t, api, id, "emoji-title-published", `"1f680"`)

	out := filepath.Join(t.TempDir(), "export", "installing.md")
	result, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, Output: out,
	})
	require.NoError(t, err)
	assert.Equal(t, out, result.Output)
	assert.Equal(t, []string{"how-to", "install"}, result.Labels, "the result names the labels the headers do")
	assert.Empty(t, result.DeclaredAttachments, "Markdown refers to its attachments without declaring them")

	exported, err := os.ReadFile(out)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(string(exported), strings.Join([]string{
		"<!-- Space: DOCS -->",
		"<!-- Parent: Guides -->",
		"<!-- Parent: Setup -->",
		"<!-- Title: Installing -->",
		"<!-- Label: how-to -->",
		"<!-- Label: install -->",
		"<!-- Content-Appearance: default -->",
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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				AttachReferenced: true,
				Features:         []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: out,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}))

	assert.Equal(t, published, server.Page(id).Body)

	var names []string
	for _, attachment := range server.Attachments(id) {
		names = append(names, attachment.Filename)
	}
	assert.ElementsMatch(t, []string{"logo.png", "report.pdf"}, names,
		"publishing the export back uploads nothing under a new name")
}

// TestExportRoundTripFrontMatter: exported with front matter, the page
// publishes back as it was with the frontmatter feature on.
func TestExportRoundTripFrontMatter(t *testing.T) {
	server, id, _ := exportFixture(t)
	published := server.Page(id).Body

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	setContentProperty(t, api, id, "content-appearance-published", `"fixed"`)
	setContentProperty(t, api, id, "emoji-title-published", `"1f680"`)

	out := filepath.Join(t.TempDir(), "export", "installing.md")
	result, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, Output: out, Metadata: export.MetadataFrontMatter,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"how-to", "install"}, result.Labels, "the result names the labels the front matter does")

	exported, err := os.ReadFile(out)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(string(exported), strings.Join([]string{
		"---",
		"space: DOCS",
		"parents:",
		"  - Guides",
		"  - Setup",
		"title: Installing",
		"labels:",
		"  - how-to",
		"  - install",
		"content-appearance: default",
		`emoji: "🚀"`,
		"---",
		"",
		"# Installing",
	}, "\n")), "the front matter names the page it came from:\n%s", exported)
	assert.NotContains(t, string(exported), "<!-- ")

	server.EditPage(id, "<p>edited in Confluence</p>")
	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				AttachReferenced: true,
				Features:         []string{"mention", "frontmatter"},
			},
			Output: io.Discard,
		},
		Files: out,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}))

	assert.Equal(t, published, server.Page(id).Body)
	assert.Equal(t, "Installing", server.Page(id).Title)
}

// TestExportRoundTripTextLikeDirectives: a page whose text reads like the
// HTML comments mark acts on -- an include, a macro, a header -- publishes
// back from its export as that text, to the same page.
func TestExportRoundTripTextLikeDirectives(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	notes := server.AddPage("DOCS", "Notes", "page", home.ID)
	body := "<p>&lt;!-- Title: Changed --&gt;</p>\n" +
		"<p>&lt;!-- Space: OTHER --&gt;</p>\n" +
		"<p>&lt;!-- Include: outside.txt --&gt;</p>\n" +
		"<p>&lt;!-- Macro: MARKER\nTemplate: outside.txt --&gt;</p>\n" +
		"<p>MARKER</p>\n" +
		`<ac:structured-macro ac:name="noformat"><ac:plain-text-body><![CDATA[<!-- Include: outside.txt -->]]></ac:plain-text-body></ac:structured-macro>` + "\n"
	server.EditPage(notes.ID, body)

	dir := t.TempDir()
	writeFile(t, dir, "outside.txt", "from the file")
	out := filepath.Join(dir, "notes.md")
	_, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     notes.ID, Output: out,
	})
	require.NoError(t, err)

	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{Features: []string{"mention"}},
			Output:    io.Discard,
		},
		Files:      out,
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
	}))

	exported, err := os.ReadFile(out)
	require.NoError(t, err)

	page := server.Page(notes.ID)
	assert.Equal(t, "Notes", page.Title, "exported Markdown:\n%s", exported)
	// The noformat text comes back the same, in two CDATA sections.
	want := strings.Replace(body, "<![CDATA[<!--", "<![CDATA[<]]><![CDATA[!--", 1)
	assert.Equal(t, want, page.Body, "exported Markdown:\n%s", exported)
}

// TestExportOKFIgnoresMetadata: OKF has front matter of its own, and writes
// it whatever the metadata says.
func TestExportOKFIgnoresMetadata(t *testing.T) {
	server, id, _ := exportFixture(t)

	var stdout bytes.Buffer
	_, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, Format: export.FormatOKF, Metadata: export.MetadataFrontMatter,
		Stdout: &stdout, AttachmentsDir: t.TempDir(),
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(stdout.String(), "---\ntype: "), "an OKF concept:\n%s", stdout.String())
	assert.NotContains(t, stdout.String(), "space: DOCS")
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
	result, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		Space:      "DOCS", Title: "Release notes",
		Output: out, AttachmentsDir: filepath.Join(dir, "files"),
	})
	require.NoError(t, err)
	assert.Nil(t, result.Labels, "a page without labels has none")
	assert.Equal(t, []string{"files/chart.png"}, result.DeclaredAttachments,
		"the result names the attachments the headers declare, and not the one the page lacks")

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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: out,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}))

	var uploaded []string
	for _, attachment := range server.Attachments(post.ID) {
		uploaded = append(uploaded, attachment.Filename)
	}
	assert.Contains(t, uploaded, "files_chart.png")
	assert.Contains(t, server.Page(post.ID).Body, `<ri:attachment ri:filename="files_chart.png"/>`)
}

// TestExportLongAttachmentNames: an attachment whose name is longer than a
// file name may be is written under a shorter one, which the document links
// to and declares, and which publishing back uploads.
func TestExportLongAttachmentNames(t *testing.T) {
	long := strings.Repeat("a", 300)
	image := long + "-one.png"
	other := long + "-two.png"
	linked := strings.Repeat("文", 100) + ".pdf"
	raw := strings.Repeat("b", 300) + ".png"

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	page := server.AddPage("DOCS", "Long names", "page", home.ID)
	server.EditPage(page.ID, `<p><ac:image><ri:attachment ri:filename="`+image+`"/></ac:image></p>`+
		`<p><ac:image><ri:attachment ri:filename="`+other+`"/></ac:image></p>`+
		`<p><ac:link><ri:attachment ri:filename="`+linked+`"/><ac:plain-text-link-body><![CDATA[report]]></ac:plain-text-link-body></ac:link></p>`+
		`<table><tbody><tr><td colspan="2"><ac:image><ri:attachment ri:filename="`+raw+`"/></ac:image></td></tr></tbody></table>`)
	for _, name := range []string{image, other, linked, raw} {
		server.AddAttachmentData(page.ID, name, []byte(name))
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "long.md")
	result, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     page.ID, Output: out, AttachmentsDir: filepath.Join(dir, "files"),
	})
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(dir, "files"))
	require.NoError(t, err)
	require.Len(t, entries, 4, "every attachment has a file of its own")

	exported, err := os.ReadFile(out)
	require.NoError(t, err)
	document := string(exported)

	written := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		assert.LessOrEqual(t, len(name), 255, "%q", name)
		assert.True(t, utf8.ValidString(name), "%q", name)
		assert.Contains(t, document, "files/"+name, "the document links to the file written")

		data, err := os.ReadFile(filepath.Join(dir, "files", name))
		require.NoError(t, err)
		written[string(data)] = name
	}
	for _, name := range []string{image, other, linked, raw} {
		assert.Contains(t, written, name, "%q is written", name)
	}

	rawName := written[raw]
	assert.Equal(t, []string{"files/" + rawName}, result.DeclaredAttachments)
	assert.Contains(t, document, "<!-- Attachment: files/"+rawName+" -->")
	assert.Contains(t, document, `ri:filename="files_`+rawName+`"`)

	require.NoError(t, Run(context.Background(), Config{
		Files:      out,
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{AttachReferenced: true},
			Output:    io.Discard,
		},
	}))

	var uploaded []string
	for _, attachment := range server.Attachments(page.ID) {
		uploaded = append(uploaded, attachment.Filename)
	}
	for _, name := range written {
		assert.Contains(t, uploaded, "files_"+name, "published back, the file is uploaded under the name it was written with")
	}
}

// TestExportRefusesToOverwrite: an export that would replace a file fails
// before it writes anything, unless told to replace it.
func TestExportRefusesToOverwrite(t *testing.T) {
	server, id, _ := exportFixture(t)

	dir := t.TempDir()
	out := writeFile(t, dir, "installing.md", "mine")

	config := ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, Output: out,
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		Space:      "DOCS", Title: "Nowhere",
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, NoAttachments: true, Stdout: &stdout,
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
	var result *export.Result
	logged := captureLog(t, func() {
		var err error
		result, err = Export(context.Background(), ExportConfig{
			Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"}, PageID: id,
			NoAttachments: true, Stdout: &stdout,
		})
		require.NoError(t, err)
	})
	assert.Contains(t, stdout.String(), "<!-- Attachment: kept.png -->")
	assert.Equal(t, []string{"kept.png"}, result.DeclaredAttachments)
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
		Connection: ConnectionConfig{BaseURL: server.URL + "/ex/confluence/cloud-id", Password: "token"},
		PageID:     id, Output: out,
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
		Connection: ConnectionConfig{BaseURL: server.URL + "/ex/confluence/cloud-id", Password: "token"},
		PageID:     id, NoAttachments: true, Stdout: io.Discard,
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
				Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
				PageID:     inside.ID, Output: out,
			})
			require.NoError(t, err)

			exported, err := os.ReadFile(out)
			require.NoError(t, err)
			assert.Contains(t, string(exported), "<!-- Space: DOCS -->\n"+test.headers+"<!-- Title: Inside -->\n")

			require.NoError(t, Run(context.Background(), Config{
				PublishConfig: PublishConfig{
					Output: io.Discard,
				},
				Files: out,
				Connection: ConnectionConfig{
					BaseURL:  server.URL,
					Username: "user",
					Password: "token",
				},
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
			Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
			PageID:     inside.ID, Stdout: &stdout,
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"}, PageID: id, Output: out,
	})
	require.NoError(t, err)

	exported, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(exported), "<!-- Attachment: kept.png -->")
	assert.NotContains(t, string(exported), "<!-- Attachment: gone.png -->")

	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Output: io.Discard,
		},
		Files: out,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"}, PageID: id, Output: out,
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"}, PageID: id, Output: out, Overwrite: true,
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
	for _, test := range []struct {
		name, body string
		status     int
		headers    http.Header
		want       error
	}{
		{"not found", `{}`, http.StatusNotFound, nil, nil},
		{"login page", `<html>Log in</html>`, http.StatusOK, http.Header{
			"Content-Type": {"text/html"}, "X-Seraph-LoginReason": {"AUTHENTICATED_FAILED"},
		}, confluence.ErrLoginPage},
		{"empty login page", "", http.StatusOK, http.Header{
			"Content-Type": {"text/html"}, "X-Seraph-LoginReason": {"AUTHENTICATED_FAILED"},
		}, confluence.ErrLoginPage},
		{"an identity provider's page", `<html>Sign in</html>`, http.StatusOK, http.Header{
			"Content-Type": {"text/html; charset=UTF-8"},
		}, confluence.ErrLoginPage},
		// Longer than net/http buffers before it sends a body in chunks, so
		// the answer gives no length and the mismatch shows only at its end.
		{"a body longer than the attachment", strings.Repeat("x", 4096), http.StatusOK, http.Header{
			"Content-Type": {"image/png"},
		}, confluence.ErrAttachmentSizeMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, id := exportPage(t, `<p><ac:image><ri:attachment ri:filename="first.png"/></ac:image>`+
				`<ac:image><ri:attachment ri:filename="second.png"/></ac:image></p>`)
			server.AddAttachmentData(id, "first.png", []byte("first"))
			server.AddAttachmentData(id, "second.png", []byte("second"))
			server.SetFailHeaders(test.headers)
			server.SetFail(func(r *http.Request) (int, string, bool) {
				return test.status, test.body, strings.HasSuffix(r.URL.Path, "/second.png")
			})

			dir := t.TempDir()
			_, err := Export(context.Background(), ExportConfig{
				Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"}, PageID: id,
				Output: filepath.Join(dir, "page.md"),
			})
			if test.want != nil {
				assert.ErrorIs(t, err, test.want)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, 1, server.CountRequests(http.MethodGet, "/first.png"))
			assert.Equal(t, 1, server.CountRequests(http.MethodGet, "/second.png"))

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

// TestExportKeepsTheMaxAppearance: an appearance mark publishes is written
// back, so that publishing the export does not make the page full width.
func TestExportKeepsTheMaxAppearance(t *testing.T) {
	server, id := exportPage(t, `<p>Body.</p>`)
	setContentProperty(t, confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}), id,
		"content-appearance-published", `"max"`)

	var stdout bytes.Buffer
	_, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"}, PageID: id, Stdout: &stdout,
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     pageID, Output: output, Overwrite: overwrite,
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

	t.Run("an attachment of a later page already there", func(t *testing.T) {
		server, pages := exportTreeSpace(t)
		out := filepath.Join(t.TempDir(), "guide")
		stale := filepath.Join(out, "c", "deep-dive", "image.png")
		require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o755))
		require.NoError(t, os.WriteFile(stale, []byte("mine"), 0o600))

		err := exportTree(t, server, pages["Guide"].ID, out, 0, false)
		require.ErrorContains(t, err, "--overwrite")
		assert.NoFileExists(t, filepath.Join(out, "index.md"), "nothing was written")
		assert.NoFileExists(t, filepath.Join(out, "c", "index.md"), "nothing was written")

		require.NoError(t, exportTree(t, server, pages["Guide"].ID, out, 0, true))
		data, err := os.ReadFile(stale)
		require.NoError(t, err)
		assert.Equal(t, "image of Deep dive", string(data))
	})

	t.Run("no directory to write to", func(t *testing.T) {
		server, pages := exportTreeSpace(t)
		require.ErrorContains(t, exportTree(t, server, pages["Guide"].ID, "", 0, false), "--output")
	})
}

// TestExportTreeRoundTrip publishes an exported tree back, its metadata
// written either way: every page has to land on the page it came from, under
// the parent it had, with no new pages and no attachment uploaded under a new
// name.
func TestExportTreeRoundTrip(t *testing.T) {
	for spelling, features := range map[export.Metadata][]string{
		export.MetadataComments:    {"mention"},
		export.MetadataFrontMatter: {"mention", "frontmatter"},
	} {
		t.Run(string(spelling), func(t *testing.T) {
			server, pages := exportTreeSpace(t)
			out := filepath.Join(t.TempDir(), "guide")
			_, err := ExportTree(context.Background(), ExportConfig{
				Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
				PageID:     pages["Guide"].ID, Output: out, Metadata: spelling,
			}, export.TreeOptions{})
			require.NoError(t, err)

			before := map[string]string{}
			for title, p := range pages {
				before[title] = server.Page(p.ID).ParentID
				server.EditPage(p.ID, "<p>edited in Confluence</p>")
			}

			require.NoError(t, Run(context.Background(), Config{
				PublishConfig: PublishConfig{
					Rendering: RenderingConfig{
						AttachReferenced: true,
						Features:         features,
					},
					Output: io.Discard,
				},
				Files: filepath.Join(out, "**", "*.md"),
				Connection: ConnectionConfig{
					BaseURL:  server.URL,
					Username: "user",
					Password: "token",
				},
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
		})
	}
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     pages["Guide"].ID, Output: out,
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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(out, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
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
	tiny, err := confluence.GenerateTinyLink(server.URL, pages["C++"].ID)
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     pages["Guide"].ID, Output: out,
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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(out, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
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
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		Space:      "DOCS", Output: out,
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
			Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
			Space:      "DOCS", Output: out,
		}, export.TreeOptions{Depth: 1})
		require.NoError(t, err)

		var titles []string
		for _, result := range results {
			titles = append(titles, result.Page.Title)
		}
		assert.ElementsMatch(t, []string{"Home", "Stray"}, titles, "the top of the space")
	})

}

// TestExportTitleAndBodyAsValues: a library caller gets the title, the labels
// and the body without the metadata as values, with nothing written to a file
// or a writer, and a label with "=" in it comes back whole.
func TestExportTitleAndBodyAsValues(t *testing.T) {
	server, id, _ := exportFixture(t)
	server.AddLabel(id, "team=docs")

	dir := t.TempDir()
	t.Chdir(dir)

	result, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, Metadata: export.MetadataNone, NoAttachments: true,
	})
	require.NoError(t, err)

	assert.Equal(t, "Installing", result.Title)
	assert.Equal(t, []string{"how-to", "install", "team=docs"}, result.Labels)
	assert.True(t, strings.HasPrefix(result.Markdown, "# Installing\n"), "the body alone:\n%s", result.Markdown)
	assert.NotContains(t, result.Markdown, "<!-- Title:")
	assert.NotContains(t, result.Markdown, "<!-- Space:")
	assert.Contains(t, result.Markdown, "![Logo](logo.png)")
	assert.Empty(t, result.Output)
	assert.Empty(t, result.Attachments)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written")

	// The body is what a writer receives too.
	var stdout bytes.Buffer
	_, err = Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, Metadata: export.MetadataNone, NoAttachments: true, Stdout: &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, result.Markdown, stdout.String())
}

// TestExportResultCarriesTheDocument: with the metadata, Result.Markdown is
// the whole document written, headers included.
func TestExportResultCarriesTheDocument(t *testing.T) {
	server, id, _ := exportFixture(t)
	t.Chdir(t.TempDir())

	var stdout bytes.Buffer
	result, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, NoAttachments: true, Stdout: &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, stdout.String(), result.Markdown)
	assert.Contains(t, result.Markdown, "<!-- Title: Installing -->")
}

// TestExportTreeRefusesNoMetadata: a tree of mark documents is exported to be
// published back, which takes the metadata; OKF writes its own and ignores it.
func TestExportTreeRefusesNoMetadata(t *testing.T) {
	server, id, _ := exportFixture(t)

	config := ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, Metadata: export.MetadataNone, NoAttachments: true,
		Output: filepath.Join(t.TempDir(), "tree"),
	}
	_, err := ExportTree(context.Background(), config, export.TreeOptions{})
	require.ErrorIs(t, err, export.ErrTreeWithoutMetadata)
	_, err = os.Stat(config.Output)
	assert.ErrorIs(t, err, os.ErrNotExist, "refused before anything is written")

	config.Format = export.FormatOKF
	_, err = ExportTree(context.Background(), config, export.TreeOptions{})
	require.NoError(t, err)
}

// TestExportTreeNeedsADirectory: "-" is standard output to a tree as to a
// page, and a tree is written to a directory.
func TestExportTreeNeedsADirectory(t *testing.T) {
	server, id, _ := exportFixture(t)
	dir := t.TempDir()
	t.Chdir(dir)

	for _, output := range []string{"", "-"} {
		_, err := ExportTree(context.Background(), ExportConfig{
			Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
			PageID:     id, Output: output, Stdout: &bytes.Buffer{},
		}, export.TreeOptions{})
		require.ErrorContains(t, err, "writes a directory", "output %q", output)
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no directory named -")
}

// TestExportNoMetadataWithAttachments: the body alone, with its attachments
// written to a directory of their own, refers to them where they were
// written, whether the document goes to a file or to Stdout.
func TestExportNoMetadataWithAttachments(t *testing.T) {
	server, id, published := exportFixture(t)
	connection := ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"}

	logo, err := os.ReadFile(filepath.Join(published, "logo.png"))
	require.NoError(t, err)

	assertWritten := func(t *testing.T, dir string, result *export.Result) {
		t.Helper()
		assert.NotContains(t, result.Markdown, "<!--", "no headers:\n%s", result.Markdown)
		assert.True(t, strings.HasPrefix(result.Markdown, "# Installing\n"), "the body alone:\n%s", result.Markdown)
		assert.Contains(t, result.Markdown, "![Logo](files/logo.png)")
		assert.Contains(t, result.Markdown, "[the report](files/report.pdf)")
		assert.Len(t, result.Attachments, 2)

		written, err := os.ReadFile(filepath.Join(dir, "files", "logo.png"))
		require.NoError(t, err)
		assert.Equal(t, logo, written)
		report, err := os.ReadFile(filepath.Join(dir, "files", "report.pdf"))
		require.NoError(t, err)
		assert.Equal(t, "%PDF-1.4 report", string(report))
	}

	t.Run("output", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "installing.md")
		result, err := Export(context.Background(), ExportConfig{
			Connection: connection, PageID: id, Metadata: export.MetadataNone,
			Output: out, AttachmentsDir: filepath.Join(dir, "files"),
		})
		require.NoError(t, err)
		assertWritten(t, dir, result)

		document, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.Equal(t, result.Markdown, string(document))
	})

	for name, output := range map[string]string{"stdout": "", "stdout as -": "-"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			var stdout bytes.Buffer
			result, err := Export(context.Background(), ExportConfig{
				Connection: connection, PageID: id, Metadata: export.MetadataNone,
				Output: output, Stdout: &stdout, AttachmentsDir: "files",
			})
			require.NoError(t, err)
			assertWritten(t, dir, result)
			assert.Equal(t, result.Markdown, stdout.String())
			assert.Empty(t, result.Output)

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "only the attachments' directory is written")
			assert.Equal(t, "files", entries[0].Name())
		})
	}
}

// TestExportToStdoutNeedsAnAttachmentsDir: with no Output for the attachments
// to go beside, a library caller whose page has attachments to write names
// where they go or does without them, rather than finding them in its working
// directory; nothing is written when it is refused.
func TestExportToStdoutNeedsAnAttachmentsDir(t *testing.T) {
	server, id, _ := exportFixture(t)
	connection := ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"}
	dir := t.TempDir()
	t.Chdir(dir)

	for _, output := range []string{"", "-"} {
		var stdout bytes.Buffer
		_, err := Export(context.Background(), ExportConfig{
			Connection: connection, PageID: id, Output: output, Metadata: export.MetadataNone, Stdout: &stdout,
		})
		require.ErrorIs(t, err, export.ErrNoAttachmentsDir, "output %q", output)
		assert.Contains(t, err.Error(), "AttachmentsDir", "the error names what to set")
		assert.Contains(t, err.Error(), "NoAttachments")
		assert.Empty(t, stdout.String(), "output %q: the document is not written either", output)
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing written to the working directory")

	result, err := Export(context.Background(), ExportConfig{Connection: connection, PageID: id, NoAttachments: true})
	require.NoError(t, err, "NoAttachments writes no attachments")
	assert.Empty(t, result.Attachments)
	entries, err = os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)

	files := filepath.Join(t.TempDir(), "files")
	result, err = Export(context.Background(), ExportConfig{Connection: connection, PageID: id, AttachmentsDir: files})
	require.NoError(t, err, "an AttachmentsDir says where they go")
	assert.NotEmpty(t, result.Attachments)
	for _, written := range result.Attachments {
		assert.FileExists(t, written)
	}

	result, err = Export(context.Background(), ExportConfig{Connection: connection, PageID: id, AttachmentsDir: "."})
	require.NoError(t, err, `"." asks for the working directory`)
	assert.NotEmpty(t, result.Attachments)
	for _, written := range result.Attachments {
		assert.FileExists(t, filepath.Join(dir, filepath.Base(written)))
	}
}

// TestExportToStdoutWithNothingToWrite: a page with no attachments to write,
// whether it has none or refers only to one it does not have, exports to
// Stdout and the Result with no AttachmentsDir and no NoAttachments.
func TestExportToStdoutWithNothingToWrite(t *testing.T) {
	for name, body := range map[string]string{
		"no attachments":       `<p>Text only.</p>`,
		"a missing attachment": `<p>Text only.</p><p><ac:image><ri:attachment ri:filename="gone.png"/></ac:image></p>`,
	} {
		for _, output := range []string{"", "-"} {
			t.Run(fmt.Sprintf("%s, Output %q", name, output), func(t *testing.T) {
				server, id := exportPage(t, body)
				dir := t.TempDir()
				t.Chdir(dir)

				var stdout bytes.Buffer
				result, err := Export(context.Background(), ExportConfig{
					Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
					PageID:     id, Output: output, Metadata: export.MetadataNone, Stdout: &stdout,
				})
				require.NoError(t, err)
				assert.True(t, strings.HasPrefix(result.Markdown, "Text only.\n"), result.Markdown)
				assert.Equal(t, result.Markdown, stdout.String())
				assert.Empty(t, result.Output)
				assert.Empty(t, result.Attachments)

				entries, err := os.ReadDir(dir)
				require.NoError(t, err)
				assert.Empty(t, entries, "nothing written to the working directory")
			})
		}
	}
}

// TestExportPageWithoutStdout: export.Page, called directly with no Stdout,
// leaves the document in the Result.
func TestExportPageWithoutStdout(t *testing.T) {
	server, id := exportPage(t, `<p>Body.</p>`)

	result, err := export.Page(context.Background(),
		confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}),
		export.Config{PageID: id, Metadata: export.MetadataNone, NoAttachments: true})
	require.NoError(t, err)
	assert.Equal(t, "Body.\n", result.Markdown)
}

// TestExportBodyOpeningWithARule: with no metadata above it, a body that
// opens with a thematic break does not open with "---", which tools that read
// front matter take for its start.
func TestExportBodyOpeningWithARule(t *testing.T) {
	server, id := exportPage(t, `<hr/><p>Intro</p><hr/><p>More</p>`)

	result, err := Export(context.Background(), ExportConfig{
		Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     id, Metadata: export.MetadataNone, NoAttachments: true,
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(result.Markdown, "***\n"), result.Markdown)
	assert.Contains(t, result.Markdown, "\n---\n", "only the opening rule is spelled otherwise")
}

// TestExportTreeKeepsClearOfGit: an export usually goes into a git working
// tree, so neither an attachment nor a page is written under a name git reads
// as its own there, the documents link to the names written, and exporting
// again writes the same files.
func TestExportTreeKeepsClearOfGit(t *testing.T) {
	for _, format := range []export.Format{export.FormatMark, export.FormatOKF} {
		t.Run(string(format), func(t *testing.T) {
			server := confluencetest.New(t)
			server.AddSpace("DOCS")
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			guide := server.AddPage("DOCS", "Guide", "page", home.ID)
			dotGit := server.AddPage("DOCS", ".git", "page", guide.ID)
			inner := server.AddPage("DOCS", "Inner", "page", dotGit.ID)

			attach := func(id string, names ...string) {
				body := ""
				for _, name := range names {
					body += `<p><ac:link><ri:attachment ri:filename="` + name + `"/></ac:link></p>`
					server.AddAttachmentData(id, name, []byte("outside"))
				}
				server.EditPage(id, body)
			}
			attach(guide.ID, ".git", ".gitattributes")
			attach(dotGit.ID, ".GIT", ".gitmodules")
			attach(inner.ID, ".gitignore")

			out := filepath.Join(t.TempDir(), "guide")
			files := func() map[string]string {
				t.Helper()
				found := map[string]string{}
				require.NoError(t, filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					switch strings.ToLower(d.Name()) {
					case ".git", ".gitattributes", ".gitmodules", ".gitignore":
						t.Errorf("%s is a name git keeps for itself", path)
					}
					if !d.IsDir() {
						data, err := os.ReadFile(path)
						require.NoError(t, err)
						rel, err := filepath.Rel(out, path)
						require.NoError(t, err)
						found[filepath.ToSlash(rel)] = string(data)
					}
					return nil
				}))
				return found
			}

			run := func(overwrite bool) {
				t.Helper()
				_, err := ExportTree(context.Background(), ExportConfig{
					Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
					PageID:     guide.ID, Output: out, Format: format, Overwrite: overwrite,
				}, export.TreeOptions{})
				require.NoError(t, err)
			}

			run(false)
			first := files()

			var guideDoc, gitDoc, innerDoc, guideFiles, gitFiles, innerFiles string
			if format == export.FormatOKF {
				guideDoc, gitDoc, innerDoc = "guide.md", "guide/git.md", "guide/git/inner.md"
				guideFiles = "references/attachments/" + guide.ID + "/"
				gitFiles = "references/attachments/" + dotGit.ID + "/"
				innerFiles = "references/attachments/" + inner.ID + "/"
			} else {
				guideDoc, gitDoc, innerDoc = "index.md", "git/index.md", "git/inner/index.md"
				guideFiles, gitFiles, innerFiles = "", "git/", "git/inner/"
			}
			for doc, attachments := range map[string][]string{
				guideDoc: {guideFiles + ".git-2", guideFiles + ".gitattributes-2"},
				gitDoc:   {gitFiles + ".GIT-2", gitFiles + ".gitmodules-2"},
				innerDoc: {innerFiles + ".gitignore-2"},
			} {
				require.Contains(t, first, doc)
				for _, attachment := range attachments {
					assert.Equal(t, "outside", first[attachment], attachment)
					link, err := filepath.Rel(filepath.Dir(filepath.FromSlash(doc)), filepath.FromSlash(attachment))
					require.NoError(t, err)
					assert.Contains(t, first[doc], "("+filepath.ToSlash(link)+")", "%s links to %s", doc, attachment)
				}
			}

			run(true)
			assert.Equal(t, first, files(), "exporting again writes the same files")
		})
	}
}
