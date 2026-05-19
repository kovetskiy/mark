package renderer

import (
	"errors"
	"fmt"
	htmlstdlib "html"
	"net/url"
	"os"
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
	return &ConfluenceHTMLBlockRenderer{
		Config:      htmlrenderer.NewConfig(),
		Stdlib:      stdlib,
		Path:        path,
		Attachments: attachments,
		ImageAlign:  imageAlign,
	}
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
	return r.goldmarkRenderHTMLBlock(w, source, node, entering)
}

// isURLScheme reports whether s is a recognised URL scheme that should be
// treated as a remote reference rather than a local file path.
func isURLScheme(s string) bool {
	switch s {
	case "http", "https", "ftp", "ftps", "data", "mailto":
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
	alt = htmlstdlib.EscapeString(alt)
	title = htmlstdlib.EscapeString(title)

	if u, err := url.Parse(src); err == nil && isURLScheme(u.Scheme) {
		escapedURL := htmlstdlib.EscapeString(src)
		effectiveAlign := calculateAlign(r.ImageAlign, width)
		effectiveLayout := calculateLayout(effectiveAlign, width)
		displayWidth := calculateDisplayWidth(width, effectiveLayout)
		err = r.Stdlib.Templates.ExecuteTemplate(w, "ac:image", acImageParams{
			Align:  effectiveAlign,
			Layout: effectiveLayout,
			Width:  displayWidth,
			Title:  title,
			Alt:    alt,
			Url:    escapedURL,
		})
		if err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkSkipChildren, nil
	}

	attachments, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(r.Path), []string{src})
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return ast.WalkStop, fmt.Errorf("resolving img src %q: %w", src, err)
		}
		// File not found — fall back to rendering as a URL.
		escapedURL := htmlstdlib.EscapeString(src)
		effectiveAlign := calculateAlign(r.ImageAlign, width)
		effectiveLayout := calculateLayout(effectiveAlign, width)
		displayWidth := calculateDisplayWidth(width, effectiveLayout)
		err = r.Stdlib.Templates.ExecuteTemplate(w, "ac:image", acImageParams{
			Align:  effectiveAlign,
			Layout: effectiveLayout,
			Width:  displayWidth,
			Title:  title,
			Alt:    alt,
			Url:    escapedURL,
		})
		if err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkSkipChildren, nil
	}
	if len(attachments) == 0 {
		return ast.WalkStop, fmt.Errorf("img src %q: no attachment resolved", src)
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
		Width:          displayWidth,
		Title:          title,
		Alt:            alt,
		Attachment:     attachments[0].Filename,
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
	doc, err := html.ParseFragment(strings.NewReader(raw), nil)
	if err != nil {
		return
	}
	// html.ParseFragment wraps output in <html><head/><body>…</body></html>
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, a := range n.Attr {
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
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range doc {
		walk(n)
	}
	return
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
