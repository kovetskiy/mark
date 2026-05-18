package renderer

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/vfs"
	"github.com/yuin/goldmark/ast"
)

func TestParseImgAttrs(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSrc     string
		wantWidth   string
		wantAlt     string
		wantTitle   string
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

// fakeOpener implements vfs.Opener by returning fixed content for known paths.
type fakeOpener struct {
	files map[string][]byte
}

func (f *fakeOpener) Open(name string) (io.ReadCloser, error) {
	data, ok := f.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
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

func TestTryRenderImgTag_URL_AmpersandEscaped(t *testing.T) {
	r := newTestRenderer(t, "", &fakeAttacher{}, "/docs/page.md")

	var buf bufWriter
	_, err := r.tryRenderImgTag(&buf, `<img src="https://example.com/img?a=1&b=2" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `ri:value="https://example.com/img?a=1&amp;b=2"`) {
		t.Errorf("ampersand not escaped in output: %s", buf.String())
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
	// Write a tiny 1x1 PNG so image.DecodeConfig succeeds.
	tmpDir := t.TempDir()
	pngBytes, _ := os.ReadFile(filepath.Join("..", "testdata", "test.png"))
	if pngBytes == nil {
		t.Skip("testdata/test.png not found")
	}
	imgPath := filepath.Join(tmpDir, "arch.png")
	if err := os.WriteFile(imgPath, pngBytes, 0644); err != nil {
		t.Fatal(err)
	}

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

func TestTryRenderImgTag_LocalFile_IOError(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a file then make it unreadable.
	imgPath := filepath.Join(tmpDir, "secret.png")
	if err := os.WriteFile(imgPath, []byte("data"), 0000); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		t.Skip("running as root, permission check not effective")
	}

	r := newTestRenderer(t, "", &fakeAttacher{}, filepath.Join(tmpDir, "page.md"))

	var buf bufWriter
	_, err := r.tryRenderImgTag(&buf, `<img src="secret.png" />`)
	if err == nil {
		t.Error("expected error for unreadable local file, got nil")
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

// fakeVFSOpener lets us inject controlled file content into ResolveLocalAttachments.
// This is needed for testing without relying on the real filesystem permission behaviour.
var _ vfs.Opener = (*fakeOpener)(nil)
