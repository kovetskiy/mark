package syntax

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

// renderHeadings parses source with the given heading options and renders it
// as HTML, the narrowest way to see which ids a heading came out with.
func renderHeadings(t *testing.T, source string, opts []parser.Option) string {
	t.Helper()

	p := parser.New(opts...)
	doc := p.Parse([]byte(source))

	var buf bytes.Buffer
	if err := html.New(html.WithXHTML()).Render(&buf, []byte(source), doc); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}

// TestHeadingOptionsWithIDs proves the generated ids the option switches off
// are the only thing that changes: a heading named by hand keeps its id, and
// with generation off a heading has none.
func TestHeadingOptionsWithIDs(t *testing.T) {
	source := "# Title {#named}\n\n## Derived Heading\n"

	assert.Contains(t,
		renderHeadings(t, source, HeadingOptions()),
		`<h2 id="Derived-Heading">Derived Heading</h2>`,
		"ids on: a heading without a named id has one generated")

	assert.Contains(t,
		renderHeadings(t, source, HeadingOptionsWithIDs(false)),
		`<h2>Derived Heading</h2>`,
		"ids off: a heading without a named id has none")

	assert.Contains(t,
		renderHeadings(t, source, HeadingOptionsWithIDs(false)),
		`<h1 id="named">Title</h1>`,
		"ids off: an author's {#custom-id} is kept regardless")
}
