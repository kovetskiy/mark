package renderer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// fakeAttacher records calls to Attach for inspection in tests.
type fakeAttacher struct {
	attached []attachment.Attachment
}

func (f *fakeAttacher) Attach(a attachment.Attachment) {
	f.attached = append(f.attached, a)
}

func newHTMLBlockFromSource(source []byte) *ast.HTMLBlock {
	node := ast.NewHTMLBlock(ast.HTMLBlockType6)
	start := 0
	for start < len(source) {
		offset := bytes.IndexByte(source[start:], '\n')
		if offset < 0 {
			node.Lines().Append(text.NewSegment(start, len(source)))
			break
		}
		end := start + offset
		node.Lines().Append(text.NewSegment(start, end))
		start = end + 1
	}
	return node
}

// bufWriter wraps bytes.Buffer to satisfy util.BufWriter.
type bufWriter struct{ bytes.Buffer }

func (b *bufWriter) Buffered() int { return b.Len() }
func (b *bufWriter) Flush() error  { return nil }

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
	r.Unsafe = true

	var buf bufWriter
	source := []byte(`<p>Hello World</p>`)
	node := newHTMLBlockFromSource(source)
	status, err := r.renderHTMLBlock(&buf, source, node, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != ast.WalkContinue {
		t.Errorf("status = %v, want WalkContinue", status)
	}
	out := buf.String()
	if !strings.Contains(out, "<p>Hello World</p>") {
		t.Errorf("expected fallback output to contain original HTML, got: %s", out)
	}
}
