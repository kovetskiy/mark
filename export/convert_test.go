package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	markdown "github.com/kovetskiy/mark/v17/markdown"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureDocument is what a fixture's .md holds: the headers Convert's result
// asks for, then the body.
func fixtureDocument(doc *Document) string {
	var headers []string
	if doc.Sidebar != "" {
		headers = append(headers, "<!-- Sidebar: "+doc.Sidebar+" -->")
	} else if doc.Layout != "" {
		headers = append(headers, "<!-- Layout: "+doc.Layout+" -->")
	}
	for _, path := range doc.Declared {
		headers = append(headers, "<!-- Attachment: "+path+" -->")
	}

	if len(headers) == 0 {
		return doc.Markdown
	}

	return strings.Join(headers, "\n") + "\n\n" + doc.Markdown
}

// TestConvertFixtures converts the storage format in testdata/export/*.xml,
// written the way Confluence's own editor writes it, and compares the
// Markdown with the .md beside it. What comes out has to publish, too: mark
// must accept it and produce well-formed storage format from it.
func TestConvertFixtures(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("..", "testdata", "export", "*.xml"))
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".xml")

		t.Run(name, func(t *testing.T) {
			storage, err := os.ReadFile(fixture)
			require.NoError(t, err)

			want, err := os.ReadFile(strings.TrimSuffix(fixture, ".xml") + ".md")
			require.NoError(t, err)

			doc, err := Convert(string(storage), Options{Space: "DOCS"})
			require.NoError(t, err)

			assert.Equal(t, string(want), fixtureDocument(doc))

			published := compile(t, doc.Markdown, filepath.Join(t.TempDir(), name+".md"))
			assert.NoError(t, markdown.CheckWellFormed(published), published)
		})
	}
}

func convert(t *testing.T, storage string) string {
	t.Helper()

	doc, err := Convert(storage, Options{Space: "DOCS"})
	require.NoError(t, err)

	return doc.Markdown
}

func TestConvertAdjacentListsStaySeparate(t *testing.T) {
	assert.Equal(t, "- a\n\n* b\n\n- c\n",
		convert(t, "<ul><li>a</li></ul><ul><li>b</li></ul><ul><li>c</li></ul>"))
	assert.Equal(t, "1. a\n\n1) b\n",
		convert(t, "<ol><li>a</li></ol><ol><li>b</li></ol>"))
}

func TestConvertTableCellWithAPipeInMarkup(t *testing.T) {
	// A pipe in text is escaped; one in the attribute of markup kept as it
	// was cannot be, and would split the cell.
	assert.Equal(t, "| a \\| b |\n| --- |\n| c |\n", convert(t, "<table><tr><th>a | b</th></tr><tr><td>c</td></tr></table>"))

	storage := `<table><tbody><tr><th>a</th></tr><tr><td><ac:emoticon ac:name="x" ac:emoji-fallback="|"/></td></tr></tbody></table>`
	assert.Equal(t, storage+"\n", convert(t, storage))
}

func TestConvertAlertNeedsItsOwnTitle(t *testing.T) {
	alert := `<ac:structured-macro ac:name="note"><ac:parameter ac:name="icon">true</ac:parameter>` +
		`<ac:parameter ac:name="title">Warning</ac:parameter><ac:rich-text-body><p>Hot.</p></ac:rich-text-body></ac:structured-macro>`
	assert.Equal(t, "> [!WARNING]\n>\n> Hot.\n", convert(t, alert))

	// In a list item, mark would publish a plain blockquote.
	assert.Equal(t,
		"- item\n\n  <ac:structured-macro ac:name=\"note\"><ac:parameter ac:name=\"icon\">true</ac:parameter>"+
			"<ac:parameter ac:name=\"title\">Warning</ac:parameter><ac:rich-text-body>\n\n  Hot.\n\n"+
			"  </ac:rich-text-body></ac:structured-macro>\n",
		convert(t, "<ul><li>item"+alert+"</li></ul>"))

	custom := `<ac:structured-macro ac:name="note"><ac:parameter ac:name="title">Careful</ac:parameter>` +
		`<ac:rich-text-body><p>Hot.</p></ac:rich-text-body></ac:structured-macro>`
	assert.Equal(t, `<ac:structured-macro ac:name="note"><ac:parameter ac:name="title">Careful</ac:parameter>`+
		"<ac:rich-text-body>\n\nHot.\n\n</ac:rich-text-body></ac:structured-macro>\n", convert(t, custom))
}

func TestConvertDiagramCodeStaysAMacro(t *testing.T) {
	// A mermaid fence would be drawn on publish, not shown as code.
	storage := `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">mermaid</ac:parameter>` +
		`<ac:plain-text-body><![CDATA[graph TD;
    A-->B;]]></ac:plain-text-body></ac:structured-macro>`
	assert.Equal(t, `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">mermaid</ac:parameter>`+
		`<ac:plain-text-body><![CDATA[graph TD;]]>&#10;<![CDATA[    A-->B;]]></ac:plain-text-body></ac:structured-macro>`+"\n",
		convert(t, storage))
}

func TestConvertCodeInfoString(t *testing.T) {
	for _, test := range []struct {
		params string
		want   string
	}{
		{``, "```\n"},
		{`<ac:parameter ac:name="language">go</ac:parameter>`, "```go\n"},
		{`<ac:parameter ac:name="collapse">true</ac:parameter>`, "```- collapse\n"},
		{`<ac:parameter ac:name="language">c#</ac:parameter><ac:parameter ac:name="title">A title</ac:parameter>`, "```c# title A title\n"},
		{`<ac:parameter ac:name="linenumbers">true</ac:parameter><ac:parameter ac:name="firstline">5</ac:parameter>`, "```- 5\n"},
		{`<ac:parameter ac:name="language">sh</ac:parameter><ac:parameter ac:name="linenumbers">true</ac:parameter>`, "```sh linenumbers\n"},
	} {
		got := convert(t, `<ac:structured-macro ac:name="code">`+test.params+
			`<ac:plain-text-body><![CDATA[x]]></ac:plain-text-body></ac:structured-macro>`)
		assert.Equal(t, test.want+"x\n```\n", got, test.params)
	}
}

func TestConvertHeadingAnchors(t *testing.T) {
	anchor := func(name string) string {
		return `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">` + name + `</ac:parameter></ac:structured-macro>`
	}
	link := func(name string) string {
		return `<p><ac:link ac:anchor="` + name + `"><ac:link-body>go</ac:link-body></ac:link></p>`
	}

	// mark puts the anchor back for the link that points at it.
	assert.Equal(t, "## Set up\n\n[go](#Set-up)\n", convert(t, "<h2>"+anchor("Set-up")+"Set up</h2>"+link("Set-up")))
	// Named otherwise, it is a custom id.
	assert.Equal(t, "## Set up {#install}\n\n[go](#install)\n", convert(t, "<h2>"+anchor("install")+"Set up</h2>"+link("install")))
	// Nothing on the page points at it, so mark would not put it back.
	assert.Equal(t, "## "+anchor("install")+"Set up\n", convert(t, "<h2>"+anchor("install")+"Set up</h2>"))
}

func TestConvertKeepsLinkTextMarkWouldMangle(t *testing.T) {
	// mark publishes the characters of a page link's text as written, so text
	// that needs escaping cannot be written as one.
	storage := `<p><ac:link><ri:page ri:content-title="Page"/><ac:plain-text-link-body><![CDATA[a *b*]]></ac:plain-text-link-body></ac:link></p>`
	assert.Equal(t, storage+"\n", convert(t, storage))

	assert.Equal(t, "[a [b]](ac:Page)\n", convert(t,
		`<p><ac:link><ri:page ri:content-title="Page"/><ac:plain-text-link-body><![CDATA[a [b]]]></ac:plain-text-link-body></ac:link></p>`))
	assert.Equal(t, "[Page](ac:Page)\n", convert(t, `<p><ac:link><ri:page ri:content-title="Page"/></ac:link></p>`))

	// A title whose last "#" part looks like an anchor would be split there.
	anchored := `<p><ac:link><ri:page ri:content-title="Notes#2024"/></ac:link></p>`
	assert.Equal(t, anchored+"\n", convert(t, anchored))
}

func TestConvertRecordsAttachments(t *testing.T) {
	doc, err := Convert(`<p><ac:image><ri:attachment ri:filename="a.png"/></ac:image>`+
		`<ac:structured-macro ac:name="view-file"><ac:parameter ac:name="name"><ri:attachment ri:filename="b.pdf"/></ac:parameter></ac:structured-macro>`+
		`<ac:image><ri:attachment ri:filename="c.png"><ri:page ri:content-title="Other"/></ri:attachment></ac:image></p>`,
		Options{AttachmentPath: func(name string) string { return "files/" + name }})
	require.NoError(t, err)

	assert.Equal(t, []string{"a.png", "b.pdf"}, doc.Attachments,
		"an attachment of another page is not this page's to download")
	assert.Equal(t, []string{"files/b.pdf"}, doc.Declared, "only what mark cannot see needs declaring")
	assert.Contains(t, doc.Markdown, "![](files/a.png)")
	assert.Contains(t, doc.Markdown, `<ri:attachment ri:filename="files_b.pdf"/>`,
		"storage format names the attachment the way publishing the file will")
}

func TestParsePageURL(t *testing.T) {
	for _, test := range []struct {
		url  string
		want PageRef
	}{
		{"https://example.atlassian.net/wiki/spaces/DOCS/pages/123456/Some+Title",
			PageRef{BaseURL: "https://example.atlassian.net/wiki", PageID: "123456", Space: "DOCS"}},
		{"https://example.atlassian.net/wiki/spaces/DOCS/pages/edit-v2/123456",
			PageRef{BaseURL: "https://example.atlassian.net/wiki", PageID: "123456", Space: "DOCS"}},
		{"https://example.atlassian.net/wiki/spaces/~me/blog/2024/01/31/42/Notes",
			PageRef{BaseURL: "https://example.atlassian.net/wiki", PageID: "42", Space: "~me"}},
		{"https://confluence.example.com/pages/viewpage.action?pageId=77",
			PageRef{BaseURL: "https://confluence.example.com", PageID: "77"}},
		{"https://example.com/confluence/pages/viewpage.action?pageId=77",
			PageRef{BaseURL: "https://example.com/confluence", PageID: "77"}},
		{"https://confluence.example.com/display/DOCS/Some+Title%3F",
			PageRef{BaseURL: "https://confluence.example.com", Space: "DOCS", Title: "Some Title?"}},
		{"https://confluence.example.com/display/DOCS/2024/01/31/Notes",
			PageRef{BaseURL: "https://confluence.example.com", Space: "DOCS", Title: "Notes"}},
	} {
		got, err := ParsePageURL(test.url)
		require.NoError(t, err, test.url)
		assert.Equal(t, test.want, got, test.url)
	}

	for _, bad := range []string{"not a url", "/wiki/spaces/DOCS/pages/1", "https://example.atlassian.net/wiki/x/AbCd", "https://example.com/"} {
		_, err := ParsePageURL(bad)
		assert.Error(t, err, bad)
	}
}
