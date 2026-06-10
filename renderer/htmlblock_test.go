package renderer

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/yuin/goldmark/ast"
	htmlrenderer "github.com/yuin/goldmark/renderer/html"
)

func TestParseImgAttrs(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantSrc   string
		wantWidth string
		wantAlt   string
		wantTitle string
	}{
		{
			name:      "full attributes",
			input:     `<img src="arch.png" width="600" alt="Architecture" title="Arch diagram" />`,
			wantSrc:   "arch.png",
			wantWidth: "600",
			wantAlt:   "Architecture",
			wantTitle: "Arch diagram",
		},
		{
			name:      "src and width only",
			input:     `<img src="diagram.png" width="760" />`,
			wantSrc:   "diagram.png",
			wantWidth: "760",
		},
		{
			name:    "src only",
			input:   `<img src="logo.png" />`,
			wantSrc: "logo.png",
		},
		{
			name:      "no closing slash",
			input:     `<img src="foo.png" width="400">`,
			wantSrc:   "foo.png",
			wantWidth: "400",
		},
		{
			name:  "not an img tag",
			input: `<p>hello</p>`,
		},
		{
			name:  "empty",
			input: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, width, alt, title := parseImgAttrs(tt.input)
			if src != tt.wantSrc {
				t.Errorf("src = %q, want %q", src, tt.wantSrc)
			}
			if width != tt.wantWidth {
				t.Errorf("width = %q, want %q", width, tt.wantWidth)
			}
			if alt != tt.wantAlt {
				t.Errorf("alt = %q, want %q", alt, tt.wantAlt)
			}
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
		})
	}
}

// bufWriter wraps bytes.Buffer to satisfy util.BufWriter.
type bufWriter struct{ bytes.Buffer }

func (b *bufWriter) Buffered() int { return b.Len() }
func (b *bufWriter) Flush() error  { return nil }

// fakeAttacher records calls to Attach for inspection in tests.
type fakeAttacher struct {
	attached []attachment.Attachment
}

func (f *fakeAttacher) Attach(a attachment.Attachment) {
	f.attached = append(f.attached, a)
}

// makePNG writes a minimal valid PNG to path and returns its path.
func makePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func newTestRenderer(t *testing.T, imageAlign string, attachments attachment.Attacher, path string) *ConfluenceHTMLBlockRenderer {
	t.Helper()
	lib, err := stdlib.New(nil)
	if err != nil {
		t.Fatalf("stdlib.New: %v", err)
	}
	return &ConfluenceHTMLBlockRenderer{
		Stdlib:      lib,
		Path:        path,
		Attachments: attachments,
		ImageAlign:  imageAlign,
	}
}

func TestNewConfluenceHTMLBlockRenderer_AppliesHTMLOptions(t *testing.T) {
	lib, err := stdlib.New(nil)
	if err != nil {
		t.Fatalf("stdlib.New: %v", err)
	}

	renderer := NewConfluenceHTMLBlockRenderer(lib, &fakeAttacher{}, "/docs/page.md", "", htmlrenderer.WithUnsafe())
	htmlBlockRenderer, ok := renderer.(*ConfluenceHTMLBlockRenderer)
	if !ok {
		t.Fatalf("renderer = %T, want *ConfluenceHTMLBlockRenderer", renderer)
	}
	if !htmlBlockRenderer.Unsafe {
		t.Error("expected htmlrenderer.WithUnsafe option to be applied")
	}
}

func TestTryRenderImgTag_URL(t *testing.T) {
	attacher := &fakeAttacher{}
	r := newTestRenderer(t, "left", attacher, "/docs/page.md")

	var buf bufWriter
	status, err := r.tryRenderImgTag(&buf, `<img src="https://example.com/logo.png" alt="Logo" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != ast.WalkSkipChildren {
		t.Errorf("status = %v, want WalkSkipChildren", status)
	}
	if len(attacher.attached) != 0 {
		t.Errorf("expected no attachments, got %d", len(attacher.attached))
	}
	out := buf.String()
	if !strings.Contains(out, `ri:url ri:value="https://example.com/logo.png"`) {
		t.Errorf("output missing ri:url: %s", out)
	}
}

func TestTryRenderImgTag_URL_XMLEscaped(t *testing.T) {
	r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

	tests := []struct {
		name    string
		src     string
		wantURL string
	}{
		{"ampersand", "https://example.com/img?a=1&b=2", `https://example.com/img?a=1&amp;b=2`},
		{"less than", "https://example.com/img?a=<1", `https://example.com/img?a=&lt;1`},
		{"quote (html-encoded in src)", `https://example.com/img?a=&quot;1&quot;`, `https://example.com/img?a=&#34;1&#34;`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bufWriter
			_, err := r.tryRenderImgTag(&buf, `<img src="`+tt.src+`" />`)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(buf.String(), `ri:value="`+tt.wantURL+`"`) {
				t.Errorf("URL not correctly escaped in output: %s", buf.String())
			}
		})
	}
}

func TestTryRenderImgTag_URL_WideAlignForced(t *testing.T) {
	r := newTestRenderer(t, "left", &fakeAttacher{}, "/docs/page.md")

	var buf bufWriter
	_, err := r.tryRenderImgTag(&buf, `<img src="https://example.com/wide.png" width="800" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// width >= 760 must force center alignment, same as the local-attachment branch
	if !strings.Contains(out, `ac:align="center"`) {
		t.Errorf("expected center alignment for wide external image, got: %s", out)
	}
}

func TestTryRenderImgTag_LocalAttachment(t *testing.T) {
	tmpDir := t.TempDir()
	makePNG(t, filepath.Join(tmpDir, "arch.png"))

	attacher := &fakeAttacher{}
	r := newTestRenderer(t, "left", attacher, filepath.Join(tmpDir, "page.md"))

	var buf bufWriter
	status, err := r.tryRenderImgTag(&buf, `<img src="arch.png" width="600" alt="Arch" title="T" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != ast.WalkSkipChildren {
		t.Errorf("status = %v, want WalkSkipChildren", status)
	}
	if len(attacher.attached) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attacher.attached))
	}
	out := buf.String()
	if !strings.Contains(out, `ri:attachment ri:filename="arch.png"`) {
		t.Errorf("output missing attachment: %s", out)
	}
	if !strings.Contains(out, `ac:alt="Arch"`) {
		t.Errorf("output missing alt: %s", out)
	}
	if !strings.Contains(out, `ac:title="T"`) {
		t.Errorf("output missing title: %s", out)
	}
}

func TestTryRenderImgTag_TabAfterImg(t *testing.T) {
	r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

	var buf bufWriter
	status, err := r.tryRenderImgTag(&buf, "<img\tsrc=\"https://example.com/logo.png\" />")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != ast.WalkSkipChildren {
		t.Errorf("status = %v, want WalkSkipChildren", status)
	}
	if !strings.Contains(buf.String(), `ri:url ri:value="https://example.com/logo.png"`) {
		t.Errorf("output missing ri:url: %s", buf.String())
	}
}

func TestTryRenderImgTag_NotImgTag(t *testing.T) {
	r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

	var buf bufWriter
	status, err := r.tryRenderImgTag(&buf, `<p>hello</p>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != ast.WalkContinue {
		t.Errorf("status = %v, want WalkContinue", status)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for non-img tag, got: %s", buf.String())
	}
}

func TestTryRenderImgTag_OnlyStandaloneImgTag(t *testing.T) {
	tests := []string{
		`<p>Caption <img src="https://example.com/logo.png" /></p>`,
		`<img src="https://example.com/one.png" /><img src="https://example.com/two.png" />`,
		`Text <img src="https://example.com/logo.png" />`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

			var buf bufWriter
			status, err := r.tryRenderImgTag(&buf, input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status != ast.WalkContinue {
				t.Errorf("status = %v, want WalkContinue", status)
			}
			if buf.Len() != 0 {
				t.Errorf("expected no output for non-standalone img tag, got: %s", buf.String())
			}
		})
	}
}

func TestTryRenderImgTag_AltTitleXMLEscaped(t *testing.T) {
	r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

	var buf bufWriter
	_, err := r.tryRenderImgTag(&buf, `<img src="https://example.com/x.png" alt="A &amp; B" title='He said "hi"' />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `ac:alt="A & B"`) {
		t.Error("unescaped & in alt attribute produces malformed XML")
	}
	if strings.Contains(out, `ac:title="He said "hi""`) {
		t.Error("unescaped \" in title attribute produces malformed XML")
	}
}

func TestTryRenderImgTag_MissingLocalFile(t *testing.T) {
	tmpDir := t.TempDir()
	r := newTestRenderer(t, "", &fakeAttacher{}, filepath.Join(tmpDir, "page.md"))

	var buf bufWriter
	status, err := r.tryRenderImgTag(&buf, `<img src="nonexistent.png" />`)
	if err != nil {
		t.Errorf("missing local file should not abort render, got error: %v", err)
	}
	if status == ast.WalkStop {
		t.Error("missing local file should not return WalkStop")
	}
	if !strings.Contains(buf.String(), `ri:url ri:value="nonexistent.png"`) {
		t.Errorf("missing local file should fall back to ri:url, got: %s", buf.String())
	}
}

// TestTryRenderImgTag_FilenameWithColon documents that a local filename containing a colon
// (e.g. "images:foo.png") must be resolved as a local attachment, not treated as a URL.
func TestTryRenderImgTag_FilenameWithColon(t *testing.T) {
	tmpDir := t.TempDir()
	makePNG(t, filepath.Join(tmpDir, "images:foo.png"))

	attacher := &fakeAttacher{}
	r := newTestRenderer(t, "", attacher, filepath.Join(tmpDir, "page.md"))

	var buf bufWriter
	_, err := r.tryRenderImgTag(&buf, `<img src="images:foo.png" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(attacher.attached) != 1 {
		t.Errorf("expected local file to be attached, got %d attachments; output: %s", len(attacher.attached), buf.String())
	}
	if !strings.Contains(buf.String(), `ri:attachment`) {
		t.Errorf("expected ri:attachment for local file with colon in name, got: %s", buf.String())
	}
}

// TestTryRenderImgTag_URL_FullWidthDisplayWidth documents that a wide external image
// must have its display width normalized to 1800 when layout is full-width,
// consistent with the attachment branch and calculateDisplayWidth.
func TestTryRenderImgTag_URL_FullWidthDisplayWidth(t *testing.T) {
	r := newTestRenderer(t, "center", &fakeAttacher{}, "/docs/page.md")

	var buf bufWriter
	_, err := r.tryRenderImgTag(&buf, `<img src="https://example.com/wide.png" width="2000" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `ac:width="2000"`) {
		t.Errorf("full-width layout should normalize ac:width to 1800, got: %s", out)
	}
	if !strings.Contains(out, `ac:width="1800"`) {
		t.Errorf("expected ac:width=\"1800\" for full-width layout, got: %s", out)
	}
}

func TestTryRenderImgTag_UnsupportedSchemeRejected(t *testing.T) {
	schemes := []struct {
		name string
		src  string
	}{
		{"javascript", "javascript:alert(1)"},
		{"file", "file:///etc/passwd"},
		{"vbscript", "vbscript:msgbox(1)"},
	}

	for _, tt := range schemes {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

			var buf bufWriter
			_, err := r.tryRenderImgTag(&buf, `<img src="`+tt.src+`" />`)
			if err == nil {
				t.Errorf("scheme %q should return an error, got nil; output: %s", tt.src, buf.String())
			}
			if strings.Contains(buf.String(), `ri:url`) {
				t.Errorf("scheme %q must not appear in ri:url output, got: %s", tt.src, buf.String())
			}
		})
	}
}

// TestTryRenderImgTag_BlobScheme documents that blob: URIs must not fall through
// to local file resolution and then silently render as ri:url with a literal blob: string.
// They should be treated as an external URL (ri:url) or rejected, not as local paths.
func TestTryRenderImgTag_BlobScheme(t *testing.T) {
	attacher := &fakeAttacher{}
	r := newTestRenderer(t, "", attacher, "/docs/page.md")

	var buf bufWriter
	status, err := r.tryRenderImgTag(&buf, `<img src="blob:https://example.com/some-uuid" />`)
	if err != nil {
		t.Fatalf("blob: URI should not error, got: %v", err)
	}
	if status == ast.WalkStop {
		t.Error("blob: URI should not return WalkStop")
	}
	if !strings.Contains(buf.String(), `ri:url`) {
		t.Errorf("blob: URI should render as ri:url, got: %s", buf.String())
	}
	if len(attacher.attached) != 0 {
		t.Errorf("blob: URI should not resolve as a local attachment, got %d attachments", len(attacher.attached))
	}
}

// TestTryRenderImgTag_UnknownSchemeGraceful documents that unknown hierarchical schemes
// (e.g. sftp://) must not abort the document walk with WalkStop.
// They should either render as ri:url or be skipped, but never be fatal.
func TestTryRenderImgTag_UnknownSchemeGraceful(t *testing.T) {
	schemes := []struct {
		name string
		src  string
	}{
		{"sftp", "sftp://host/img.png"},
		{"s3", "s3://bucket/img.png"},
		{"ssh", "ssh://host/img.png"},
	}

	for _, tt := range schemes {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

			var buf bufWriter
			status, err := r.tryRenderImgTag(&buf, `<img src="`+tt.src+`" />`)
			if err != nil {
				t.Errorf("scheme %q should not error, got: %v", tt.src, err)
			}
			if status == ast.WalkStop {
				t.Errorf("scheme %q should not return WalkStop, must not abort document render", tt.src)
			}
			if !strings.Contains(buf.String(), `ri:url`) {
				t.Errorf("scheme %q should render as ri:url, got: %s", tt.src, buf.String())
			}
		})
	}
}

// TestTryRenderImgTag_AttachmentFilenameXMLEscaped documents that a local
// filename containing XML-special characters (decoded from HTML entities in
// the src attribute) must be escaped before appearing in ri:filename.
// e.g. src="arch&amp;logo.png" decodes to arch&logo.png which would produce
// malformed XML if written as-is into ri:filename="arch&logo.png".
func TestTryRenderImgTag_AttachmentFilenameXMLEscaped(t *testing.T) {
	tmpDir := t.TempDir()
	makePNG(t, filepath.Join(tmpDir, "arch&logo.png"))

	attacher := &fakeAttacher{}
	r := newTestRenderer(t, "", attacher, filepath.Join(tmpDir, "page.md"))

	var buf bufWriter
	_, err := r.tryRenderImgTag(&buf, `<img src="arch&amp;logo.png" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `ri:filename="arch&logo.png"`) {
		t.Error("unescaped & in ri:filename produces malformed XML")
	}
	if !strings.Contains(out, `ri:filename="arch&amp;logo.png"`) {
		t.Errorf("expected escaped ri:filename, got: %s", out)
	}
}

// TestTryRenderImgTag_WidthXMLEscaped documents that a width attribute
// containing XML-special characters must not appear unescaped in ac:width.
// e.g. width="100&amp;x" decodes to 100&x which would produce malformed XML.
func TestTryRenderImgTag_WidthXMLEscaped(t *testing.T) {
	r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

	var buf bufWriter
	_, err := r.tryRenderImgTag(&buf, `<img src="https://example.com/img.png" width="100&amp;x" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `ac:width="100&x"`) {
		t.Error("unescaped & in ac:width produces malformed XML")
	}
}

func TestTryRenderImgTag_NonHTTPScheme(t *testing.T) {
	schemes := []struct {
		name string
		src  string
	}{
		{"data URI", "data:image/png;base64,abc"},
		{"ftp", "ftp://example.com/img.png"},
		{"mailto", "mailto:test@example.com"},
	}

	for _, tt := range schemes {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

			var buf bufWriter
			status, err := r.tryRenderImgTag(&buf, `<img src="`+tt.src+`" />`)
			if err != nil {
				t.Errorf("scheme %q should not cause an error, got: %v", tt.src, err)
			}
			if status == ast.WalkStop {
				t.Errorf("scheme %q should not return WalkStop", tt.src)
			}
			if !strings.Contains(buf.String(), `ri:url`) {
				t.Errorf("scheme %q should render as ri:url, got: %s", tt.src, buf.String())
			}
		})
	}
}
