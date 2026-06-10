package renderer

import (
	"errors"
	"fmt"
	htmlstdlib "html"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/vfs"
	"golang.org/x/net/html"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	htmlrenderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceHTMLBlockRenderer struct {
	htmlrenderer.Config
	Stdlib      *stdlib.Lib
	Path        string
	Attachments attachment.Attacher
	ImageAlign  string
}

func NewConfluenceHTMLBlockRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, path string, imageAlign string, opts ...htmlrenderer.Option) renderer.NodeRenderer {
	r := &ConfluenceHTMLBlockRenderer{
		Config:      htmlrenderer.NewConfig(),
		Stdlib:      stdlib,
		Path:        path,
		Attachments: attachments,
		ImageAlign:  imageAlign,
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

func (r *ConfluenceHTMLBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
}

func (r *ConfluenceHTMLBlockRenderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return r.goldmarkRenderHTMLBlock(w, source, node, entering)
	}

	n := node.(*ast.HTMLBlock)
	l := n.Lines().Len()
	for i := 0; i < l; i++ {
		line := n.Lines().At(i)
		raw := strings.TrimSpace(string(line.Value(source)))

		switch raw {
		case "<!-- ac:layout -->":
			_, _ = w.WriteString("<ac:layout>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout end -->":
			_, _ = w.WriteString("</ac:layout>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:single -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"single\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_equal -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_equal\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_left_sidebar -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_left_sidebar\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_right_sidebar -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_right_sidebar\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:three -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"three\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:three_with_sidebars -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"three_with_sidebars\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section end -->":
			_, _ = w.WriteString("</ac:layout-section>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-cell -->":
			_, _ = w.WriteString("<ac:layout-cell>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-cell end -->":
			_, _ = w.WriteString("</ac:layout-cell>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:placeholder -->":
			_, _ = w.WriteString("<ac:placeholder>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:placeholder end -->":
			_, _ = w.WriteString("</ac:placeholder>\n")
			return ast.WalkContinue, nil
		}

		if l == 1 {
			if status, err := r.tryRenderImgTag(w, raw); status != ast.WalkContinue || err != nil {
				return status, err
			}
		} else {
			break
		}
	}

	if l > 1 {
		if status, err := r.tryRenderImgTagLines(w, source, n); status != ast.WalkContinue || err != nil {
			return status, err
		}
	}

	return r.goldmarkRenderHTMLBlock(w, source, node, entering)
}

// isURLScheme reports whether s is a recognised URL scheme that should be
// treated as a remote reference rather than a local file path.
func isURLScheme(s string) bool {
	switch s {
	case "http", "https", "ftp", "ftps", "data", "mailto", "blob":
		return true
	}
	return false
}

// isDangerousScheme reports whether s is a scheme that must never be rendered,
// regardless of context.
func isDangerousScheme(s string) bool {
	switch s {
	case "javascript", "vbscript", "file":
		return true
	}
	return false
}

// tryRenderImgTag checks if raw is an <img> tag and renders it as ac:image.
// Returns WalkSkipChildren if handled, WalkContinue if not an img tag.
func (r *ConfluenceHTMLBlockRenderer) tryRenderImgTag(w util.BufWriter, raw string) (ast.WalkStatus, error) {
	src, width, alt, title := parseImgAttrs(raw)
	if src == "" {
		return ast.WalkContinue, nil
	}

	if u, err := url.Parse(src); err == nil {
		scheme := strings.ToLower(u.Scheme)
		if isDangerousScheme(scheme) {
			return ast.WalkStop, fmt.Errorf("img src %q: unsupported URL scheme %q", src, u.Scheme)
		}
		if isURLScheme(scheme) || strings.Contains(src, "://") {
			return r.renderImgURL(w, src, width, alt, title)
		}
	}

	attachments, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(r.Path), []string{src})
	if err != nil {
		return r.renderImgURL(w, src, width, alt, title)
	}
	if len(attachments) == 0 {
		return r.renderImgURL(w, src, width, alt, title)
	}

	r.Attachments.Attach(attachments[0])

	// Width from the <img> tag takes precedence over the detected file width.
	effectiveWidth := width
	if effectiveWidth == "" {
		effectiveWidth = attachments[0].Width
	}

	effectiveAlign := calculateAlign(r.ImageAlign, effectiveWidth)
	effectiveLayout := calculateLayout(effectiveAlign, effectiveWidth)
	displayWidth := calculateDisplayWidth(effectiveWidth, effectiveLayout)

	err = r.Stdlib.Templates.ExecuteTemplate(w, "ac:image", acImageParams{
		Align:          effectiveAlign,
		Layout:         effectiveLayout,
		OriginalWidth:  attachments[0].Width,
		OriginalHeight: attachments[0].Height,
		Width:          htmlstdlib.EscapeString(displayWidth),
		Title:          htmlstdlib.EscapeString(title),
		Alt:            htmlstdlib.EscapeString(alt),
		Attachment:     htmlstdlib.EscapeString(attachments[0].Filename),
	})
	if err != nil {
		return ast.WalkStop, err
	}
	return ast.WalkSkipChildren, nil
}

func (r *ConfluenceHTMLBlockRenderer) tryRenderImgTagLines(w util.BufWriter, source []byte, node *ast.HTMLBlock) (ast.WalkStatus, error) {
	l := node.Lines().Len()
	lines := make([]string, 0, l)

	for i := 0; i < l; i++ {
		line := node.Lines().At(i)
		raw := strings.TrimSpace(string(line.Value(source)))
		if raw == "" {
			continue
		}
		src, _, _, _ := parseImgAttrs(raw)
		if src == "" {
			return ast.WalkContinue, nil
		}
		lines = append(lines, raw)
	}

	if len(lines) == 0 {
		return ast.WalkContinue, nil
	}

	for _, raw := range lines {
		status, err := r.tryRenderImgTag(w, raw)
		if err != nil {
			return status, err
		}
		if status != ast.WalkSkipChildren {
			return ast.WalkContinue, nil
		}
	}

	return ast.WalkSkipChildren, nil
}

func (r *ConfluenceHTMLBlockRenderer) renderImgURL(w util.BufWriter, src, width, alt, title string) (ast.WalkStatus, error) {
	escapedURL := htmlstdlib.EscapeString(src)
	effectiveAlign := calculateAlign(r.ImageAlign, width)
	effectiveLayout := calculateLayout(effectiveAlign, width)
	displayWidth := calculateDisplayWidth(width, effectiveLayout)
	err := r.Stdlib.Templates.ExecuteTemplate(w, "ac:image", acImageParams{
		Align:  effectiveAlign,
		Layout: effectiveLayout,
		Width:  htmlstdlib.EscapeString(displayWidth),
		Title:  htmlstdlib.EscapeString(title),
		Alt:    htmlstdlib.EscapeString(alt),
		Url:    escapedURL,
	})
	if err != nil {
		return ast.WalkStop, err
	}
	return ast.WalkSkipChildren, nil
}

// acImageParams holds the parameters for the ac:image template.
type acImageParams struct {
	Align          string
	Layout         string
	OriginalWidth  string
	OriginalHeight string
	Width          string
	Height         string
	Title          string
	Alt            string
	Attachment     string
	Url            string
}

// parseImgAttrs parses src, width, alt, and title from an HTML <img> tag.
func parseImgAttrs(raw string) (src, width, alt, title string) {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	seenImg := false

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(tokenizer.Err(), io.EOF) && seenImg {
				return
			}
			return "", "", "", ""
		case html.TextToken:
			if strings.TrimSpace(string(tokenizer.Text())) != "" {
				return "", "", "", ""
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if seenImg || token.Data != "img" {
				return "", "", "", ""
			}
			seenImg = true
			for _, a := range token.Attr {
				switch a.Key {
				case "src":
					src = a.Val
				case "width":
					width = a.Val
				case "alt":
					alt = a.Val
				case "title":
					title = a.Val
				}
			}
		default:
			return "", "", "", ""
		}
	}
}

func (r *ConfluenceHTMLBlockRenderer) goldmarkRenderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.HTMLBlock)
	if entering {
		if r.Unsafe {
			l := n.Lines().Len()
			for i := 0; i < l; i++ {
				line := n.Lines().At(i)
				r.Writer.SecureWrite(w, line.Value(source))
			}
		} else {
			_, _ = w.WriteString("<!-- raw HTML omitted -->\n")
		}
	} else {
		if n.HasClosure() {
			if r.Unsafe {
				closure := n.ClosureLine
				r.Writer.SecureWrite(w, closure.Value(source))
			} else {
				_, _ = w.WriteString("<!-- raw HTML omitted -->\n")
			}
		}
	}
	return ast.WalkContinue, nil
}
