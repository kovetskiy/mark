package export

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTripExcluded are the publish fixtures whose storage format does not
// come back from Markdown the same way, and why.
var roundTripExcluded = map[string]string{
	"html-img-block": "an image standing in an expand macro's body with no paragraph around it comes " +
		"back inside one: Markdown puts an image that stands alone in a paragraph",
}

// TestRoundTripFixtures exports the storage format of each of mark's own
// publish fixtures and publishes the Markdown that comes out: what mark
// publishes has to be what it exported.
func TestRoundTripFixtures(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("..", "testdata", "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".md")

		t.Run(name, func(t *testing.T) {
			if reason, excluded := roundTripExcluded[name]; excluded {
				t.Skip(reason)
			}

			storage, err := os.ReadFile(filepath.Join("..", "testdata", name+".html"))
			require.NoError(t, err)

			doc, err := Convert(string(storage), Options{})
			require.NoError(t, err)

			published := compile(t, doc.Markdown, fixture)

			assert.Equal(t, canonical(t, string(storage)), canonical(t, published),
				"exported Markdown:\n%s", doc.Markdown)
		})
	}
}

// TestRoundTripTextFeaturesWouldParse: text that the math, emoji and date
// features would publish as a formula, an emoji or a date comes back as the
// text it was.
func TestRoundTripTextFeaturesWouldParse(t *testing.T) {
	for _, text := range []string{
		"costs $5,$6 today", "$x$", "$$a$$", "a $b$ c", "price: $", "$ alone", `\(x\) and \[y\]`,
		":smile:", "at 10:30:45", ":a:smile:", "a:smile:b", "Note: this", "::",
		"@date(2024-01-01)", "on @date( x ) and @date()", "user@example",
	} {
		t.Run(text, func(t *testing.T) {
			for _, storage := range []string{
				"<p>" + escapeXMLText(text) + "</p>",
				"<p><strong>" + escapeXMLText(text) + "</strong> and <em>" + escapeXMLText(text) + "</em></p>",
				"<table><thead><tr><th>h</th></tr></thead><tbody><tr><td>" + escapeXMLText(text) + "</td></tr></tbody></table>",
				`<p><a href="https://example.com">` + escapeXMLText(text) + "</a></p>",
			} {
				doc, err := Convert(storage, Options{})
				require.NoError(t, err)

				published := compileWith(t, doc.Markdown, filepath.Join(t.TempDir(), "page.md"),
					"mention", "math", "emoji", "date")
				assert.Equal(t, canonical(t, storage), canonical(t, published),
					"exported Markdown:\n%s", doc.Markdown)
			}
		})
	}
}

// TestRoundTripTextLikeDirectives: page text that reads like one of the HTML
// comments mark acts on before goldmark -- an include, a macro, a header, an
// ignored region -- publishes back as that text, wherever on the page it
// stands, and none of them takes effect.
func TestRoundTripTextLikeDirectives(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("from the file"), 0o600))

	directives := map[string]string{
		"include":          "<!-- Include: outside.txt -->",
		"include unspaced": "<!--Include:outside.txt-->",
		"macro":            "<!-- Macro: MARKER\nTemplate: outside.txt -->",
		"title":            "<!-- Title: Changed -->",
		"space":            "<!-- Space: OTHER -->",
		"ignore":           "<!-- ac:ignore -->\nMARKER\n<!-- ac:ignore end -->",
	}

	c := func(s string) string { return "<![CDATA[" + s + "]]>" }

	for name, text := range directives {
		t.Run(name, func(t *testing.T) {
			x := escapeXMLText(text)
			line := escapeXMLText(strings.ReplaceAll(text, "\n", " "))

			for where, storage := range map[string]string{
				"paragraph":  "<p>" + x + "</p>",
				"emphasis":   "<p><strong>" + line + "</strong> and <em>" + line + "</em></p>",
				"heading":    "<h2>" + line + "</h2>",
				"list item":  "<ul><li>" + x + "</li></ul>",
				"blockquote": "<blockquote><p>" + x + "</p></blockquote>",
				"table":      "<table><thead><tr><th>h</th></tr></thead><tbody><tr><td>" + line + "</td></tr></tbody></table>",
				"link":       `<p><a href="https://example.com" title="` + line + `">` + line + "</a></p>",
				"image": `<p><ac:image ac:alt="` + line + `" ac:title="` + line + `">` +
					`<ri:url ri:value="https://example.com/a.png"/></ac:image></p>`,
				"page link": `<p><ac:link><ri:page ri:content-title="Other"/>` +
					"<ac:plain-text-link-body>" + c(strings.ReplaceAll(text, "\n", " ")) + "</ac:plain-text-link-body></ac:link></p>",
				"noformat": `<ac:structured-macro ac:name="noformat"><ac:plain-text-body>` + c(text) +
					"</ac:plain-text-body></ac:structured-macro>",
				"code title": `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language"></ac:parameter>` +
					`<ac:parameter ac:name="collapse">false</ac:parameter><ac:parameter ac:name="title">` + line +
					"</ac:parameter><ac:plain-text-body>" + c("x") + "</ac:plain-text-body></ac:structured-macro>",
				"code": `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language"></ac:parameter>` +
					`<ac:parameter ac:name="collapse">false</ac:parameter><ac:plain-text-body>` + c(text) +
					"</ac:plain-text-body></ac:structured-macro>",
				"code span": "<p><code>" + line + "</code></p>",
			} {
				storage += "<p>MARKER</p>"

				doc, err := Convert(storage, Options{})
				require.NoError(t, err, where)

				meta, published := publishBack(t, doc.Markdown, filepath.Join(dir, "page.md"))
				// The id is mark's own, which the page did not have.
				published = headingID.ReplaceAllString(published, "<h2>")
				assert.Equal(t, canonical(t, storage), canonical(t, published),
					"%s: exported Markdown:\n%s", where, doc.Markdown)
				assert.NotContains(t, published, "from the file", "%s: nothing is included", where)
				assert.Empty(t, meta.Title, "%s: no header is read", where)
				assert.Empty(t, meta.Space, "%s: no header is read", where)
			}
		})
	}
}

var headingID = regexp.MustCompile(`<h2 id="[^"]*">`)

// TestRoundTripDirectiveAcrossElements: a "<" that ends one piece of text and
// a "!" that starts the next -- across an inline comment's marker, which the
// export leaves out -- do not meet as "<!--", and a heading's anchor named
// with "<!--" does not put it in the heading line.
func TestRoundTripDirectiveAcrossElements(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("from the file"), 0o600))

	for name, storage := range map[string]string{
		"comment marker": `<p>&lt;<ac:inline-comment-marker ac:ref="r">!--Include:outside.txt--&gt;</ac:inline-comment-marker></p>`,
		"nested marker": `<p>&lt;<ac:inline-comment-marker ac:ref="r">!</ac:inline-comment-marker>` +
			`<ac:inline-comment-marker ac:ref="s">--Include:outside.txt--&gt;</ac:inline-comment-marker></p>`,
		"heading anchor": `<h2><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">&lt;!--Include:outside.txt--&gt;</ac:parameter>` +
			`</ac:structured-macro>Other</h2><p><ac:link ac:anchor="&lt;!--Include:outside.txt--&gt;"><ac:plain-text-link-body>` +
			`<![CDATA[go]]></ac:plain-text-link-body></ac:link></p>`,
	} {
		doc, err := Convert(storage, Options{})
		require.NoError(t, err, name)
		assert.NotContains(t, doc.Markdown, "<!--", name)

		_, published := publishBack(t, doc.Markdown, filepath.Join(dir, "page.md"))
		assert.NotContains(t, published, "from the file", "%s: exported Markdown:\n%s", name, doc.Markdown)
		assert.Contains(t, published, "&lt;!--Include:outside.txt--&gt;", "%s: exported Markdown:\n%s", name, doc.Markdown)
	}
}

// TestRoundTripHeadingAnchorIDs: a heading's anchor whose name mark would
// not read back from {#id} stays in the heading as the anchor it is.
func TestRoundTripHeadingAnchorIDs(t *testing.T) {
	for _, name := range []string{"a(b", "a<!b", "x/y"} {
		storage := `<h2><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">` + escapeXMLText(name) +
			`</ac:parameter></ac:structured-macro>Other</h2><p><ac:link ac:anchor="` + escapeAttr(name) + `">` +
			`<ac:plain-text-link-body><![CDATA[go]]></ac:plain-text-link-body></ac:link></p>`

		doc, err := Convert(storage, Options{})
		require.NoError(t, err, name)

		published := compile(t, doc.Markdown, filepath.Join(t.TempDir(), "page.md"))
		assert.NotContains(t, published, "{#", "%s: exported Markdown:\n%s", name, doc.Markdown)
		assert.Contains(t, published, `<ac:parameter ac:name="">`+escapeXMLText(name)+"</ac:parameter>",
			"%s: exported Markdown:\n%s", name, doc.Markdown)
	}
}

// TestRoundTripDirectiveHeadingID: a heading whose text reads like a
// directive gets the id mark gives its words, the "<!" dropped as any other
// punctuation is.
func TestRoundTripDirectiveHeadingID(t *testing.T) {
	doc, err := Convert("<h2>&lt;!-- Title: Changed --&gt;</h2>", Options{})
	require.NoError(t, err)

	published := compile(t, doc.Markdown, filepath.Join(t.TempDir(), "page.md"))
	assert.Equal(t, canonical(t, `<h2 id="---Title-Changed---">&lt;!-- Title: Changed --&gt;</h2>`), canonical(t, published),
		"exported Markdown:\n%s", doc.Markdown)
}

// TestRoundTripCodeKeepsDirectives: a code block holds what the page shows
// byte for byte, a directive's spelling included, since mark leaves code
// alone.
func TestRoundTripCodeKeepsDirectives(t *testing.T) {
	doc, err := Convert(`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language"></ac:parameter>`+
		`<ac:parameter ac:name="collapse">false</ac:parameter><ac:plain-text-body><![CDATA[<!-- Include: outside.txt -->]]>`+
		`</ac:plain-text-body></ac:structured-macro><p><code>&lt;!-- Include: outside.txt --&gt;</code></p>`, Options{})
	require.NoError(t, err)
	assert.Equal(t, "```\n<!-- Include: outside.txt -->\n```\n\n`<!-- Include: outside.txt -->`\n", doc.Markdown)
}

// publishBack publishes Markdown as mark publish does a file at path: its
// ignored regions dropped, its headers read, and the rest compiled.
func publishBack(t *testing.T, md, path string) (*metadata.Meta, string) {
	t.Helper()

	stripped, err := metadata.StripIgnoredBlocks([]byte(md))
	require.NoError(t, err, "exported Markdown:\n%s", md)

	meta, body, err := metadata.ExtractMeta(stripped, metadata.ExtractOptions{Filename: path})
	require.NoError(t, err, "exported Markdown:\n%s", md)
	if meta == nil {
		meta = &metadata.Meta{}
	}

	return meta, compile(t, string(body), path)
}

// compile publishes Markdown the way the golden tests in markdown/ do, as if
// it were the document at path.
func compile(t *testing.T, md, path string) string {
	t.Helper()

	return compileWith(t, md, path, "mkdocsadmonitions", "mention", "plantuml", "frontmatter")
}

// compileWith is compile with the features given.
func compileWith(t *testing.T, md, path string, features ...string) string {
	t.Helper()

	lib, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	out, _, err := markdown.CompileMarkdown(t.Context(), []byte(md), lib, path, markdown.Options{
		MermaidScale: 1.0,
		D2Scale:      1.0,
		MathScale:    1.0,
		MathOutput:   "svg",
		Features:     features,
	})
	require.NoError(t, err, "exported Markdown:\n%s", md)

	return out
}

// canonical writes storage format out in one spelling: attributes in order,
// text escaped one way, CDATA as text, and none of the whitespace between
// block elements that neither Confluence nor a reader sees.
func canonical(t *testing.T, storage string) string {
	t.Helper()

	root, err := parseStorage(storage)
	require.NoError(t, err)

	var b strings.Builder
	writeCanonical(&b, root)

	return b.String()
}

var inlineParents = map[string]bool{
	"p": true, "a": true, "strong": true, "em": true, "b": true, "i": true, "code": true, "span": true,
	"del": true, "s": true, "u": true, "sup": true, "sub": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "ac:parameter": true, "ac:plain-text-body": true,
	"ac:plain-text-link-body": true, "ac:link-body": true, "ac:task-body": true, "summary": true,
}

func writeCanonical(b *strings.Builder, n *node) {
	for _, child := range n.children {
		if child.kind == textNode {
			text := child.text
			if !inlineParents[n.name] {
				text = strings.TrimSpace(text)
			}
			b.WriteString(escapeXMLText(text))
			continue
		}

		b.WriteString("<" + child.name)
		attrs := append([]attr(nil), child.attrs...)
		sort.SliceStable(attrs, func(i, j int) bool { return attrs[i].name < attrs[j].name })
		for _, a := range attrs {
			b.WriteString(" " + a.name + `="` + escapeAttr(a.value) + `"`)
		}
		b.WriteString(">")
		writeCanonical(b, child)
		b.WriteString("</" + child.name + ">\n")
	}
}
