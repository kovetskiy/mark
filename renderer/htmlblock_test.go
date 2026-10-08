package renderer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

// fakeAttacher records calls to Attach for inspection in tests.
type fakeAttacher struct {
	attached []attachment.Attachment
}

func (f *fakeAttacher) Attach(a attachment.Attachment) {
	f.attached = append(f.attached, a)
}

func newTestRenderer(t *testing.T, imageAlign string, attachments attachment.Attacher, path string) *ConfluenceHTMLBlockRenderer {
	t.Helper()
	lib, err := stdlib.New(nil)
	if err != nil {
		t.Fatalf("stdlib.New: %v", err)
	}
	renderer := NewConfluenceHTMLBlockRenderer(lib, attachments, path, imageAlign)
	htmlBlockRenderer, ok := renderer.(*ConfluenceHTMLBlockRenderer)
	if !ok {
		t.Fatalf("renderer = %T, want *ConfluenceHTMLBlockRenderer", renderer)
	}
	return htmlBlockRenderer
}

func TestHTMLBlock_NonImgTagFallback(t *testing.T) {
	r := newTestRenderer(t, "left", &fakeAttacher{}, "/docs/page.md")

	source := []byte(`<p>Hello World</p>`)
	doc := parser.New().Parse(source)
	if _, ok := doc.FirstChild().(*ast.HTMLBlock); !ok {
		t.Fatalf("source parsed as %T, want an HTML block", doc.FirstChild())
	}

	var buf bytes.Buffer
	err := html.New(html.WithUnsafe(), html.WithExtensions(r)).Render(&buf, source, doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Unsafe {
		t.Errorf("renderer was not given the Unsafe option it is registered with")
	}
	out := buf.String()
	if !strings.Contains(out, "<p>Hello World</p>") {
		t.Errorf("expected fallback output to contain original HTML, got: %s", out)
	}
}
