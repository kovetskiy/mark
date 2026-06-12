package renderer

import (
	"errors"
	"fmt"
	htmlstdlib "html"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/vfs"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/html"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	htmlrenderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

var layoutComments = map[string]string{
	"<!-- ac:layout -->":                                  "<ac:layout>\n",
	"<!-- ac:layout end -->":                              "</ac:layout>\n",
	"<!-- ac:layout-section type:single -->":              "<ac:layout-section ac:type=\"single\">\n",
	"<!-- ac:layout-section type:two_equal -->":           "<ac:layout-section ac:type=\"two_equal\">\n",
	"<!-- ac:layout-section type:two_left_sidebar -->":    "<ac:layout-section ac:type=\"two_left_sidebar\">\n",
	"<!-- ac:layout-section type:two_right_sidebar -->":   "<ac:layout-section ac:type=\"two_right_sidebar\">\n",
	"<!-- ac:layout-section type:three -->":               "<ac:layout-section ac:type=\"three\">\n",
	"<!-- ac:layout-section type:three_with_sidebars -->": "<ac:layout-section ac:type=\"three_with_sidebars\">\n",
	"<!-- ac:layout-section end -->":                      "</ac:layout-section>\n",
	"<!-- ac:layout-cell -->":                             "<ac:layout-cell>\n",
	"<!-- ac:layout-cell end -->":                         "</ac:layout-cell>\n",
	"<!-- ac:placeholder -->":                             "<ac:placeholder>\n",
	"<!-- ac:placeholder end -->":                         "</ac:placeholder>\n",
}

// containsImgTagBytes checks case-insensitively whether a byte slice contains an '<img' substring.
// This is a zero-allocation fast-path check.
func containsImgTagBytes(b []byte) bool {
	for i := 0; i < len(b)-3; i++ {
		if b[i] == '<' &&
			(b[i+1] == 'i' || b[i+1] == 'I') &&
			(b[i+2] == 'm' || b[i+2] == 'M') &&
			(b[i+3] == 'g' || b[i+3] == 'G') {
			return true
		}
	}
	return false
}

func isWindowsDrivePath(s string) bool {
	if len(s) >= 3 && s[1] == ':' && (s[2] == '/' || s[2] == '\\') {
		c := s[0]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return false
}

type ConfluenceHTMLBlockRenderer struct {
	htmlrenderer.Config
	Stdlib      *stdlib.Lib
	Path        string
	Attachments attachment.Attacher
	ImageAlign  string
	ConvertImgs bool
}

func NewConfluenceHTMLBlockRenderer(stdlib *stdlib.Lib, opts ...htmlrenderer.Option) renderer.NodeRenderer {
	return newConfluenceHTMLBlockRenderer(stdlib, nil, "", "", false, opts...)
}

func NewConfluenceHTMLBlockRendererWithAttachments(stdlib *stdlib.Lib, attachments attachment.Attacher, path string, imageAlign string, opts ...htmlrenderer.Option) renderer.NodeRenderer {
	return newConfluenceHTMLBlockRenderer(stdlib, attachments, path, imageAlign, true, opts...)
}

func newConfluenceHTMLBlockRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, path string, imageAlign string, convertImgs bool, opts ...htmlrenderer.Option) renderer.NodeRenderer {
	r := &ConfluenceHTMLBlockRenderer{
		Config:      htmlrenderer.NewConfig(),
		Stdlib:      stdlib,
		Path:        path,
		Attachments: attachments,
		ImageAlign:  imageAlign,
		ConvertImgs: convertImgs,
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

	if ok, err := tryRenderLayoutComments(w, source, n); ok || err != nil {
		if err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}

	if r.ConvertImgs {
		// Zero-allocation scan lines first
		hasImgCandidate := false
		for i := 0; i < l; i++ {
			line := n.Lines().At(i)
			if containsImgTagBytes(line.Value(source)) {
				hasImgCandidate = true
				break
			}
		}
		if !hasImgCandidate {
			return r.goldmarkRenderHTMLBlock(w, source, node, entering)
		}

		var contentBuilder strings.Builder
		for i := 0; i < l; i++ {
			line := n.Lines().At(i)
			contentBuilder.Write(line.Value(source))
		}
		content := contentBuilder.String()

		if imgNodes, ok := parseHTML(content); ok {
			// Pre-validate all image nodes before writing anything to w
			for _, imgNode := range imgNodes {
				var src, width string
				for _, a := range imgNode.Attr {
					switch a.Key {
					case "src":
						src = a.Val
					case "width":
						width = a.Val
					}
				}
				_, valid, err := validateImgTagConversionInput(src, width)
				if err != nil {
					return ast.WalkStop, err
				}
				if !valid {
					return r.goldmarkRenderHTMLBlock(w, source, node, entering)
				}
			}

			// Convert all images
			for _, imgNode := range imgNodes {
				status, err := r.tryRenderImgTagNode(w, imgNode)
				if err != nil {
					return status, err
				}
				if status != ast.WalkSkipChildren {
					return r.goldmarkRenderHTMLBlock(w, source, node, entering)
				}
			}
			return ast.WalkSkipChildren, nil
		}
	}

	return r.goldmarkRenderHTMLBlock(w, source, node, entering)
}

func tryRenderLayoutComments(w util.BufWriter, source []byte, node *ast.HTMLBlock) (bool, error) {
	l := node.Lines().Len()
	var lines []string
	for i := 0; i < l; i++ {
		line := node.Lines().At(i)
		raw := strings.TrimSpace(string(line.Value(source)))
		if raw != "" {
			lines = append(lines, raw)
		}
	}
	if len(lines) == 0 {
		return false, nil
	}
	for _, raw := range lines {
		if !isLayoutComment(raw) {
			return false, nil
		}
	}
	for _, raw := range lines {
		if output, ok := layoutComments[raw]; ok {
			_, _ = w.WriteString(output)
		}
	}
	return true, nil
}

func isLayoutComment(raw string) bool {
	_, ok := layoutComments[raw]
	return ok
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

type invalidImgWidthError struct {
	width string
}

func (e *invalidImgWidthError) Error() string {
	return fmt.Sprintf("invalid width %q: expected a positive integer pixel value", e.width)
}

func validateImgWidth(width string) error {
	if width == "" {
		return nil
	}

	for _, r := range width {
		if r < '0' || r > '9' {
			return &invalidImgWidthError{width: width}
		}
	}

	n, err := strconv.Atoi(width)
	if err != nil || n <= 0 {
		return &invalidImgWidthError{width: width}
	}

	return nil
}

func validateImgTagConversionInput(src, width string) (string, bool, error) {
	// Sanitize src by stripping leading/trailing whitespace and ASCII control characters
	src = strings.TrimFunc(src, func(r rune) bool {
		return r <= ' ' || r == 127
	})

	if src == "" {
		return "", false, nil
	}

	if u, err := url.Parse(src); err == nil {
		scheme := strings.ToLower(u.Scheme)
		if isDangerousScheme(scheme) {
			return "", false, fmt.Errorf("img src %q: unsupported URL scheme %q", src, u.Scheme)
		}
	}

	if err := validateImgWidth(width); err != nil {
		var widthErr *invalidImgWidthError
		if errors.As(err, &widthErr) {
			log.Warn().
				Str("width", width).
				Err(err).
				Msg("skipping html img conversion")
			return "", false, nil
		}
		return "", false, err
	}

	return src, true, nil
}

// tryRenderImgTag checks if raw is an <img> tag and renders it as ac:image.
// Returns WalkSkipChildren if handled, WalkContinue if not an img tag.
func (r *ConfluenceHTMLBlockRenderer) tryRenderImgTag(w util.BufWriter, raw string) (ast.WalkStatus, error) {
	imgNodes, ok := parseHTML(raw)
	if !ok || len(imgNodes) != 1 {
		return ast.WalkContinue, nil
	}
	return r.tryRenderImgTagNode(w, imgNodes[0])
}

func (r *ConfluenceHTMLBlockRenderer) tryRenderImgTagNode(w util.BufWriter, n *html.Node) (ast.WalkStatus, error) {
	var src, width, alt, title string
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

	sanitizedSrc, valid, err := validateImgTagConversionInput(src, width)
	if err != nil {
		return ast.WalkStop, err
	}
	if !valid {
		return ast.WalkContinue, nil
	}

	if u, err := url.Parse(sanitizedSrc); err == nil {
		scheme := strings.ToLower(u.Scheme)
		if isURLScheme(scheme) || strings.HasPrefix(sanitizedSrc, "//") || strings.Contains(sanitizedSrc, "://") {
			return r.renderImgURL(w, sanitizedSrc, width, alt, title)
		}
	}

	// Force ri:url fallback for absolute paths or Windows UNC/drive paths
	// to prevent local file exfiltration outside relative directories.
	if filepath.IsAbs(sanitizedSrc) || isWindowsDrivePath(sanitizedSrc) || strings.HasPrefix(sanitizedSrc, "\\\\") {
		return r.renderImgURL(w, sanitizedSrc, width, alt, title)
	}

	if r.Attachments == nil {
		return r.renderImgURL(w, sanitizedSrc, width, alt, title)
	}

	attachments, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(r.Path), []string{sanitizedSrc})
	if err != nil {
		return r.renderImgURL(w, sanitizedSrc, width, alt, title)
	}
	if len(attachments) == 0 {
		return r.renderImgURL(w, sanitizedSrc, width, alt, title)
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

func parseHTML(content string) ([]*html.Node, bool) {
	doc, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return nil, false
	}
	var imgs []*html.Node
	var valid = true
	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if !valid {
			return
		}
		switch n.Type {
		case html.ElementNode:
			switch n.Data {
			case "html", "head", "body":
				// Standard containers, traverse children
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					traverse(c)
				}
			case "img":
				imgs = append(imgs, n)
				// An image node shouldn't contain other element nodes
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode {
						valid = false
						return
					}
				}
			default:
				// Any other element is invalid
				valid = false
			}
		case html.TextNode:
			// Text is only valid if it's whitespace
			if strings.TrimSpace(n.Data) != "" {
				valid = false
			}
		case html.CommentNode:
			// Any comment invalidates the standalone conversion to prevent silent comment data loss
			valid = false
		case html.DocumentNode, html.DoctypeNode:
			// Document roots, traverse children
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverse(c)
			}
		}
	}
	traverse(doc)
	return imgs, valid && len(imgs) > 0
}

func parseImgAttrs(raw string) (src, width, alt, title string) {
	imgNodes, ok := parseHTML(raw)
	if !ok || len(imgNodes) != 1 {
		return
	}
	for _, a := range imgNodes[0].Attr {
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
