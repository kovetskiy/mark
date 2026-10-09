package renderer

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	ctransformer "github.com/kovetskiy/mark/v16/transformer"
	"github.com/kovetskiy/mark/v16/vfs"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// calculateAlign determines the appropriate ac:align value
// Images >= 760px must use "center" alignment, smaller images can use configured alignment
func calculateAlign(configuredAlign string, width string) string {
	if configuredAlign == "" {
		return ""
	}

	if width != "" {
		widthInt, err := strconv.Atoi(width)
		if err == nil && widthInt >= 760 {
			return "center"
		}
	}

	return configuredAlign
}

// calculateLayout determines the appropriate ac:layout value based on width and alignment
// Images >= 1800px use "full-width", images >= 760px use "wide", otherwise based on alignment
// These thresholds are based on Confluence's behavior as of 2026-02, but may need adjustment in the future
// Returns empty string if no alignment is configured
func calculateLayout(align string, width string) string {
	if align == "" {
		return ""
	}

	if width != "" {
		widthInt, err := strconv.Atoi(width)
		if err == nil {
			if widthInt >= 1800 {
				return "full-width"
			}
			if widthInt >= 760 {
				return "wide"
			}
		}
	}

	switch align {
	case "left":
		return "align-start"
	case "center":
		return "center"
	case "right":
		return "align-end"
	default:
		return ""
	}
}

// calculateDisplayWidth determines the display width
// Full-width layout uses 1800px, otherwise uses original width
func calculateDisplayWidth(originalWidth string, layout string) string {
	if layout == "full-width" {
		return "1800"
	}
	return originalWidth
}

// resolveWidth calculates the effective pixel width if explicitWidth is a percentage (e.g. "50%")
// and originalWidth is known. Otherwise returns explicitWidth or originalWidth.
func resolveWidth(explicitWidth string, originalWidth string) string {
	if explicitWidth == "" {
		return originalWidth
	}
	if strings.HasSuffix(explicitWidth, "%") && originalWidth != "" {
		percentStr := strings.TrimSuffix(explicitWidth, "%")
		if percent, err := strconv.ParseFloat(percentStr, 64); err == nil {
			if origWidth, err := strconv.ParseFloat(originalWidth, 64); err == nil {
				return strconv.Itoa(int(math.Round((percent / 100.0) * origWidth)))
			}
		}
	}
	return explicitWidth
}

type ConfluenceImageRenderer struct {
	htmlOptions

	Stdlib      *stdlib.Lib
	Path        string
	Attachments attachment.Attacher
	ImageAlign  string
}

// NewConfluenceImageRenderer creates a new instance of the ConfluenceImageRenderer
func NewConfluenceImageRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, path string, imageAlign string, opts ...html.Option) html.Extension {
	r := &ConfluenceImageRenderer{
		Stdlib:      stdlib,
		Path:        path,
		Attachments: attachments,
		ImageAlign:  imageAlign,
	}
	r.htmlOptions = newHTMLOptions(opts)
	return r
}

// RendererOptions implements html.Extension.
func (r *ConfluenceImageRenderer) RendererOptions(cfg *html.Config) []html.Option {
	r.configure(cfg)

	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		ast.KindImage: nodeRenderer(r.renderImage),
	})}
}

// renderImage renders an inline image
func (r *ConfluenceImageRenderer) renderImage(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)

	// Read as CommonMark defines it before anything looks at it, which is
	// also what goldmark's own renderer checks for a dangerous scheme.
	destination := n.Destination.Value(source)

	if !r.Unsafe && html.IsDangerousURL(destination) {
		return ast.WalkContinue, nil
	}

	explicitWidth, _ := ctransformer.AttributeText(n, "width", source)
	explicitHeight, _ := ctransformer.AttributeText(n, "height", source)
	explicitAlign, _ := ctransformer.AttributeText(n, "align", source)

	align := r.ImageAlign
	if explicitAlign != "" {
		align = explicitAlign
	}

	attached, err := r.resolveLocalImage(destination)

	// A path that reaches outside the project is refused rather than quietly
	// treated as a URL. The file is not uploaded either way, but publishing a
	// broken image and saying nothing leaves the author to work out why, and
	// leaves a document that tried to read somewhere it should not looking
	// like a typo.
	if errors.Is(err, attachment.ErrOutsideProject) {
		line, col := GetLineCol(source, node.Pos())

		return ast.WalkStop, fmt.Errorf("line %d, col %d: %w", line, col, err)
	}

	// We were unable to resolve it locally, treat as URL
	if err != nil {
		effectiveAlign := calculateAlign(align, explicitWidth)
		effectiveLayout := calculateLayout(effectiveAlign, explicitWidth)
		displayWidth := calculateDisplayWidth(explicitWidth, effectiveLayout)

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
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
			}{
				effectiveAlign,
				effectiveLayout,
				"",
				"",
				displayWidth,
				explicitHeight,
				r.imageTitle(n, source),
				r.imageAlt(n, source),
				"",
				destination,
			},
		)
	} else {
		r.Attachments.Attach(attached)

		effectiveWidth := resolveWidth(explicitWidth, attached.Width)
		effectiveAlign := calculateAlign(align, effectiveWidth)
		effectiveLayout := calculateLayout(effectiveAlign, effectiveWidth)
		displayWidth := calculateDisplayWidth(effectiveWidth, effectiveLayout)

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
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
			}{
				effectiveAlign,
				effectiveLayout,
				attached.Width,
				attached.Height,
				displayWidth,
				explicitHeight,
				r.imageTitle(n, source),
				r.imageAlt(n, source),
				attached.Filename,
				"",
			},
		)
	}

	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkSkipChildren, nil
}

// resolveLocalImage finds the file an image destination names beside the
// document, trying each spelling of it in turn.
//
// "my%20file.png" and "my\_file.png" name the same files as "<my file.png>" and
// "my_file.png", and were published as a relative ri:url instead: a broken
// image, with the file never uploaded. A path that reaches outside the project
// stops the search, whichever spelling reached it.
//
// Each spelling is resolved as written, never as a pattern: an image names one
// file, and "img[1].png" read as a glob published img1.png in its place.
func (r *ConfluenceImageRenderer) resolveLocalImage(destination string) (attachment.Attachment, error) {
	err := errors.New("not a local file")

	for _, path := range ctransformer.LocalImagePaths(destination) {
		var attached attachment.Attachment

		attached, err = attachment.ResolveLocalAttachment(vfs.LocalOS, filepath.Dir(r.Path), path)
		if err == nil || errors.Is(err, attachment.ErrOutsideProject) {
			return attached, err
		}
	}

	return attachment.Attachment{}, err
}

// imageTitle is the title as its text, ready to be escaped once by the
// template. Written as Markdown it still carries its backslash escapes and
// entity references, and passing those through put `\&#34;` and `&amp;amp;` on
// the page where `"` and `&` were meant.
func (r *ConfluenceImageRenderer) imageTitle(n *ast.Image, source []byte) string {
	return n.Title.Value(source)
}

// imageAlt is the alt text as its text, read the same way as the title.
func (r *ConfluenceImageRenderer) imageAlt(n *ast.Image, source []byte) string {
	var buf bytes.Buffer
	r.writeAltText(&buf, n, source)

	return buf.String()
}

// https://github.com/yuin/goldmark/blob/c446c414ef3a41fb562da0ae5badd18f1502c42f/renderer/html/html.go
//
// Nothing here is escaped: the result is interpolated into an attribute by the
// ac:image template, which escapes it, and escaping here as well puts a literal
// &amp;amp; on the page.
func (r *ConfluenceImageRenderer) writeAltText(buf *bytes.Buffer, n ast.Node, source []byte) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ctransformer.String:
			// The <img> transformer hands the alt text over as a plain String,
			// and only the IsCode branch existed, so every alt written as an
			// HTML attribute was silently dropped while the Markdown spelling
			// kept its own. It is already the text: the HTML parser decoded it.
			buf.Write(t.Value)
		case *ast.CodeSpan:
			// A code span's text, where a backslash is a backslash.
			buf.WriteString(t.Value.Value(source))
		case *ast.Text:
			// Markdown, read the way goldmark reads it.
			buf.WriteString(r.plainText(t.Value.Bytes(source)))
		default:
			r.writeAltText(buf, c, source)
		}
	}
}

// plainText reads Markdown text the way goldmark does -- backslash escapes
// dropped, entity and numeric references resolved -- and returns the text
// itself rather than HTML. Borrowing goldmark's decoder keeps the edge cases
// its own: "\&amp;" is the five characters "&amp;", not "&".
func (r *ConfluenceImageRenderer) plainText(markdown []byte) string {
	return string(ctransformer.DecodeMarkdown(markdown))
}
