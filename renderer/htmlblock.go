package renderer

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/vfs"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

var (
	imgTagRegex = regexp.MustCompile(`(?s)^<img\s+(.*?)>\s*$`)
	srcRegex    = regexp.MustCompile(`\bsrc=["']([^"']*)["']`)
	widthRegex  = regexp.MustCompile(`\bwidth=["']([^"']*)["']`)
	altRegex    = regexp.MustCompile(`\balt=["']([^"']*)["']`)
	titleRegex  = regexp.MustCompile(`\btitle=["']([^"']*)["']`)
)

type ConfluenceHTMLBlockRenderer struct {
	html.Config
	Stdlib      *stdlib.Lib
	Attachments attachment.Attacher
	Path        string
	ImageAlign  string
}

// NewConfluenceHTMLBlockRenderer creates a new instance of the ConfluenceHTMLBlockRenderer
func NewConfluenceHTMLBlockRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, path string, imageAlign string, opts ...html.Option) renderer.NodeRenderer {
	r := &ConfluenceHTMLBlockRenderer{
		Config:      html.NewConfig(),
		Stdlib:      stdlib,
		Attachments: attachments,
		Path:        path,
		ImageAlign:  imageAlign,
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
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
		lineStr := strings.Trim(string(line.Value(source)), "\n")

		lineTrimmed := strings.TrimSpace(lineStr)
		switch {
		case lineStr == "<!-- ac:layout -->":
			_, _ = w.WriteString("<ac:layout>\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout end -->":
			_, _ = w.WriteString("</ac:layout>\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-section type:single -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"single\">\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-section type:two_equal -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_equal\">\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-section type:two_left_sidebar -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_left_sidebar\">\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-section type:two_right_sidebar -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"two_right_sidebar\">\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-section type:three -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"three\">\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-section type:three_with_sidebars -->":
			_, _ = w.WriteString("<ac:layout-section ac:type=\"three_with_sidebars\">\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-section end -->":
			_, _ = w.WriteString("</ac:layout-section>\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-cell -->":
			_, _ = w.WriteString("<ac:layout-cell>\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:layout-cell end -->":
			_, _ = w.WriteString("</ac:layout-cell>\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:placeholder -->":
			_, _ = w.WriteString("<ac:placeholder>\n")
			return ast.WalkContinue, nil
		case lineStr == "<!-- ac:placeholder end -->":
			_, _ = w.WriteString("</ac:placeholder>\n")
			return ast.WalkContinue, nil
		case strings.HasPrefix(lineTrimmed, "<img"):
			status, err := r.tryRenderImgTag(w, lineTrimmed)
			if err != nil || status == ast.WalkSkipChildren {
				return status, err
			}
		}
	}

	return r.goldmarkRenderHTMLBlock(w, source, node, entering)
}

func (r *ConfluenceHTMLBlockRenderer) tryRenderImgTag(w util.BufWriter, line string) (ast.WalkStatus, error) {
	match := imgTagRegex.FindStringSubmatch(line)
	if match == nil {
		return ast.WalkContinue, nil
	}

	attrs := match[1]
	srcMatch := srcRegex.FindStringSubmatch(attrs)
	if srcMatch == nil {
		return ast.WalkContinue, nil
	}

	src := srcMatch[1]
	if !r.Unsafe && html.IsDangerousURL([]byte(src)) {
		return ast.WalkContinue, nil
	}
	var width, alt, title string
	if wMatch := widthRegex.FindStringSubmatch(attrs); wMatch != nil {
		width = wMatch[1]
	}
	if aMatch := altRegex.FindStringSubmatch(attrs); aMatch != nil {
		alt = aMatch[1]
	}
	if tMatch := titleRegex.FindStringSubmatch(attrs); tMatch != nil {
		title = tMatch[1]
	}

	attachments, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(r.Path), []string{src})
	if err != nil || len(attachments) == 0 {
		escapedURL := strings.ReplaceAll(src, "&", "&amp;")
		effectiveAlign := calculateAlign(r.ImageAlign, width)
		effectiveLayout := calculateLayout(effectiveAlign, width)
		displayWidth := calculateDisplayWidth(width, effectiveLayout)

		err = r.Stdlib.Templates.ExecuteTemplate(
			w,
			"ac:image",
			acImageParams{
				Align:  effectiveAlign,
				Layout: effectiveLayout,
				Width:  displayWidth,
				Title:  title,
				Alt:    alt,
				Url:    escapedURL,
			},
		)
		if err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkSkipChildren, nil
	}

	r.Attachments.Attach(attachments[0])
	effectiveWidth := width
	if effectiveWidth == "" {
		effectiveWidth = attachments[0].Width
	}
	effectiveAlign := calculateAlign(r.ImageAlign, effectiveWidth)
	effectiveLayout := calculateLayout(effectiveAlign, effectiveWidth)
	displayWidth := calculateDisplayWidth(effectiveWidth, effectiveLayout)

	err = r.Stdlib.Templates.ExecuteTemplate(
		w,
		"ac:image",
		acImageParams{
			Align:          effectiveAlign,
			Layout:         effectiveLayout,
			OriginalWidth:  attachments[0].Width,
			OriginalHeight: attachments[0].Height,
			Width:          displayWidth,
			Title:          title,
			Alt:            alt,
			Attachment:     attachments[0].Filename,
		},
	)
	if err != nil {
		return ast.WalkStop, err
	}
	return ast.WalkSkipChildren, nil
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
