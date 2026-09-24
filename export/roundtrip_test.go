package export

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	markdown "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
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

// compile publishes Markdown the way the golden tests in markdown/ do, as if
// it were the document at path.
func compile(t *testing.T, md, path string) string {
	t.Helper()

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := markdown.CompileMarkdown([]byte(md), lib, path, types.MarkConfig{
		MermaidScale: 1.0,
		D2Scale:      1.0,
		Features:     []string{"mkdocsadmonitions", "mention", "plantuml", "frontmatter"},
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
