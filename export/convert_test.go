package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	markdown "github.com/kovetskiy/mark/v16/markdown"
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

// A fence of "sh" publishes as "bash", so a page holding "sh" -- one mark
// published before it renamed aliases -- can only come back as the macro.
func TestConvertCodeLanguageAliasStaysAMacro(t *testing.T) {
	for _, language := range []string{"sh", "Python"} {
		storage := `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">` + language +
			`</ac:parameter><ac:plain-text-body><![CDATA[x]]></ac:plain-text-body></ac:structured-macro>`
		md, published := republish(t, storage)
		assert.True(t, strings.HasPrefix(md, `<ac:structured-macro ac:name="code">`), md)
		assert.Equal(t, canonical(t, storage), published)
	}
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
		{`<ac:parameter ac:name="language">bash</ac:parameter><ac:parameter ac:name="linenumbers">true</ac:parameter>`, "```bash linenumbers\n"},
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
		// The context path is found as --target-url finds it, any *.action
		// included.
		{"https://example.com/confluence/plugins/viewsource/viewpagesrc.action?pageId=77",
			PageRef{BaseURL: "https://example.com/confluence", PageID: "77"}},
		// A short link carries the page id, encoded: see page.GenerateTinyLink.
		{"https://example.atlassian.net/wiki/x/AfIFKgE",
			PageRef{BaseURL: "https://example.atlassian.net/wiki", PageID: "5000000001"}},
		{"https://confluence.example.com/x/D4AB/",
			PageRef{BaseURL: "https://confluence.example.com", PageID: "98319"}},
	} {
		got, err := ParsePageURL(test.url)
		require.NoError(t, err, test.url)
		assert.Equal(t, test.want, got, test.url)
	}

	for _, bad := range []string{"not a url", "/wiki/spaces/DOCS/pages/1", "https://example.atlassian.net/wiki/x/A", "https://example.atlassian.net/wiki/x/a.b", "https://example.com/"} {
		_, err := ParsePageURL(bad)
		assert.Error(t, err, bad)
	}
}

// republish exports storage and publishes the Markdown that comes out,
// returning both, the storage format in canonical form.
func republish(t *testing.T, storage string) (string, string) {
	t.Helper()

	md := convert(t, storage)

	return md, canonical(t, compile(t, md, filepath.Join(t.TempDir(), "page.md")))
}

// TestConvertListNestedDirectlyInAList: older editors and pasted HTML put a
// sublist straight into the list rather than into the item it belongs to.
// It is kept, under the item before it.
func TestConvertListNestedDirectlyInAList(t *testing.T) {
	for _, test := range []struct {
		name, storage, markdown, published string
	}{
		{
			"bullets",
			`<ul><li>a</li><ul><li>nested</li></ul><li>b</li></ul>`,
			"- a\n  - nested\n- b\n",
			`<ul><li>a<ul><li>nested</li></ul></li><li>b</li></ul>`,
		},
		{
			"numbers",
			`<ol><li>a</li><ol><li>nested</li></ol><li>b</li></ol>`,
			"1. a\n   1. nested\n2. b\n",
			`<ol><li>a<ol><li>nested</li></ol></li><li>b</li></ol>`,
		},
		{
			"before the first item",
			`<ul><ul><li>nested</li></ul><li>b</li></ul>`,
			"- - nested\n- b\n",
			`<ul><li><ul><li>nested</li></ul></li><li>b</li></ul>`,
		},
		{
			"tasks",
			`<ac:task-list><ac:task><ac:task-id>1</ac:task-id><ac:task-status>incomplete</ac:task-status><ac:task-body>a</ac:task-body></ac:task>` +
				`<ac:task-list><ac:task><ac:task-id>2</ac:task-id><ac:task-status>complete</ac:task-status><ac:task-body>nested</ac:task-body></ac:task></ac:task-list>` +
				`</ac:task-list>`,
			"- [ ] a\n  - [x] nested\n",
			nestedTask,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			md, published := republish(t, test.storage)
			assert.Equal(t, test.markdown, md)
			assert.Equal(t, canonical(t, test.published), published)
		})
	}
}

// TestConvertTaskListInATask: what a task holds past its first line is
// indented under the box, where Markdown reads it as the task's.
func TestConvertTaskListInATask(t *testing.T) {
	md, published := republish(t, nestedTask)
	assert.Equal(t, "- [ ] a\n  - [x] nested\n", md)
	assert.Equal(t, canonical(t, nestedTask), published)
}

// nestedTask is a task list in a task, as mark publishes it -- the line
// breaks around the inner list are the text of the outer task's body.
const nestedTask = `<ac:task-list><ac:task><ac:task-id>1</ac:task-id><ac:task-status>incomplete</ac:task-status><ac:task-body>a
<ac:task-list><ac:task><ac:task-id>2</ac:task-id><ac:task-status>complete</ac:task-status><ac:task-body>nested</ac:task-body></ac:task></ac:task-list>
</ac:task-body></ac:task></ac:task-list>`

// TestConvertNestedListNumberedFromElsewhere: in a tight item a nested list
// sits on the line after the item's text, but a numbered one that does not
// start at 1 cannot interrupt a paragraph there, and was published as more
// of the item's text. It is set off by a blank line instead.
func TestConvertNestedListNumberedFromElsewhere(t *testing.T) {
	storage := `<ul><li>a<ol start="3"><li>c</li><li>d</li></ol></li><li>b</li></ul>`

	md, published := republish(t, storage)
	assert.Equal(t, "- a\n\n  3. c\n  4. d\n- b\n", md)
	assert.Equal(t, canonical(t, `<ul><li><p>a</p><ol start="3"><li>c</li><li>d</li></ol></li><li><p>b</p></li></ul>`), published)

	// Numbered from 1, it still sits right under the text.
	md, published = republish(t, `<ul><li>a<ol><li>c</li></ol></li></ul>`)
	assert.Equal(t, "- a\n  1. c\n", md)
	assert.Equal(t, canonical(t, `<ul><li>a<ol><li>c</li></ol></li></ul>`), published)
}

// TestConvertParagraphLikeATable: a line of a paragraph that reads as a
// table's delimiter row turns the paragraph into a table, so it is escaped.
func TestConvertParagraphLikeATable(t *testing.T) {
	for _, storage := range []string{
		"<p>a | b<br/>\n--- | ---</p>",
		"<p>a | b<br/>\n|:--|--:|</p>",
		"<p>a<br/>\n:-</p>",
		"<p>a | b\n--- | ---</p>",
	} {
		t.Run(storage, func(t *testing.T) {
			md, published := republish(t, storage)
			assert.Equal(t, canonical(t, storage), published, "exported Markdown:\n%s", md)
		})
	}
}

// TestConvertCodeTitleRoundTrips: a code block's title has to come back
// from the info string exactly. renderer/fencedcodeblock.go drops a "{...}"
// at the end of it as an attribute block and reads a "title=" in it as the
// title, so such a title is quoted.
func TestConvertCodeTitleRoundTrips(t *testing.T) {
	for _, test := range []struct {
		title, language, info string
	}{
		{"A title", "go", "go title A title"},
		{"A title", "", "- title A title"},
		{"title linenumbers", "", "- title title linenumbers"},
		{"title linenumbers", "bash", "bash title title linenumbers"},
		{"Config {prod}", "yaml", `yaml title="Config {prod}"`},
		{"Config {prod}", "", `- title="Config {prod}"`},
		{"a title=b", "go", `go title="a title=b"`},
		{"= b", "go", `go title="= b"`},
		{`say "hi" {x}`, "go", `go title='say "hi" {x}'`},
		{"{x} in front", "go", "go title {x} in front"},
		{"two  spaces", "go", "go title two  spaces"},
	} {
		t.Run(test.title, func(t *testing.T) {
			params := `<ac:parameter ac:name="title">` + escapeXMLText(test.title) + `</ac:parameter>`
			if test.language != "" {
				params = `<ac:parameter ac:name="language">` + test.language + `</ac:parameter>` + params
			}

			md, published := republish(t, `<ac:structured-macro ac:name="code">`+params+
				`<ac:plain-text-body><![CDATA[x]]></ac:plain-text-body></ac:structured-macro>`)
			assert.Equal(t, "```"+test.info+"\nx\n```\n", md)
			assert.Contains(t, published, `<ac:parameter ac:name="title">`+escapeXMLText(test.title)+`</ac:parameter>`)
		})
	}

	// With both quotes and a brace at the end, it cannot be written in the
	// info string, and the macro is kept as it is.
	storage := `<ac:structured-macro ac:name="code"><ac:parameter ac:name="title">it's "a" {b}</ac:parameter>` +
		`<ac:plain-text-body><![CDATA[x]]></ac:plain-text-body></ac:structured-macro>`
	md, published := republish(t, storage)
	assert.NotContains(t, md, "```")
	assert.Equal(t, canonical(t, storage), published)
}

// TestConvertAdjacentInlineRuns: the closing and opening delimiters of two
// runs side by side would read as one run with the delimiters as text.
func TestConvertAdjacentInlineRuns(t *testing.T) {
	for _, storage := range []string{
		`<p><strong>a</strong><strong>b</strong></p>`,
		`<p><em>a</em><em>b</em></p>`,
		`<p><em>a</em><strong>b</strong></p>`,
		`<p><s>a</s><s>b</s></p>`,
		`<p><code>a</code><code>b</code></p>`,
	} {
		_, published := republish(t, storage)
		assert.Equal(t, canonical(t, strings.NewReplacer("<s>", "<del>", "</s>", "</del>").Replace(storage)), published, storage)
	}
}

// TestConvertControlCharacters: encoding/xml refuses them, raw or as a
// reference, and so would Confluence; the page is exported with U+FFFD.
func TestConvertControlCharacters(t *testing.T) {
	assert.Equal(t, "a\ufffd\\[0mb c\ufffd\n", convert(t, "<p>a&#27;[0mb c\x1b</p>"))
}

// TestConvertNamespacedNames are written back with the prefix they had, not
// the namespace it stands for.
func TestConvertNamespacedNames(t *testing.T) {
	for _, storage := range []string{
		`<p xml:lang="en">x</p>`,
		`<div xmlns="http://www.w3.org/1999/xhtml"><p>x</p></div>`,
	} {
		md := convert(t, storage)
		assert.NotContains(t, md, "http://www.w3.org/XML/1998/namespace:", storage)
		assert.NotContains(t, md, "xhtml:p", storage)
		_, published := republish(t, storage)
		assert.Equal(t, canonical(t, storage), published, storage)
	}
}

// TestConvertKeepsNonBreakingSpaces: Markdown keeps one at either end of a
// paragraph, so it is written where a reader can see it; a paragraph holding
// nothing else is a spacer the page's layout relies on.
func TestConvertKeepsNonBreakingSpaces(t *testing.T) {
	md, published := republish(t, "<p>&nbsp;</p><p>after</p>")
	assert.Equal(t, "&nbsp;\n\nafter\n", md)
	assert.Equal(t, canonical(t, "<p> </p><p>after</p>"), published)

	md, published = republish(t, "<p>&nbsp;lead and trail&nbsp;</p>")
	assert.Equal(t, "&nbsp;lead and trail&nbsp;\n", md)
	assert.Equal(t, canonical(t, "<p> lead and trail </p>"), published)
}

// TestConvertKeepsWhatMarkdownCannotSay: an aligned heading, a highlighted
// cell, a <pre> -- written as Markdown, each would lose what it had.
func TestConvertKeepsWhatMarkdownCannotSay(t *testing.T) {
	for _, storage := range []string{
		`<h2 style="text-align: center;">Title</h2>`,
		`<table><tbody><tr><th>a</th></tr><tr><td data-highlight-colour="#e3fcef">b</td></tr></tbody></table>`,
		`<table><tbody><tr><th class="highlight-green">a</th></tr><tr><td>b</td></tr></tbody></table>`,
		`<table><tbody><tr><th>a</th></tr><tr><td style="background-color: red;">b</td></tr></tbody></table>`,
		`<table><tbody><tr><th style="text-align:center">a</th></tr><tr><td>b</td></tr></tbody></table>`,
		`<pre>a <b>b</b></pre>`,
	} {
		_, published := republish(t, storage)
		assert.Equal(t, canonical(t, storage), published, storage)
	}

	// mark's own aligned table is still a GFM table.
	assert.Equal(t, "| a |\n| :---: |\n| b |\n", convert(t,
		`<table><tbody><tr><th style="text-align:center">a</th></tr><tr><td style="text-align:center">b</td></tr></tbody></table>`))
}

// TestConvertColumnWidths: a <colgroup> of pixel widths is a Column-Widths
// comment, and one that comment cannot say keeps the table as it was.
func TestConvertColumnWidths(t *testing.T) {
	const rows = `<thead><tr><th>a</th><th>b</th></tr></thead><tbody><tr><td>c</td><td>d</td></tr></tbody></table>`

	md, published := republish(t, `<table><colgroup><col style="width: 226.0px;"/><col style="width: 80px"/></colgroup>`+rows)
	assert.Equal(t, "<!-- Column-Widths: 226,80 -->\n| a | b |\n| --- | --- |\n| c | d |\n", md)
	assert.Equal(t, canonical(t, `<table><colgroup><col style="width: 226px;"/><col style="width: 80px;"/></colgroup>`+rows), published)

	assert.Equal(t, "| a | b |\n| --- | --- |\n| c | d |\n", convert(t, `<table><colgroup><col/><col/></colgroup>`+rows),
		"a colgroup without widths says nothing")

	for _, storage := range []string{
		`<table><colgroup><col style="width: 50%;"/><col style="width: 50%;"/></colgroup>` + rows,
		`<table><colgroup><col style="width: 10.5px;"/><col style="width: 80px;"/></colgroup>` + rows,
		`<table><colgroup><col style="width: 80px;"/><col/></colgroup>` + rows,
		`<table><colgroup><col style="width: 80px;"/></colgroup>` + rows,
		`<table><colgroup><col span="2" style="width: 80px;"/></colgroup>` + rows,
	} {
		_, published := republish(t, storage)
		assert.Equal(t, canonical(t, storage), published, storage)
	}
}

// TestConvertCodeTitleReadsBack: the info string is the one publishing reads
// back as the same title, quoted where mark's own form would be misread.
func TestConvertCodeTitleReadsBack(t *testing.T) {
	for _, title := range []string{"Plain", "a title=b", "ends {in braces}", `quote " and title=x`} {
		storage := `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter>` +
			`<ac:parameter ac:name="title">` + escapeXMLText(title) + `</ac:parameter>` +
			`<ac:plain-text-body><![CDATA[x]]></ac:plain-text-body></ac:structured-macro>`
		md, published := republish(t, storage)
		assert.True(t, strings.HasPrefix(md, "```go "), md)
		assert.Contains(t, published, `<ac:parameter ac:name="title">`+escapeXMLText(title)+`</ac:parameter>`, md)
	}
}

// TestConvertEscapesThroughStdlib: what is written as storage format has the
// characters XML cannot carry replaced, in attributes and CDATA too.
func TestConvertEscapesThroughStdlib(t *testing.T) {
	md := convert(t, `<p><a href="x" data-x="&quot;a&quot;&#10;b" data-y="c&#27;">t</a></p>`)
	assert.Contains(t, md, `data-x="&#34;a&#34;&#10;b"`)
	assert.Contains(t, md, "data-y=\"c\ufffd\"")
	assert.Equal(t, "<![CDATA[a]]><![CDATA[]]]]><![CDATA[>b]]>", cdata("a]]>b"))
}

// A URL pasted into a page is rewritten only when it opens a page of the
// instance being exported to be read, and says nothing a relative link would
// drop.
func TestPageOnInstance(t *testing.T) {
	const base = "https://example.atlassian.net/wiki"

	for _, test := range []struct {
		name, href, base string
		want             PageRef
	}{
		{"page", base + "/spaces/DOCS/pages/123/Title", base, PageRef{BaseURL: base, PageID: "123", Space: "DOCS"}},
		{"viewpage.action", base + "/pages/viewpage.action?pageId=123", base, PageRef{BaseURL: base, PageID: "123"}},
		{"short link", base + "/x/ew", base, PageRef{BaseURL: base, PageID: "123"}},
		{"host in another case", "https://Example.atlassian.net/wiki/display/DOCS/Title", base,
			PageRef{BaseURL: "https://Example.atlassian.net/wiki", Space: "DOCS", Title: "Title"}},
		{"base without /wiki", base + "/spaces/DOCS/pages/123/Title", "https://example.atlassian.net",
			PageRef{BaseURL: base, PageID: "123", Space: "DOCS"}},
		{"default port", "https://example.atlassian.net:443/wiki/x/ew", base,
			PageRef{BaseURL: "https://example.atlassian.net:443/wiki", PageID: "123"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := pageOnInstance(test.href, test.base)
			require.True(t, ok)
			assert.Equal(t, test.want, got)
		})
	}

	for _, test := range []struct{ name, href string }{
		{"focused comment", base + "/spaces/DOCS/pages/123/Title?focusedCommentId=9"},
		{"old version", base + "/pages/viewpage.action?pageId=123&pageVersion=2"},
		{"anchor", base + "/spaces/DOCS/pages/123/Title#Section"},
		{"another host", "https://other.atlassian.net/wiki/spaces/DOCS/pages/123/Title"},
		{"another context path", "https://example.atlassian.net/confluence/spaces/DOCS/pages/123/Title"},
		{"dated blog post", base + "/display/DOCS/2024/01/31/Post"},
		{"editpage.action", base + "/pages/editpage.action?pageId=123"},
		{"short link too long", base + "/x/AQAAAAAAAAAB"},
		{"pageId beside a path that names another page", base + "/spaces/DOCS/pages/123/Title?pageId=456"},
		{"pageId on a short link", base + "/x/ew?pageId=999"},
		{"context path in another case", "https://example.atlassian.net/WIKI/x/ew"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, ok := pageOnInstance(test.href, base)
			assert.False(t, ok)
		})
	}
}

// A "!" just before a link would make it an image.
func TestConvertEscapesABangBeforeALink(t *testing.T) {
	doc, err := Convert(`<p>Wow!<a href="https://example.com/">site</a></p>`, Options{})
	require.NoError(t, err)
	assert.Equal(t, "Wow\\![site](https://example.com/)\n", doc.Markdown)
}

// TestConvertEscapesOnlyWhatFeaturesParse: the math, emoji and date escapes
// leave alone text those features would not parse.
func TestConvertEscapesOnlyWhatFeaturesParse(t *testing.T) {
	for storage, want := range map[string]string{
		"<p>costs $5 and $ 6</p>":              `costs \$5 and $ 6`,
		"<p>at 10:30:45, Note: :nosuch:</p>":   "at 10:30:45, Note: :nosuch:",
		"<p>:smile: and :+1:</p>":              `\:smile: and \:+1:`,
		"<p>@date(2024-01-01) and @date()</p>": `\@date(2024-01-01) and @date()`,
	} {
		assert.Equal(t, want+"\n", convert(t, storage), storage)
	}
}
