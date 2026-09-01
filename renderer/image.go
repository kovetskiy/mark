package renderer

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/vfs"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
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
	html.Config
	Stdlib      *stdlib.Lib
	Path        string
	Attachments attachment.Attacher
	ImageAlign  string
}

// NewConfluenceImageRenderer creates a new instance of the ConfluenceImageRenderer
func NewConfluenceImageRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, path string, imageAlign string, opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceImageRenderer{
		Config:      html.NewConfig(),
		Stdlib:      stdlib,
		Path:        path,
		Attachments: attachments,
		ImageAlign:  imageAlign,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceImageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindImage, r.renderImage)
}

// renderImage renders an inline image
func (r *ConfluenceImageRenderer) renderImage(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)

	if !r.Unsafe && html.IsDangerousURL(n.Destination) {
		return ast.WalkContinue, nil
	}

	var explicitWidth, explicitHeight, explicitAlign string
	if attr, ok := n.Attribute([]byte("width")); ok {
		if b, ok := attr.([]byte); ok {
			explicitWidth = string(b)
		}
	}
	if attr, ok := n.Attribute([]byte("height")); ok {
		if b, ok := attr.([]byte); ok {
			explicitHeight = string(b)
		}
	}
	if attr, ok := n.Attribute([]byte("align")); ok {
		if b, ok := attr.([]byte); ok {
			explicitAlign = string(b)
		}
	}

	align := r.ImageAlign
	if explicitAlign != "" {
		align = explicitAlign
	}

	attachments, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(r.Path), []string{string(n.Destination)})

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
				string(n.Title),
				string(nodeToHTMLText(n, source)),
				"",
				string(n.Destination),
			},
		)
	} else {
		if len(attachments) == 0 {
			line, col := GetLineCol(source, node.Pos())
			return ast.WalkStop, fmt.Errorf("line %d, col %d: no attachment resolved for %q", line, col, string(n.Destination))
		}

		r.Attachments.Attach(attachments[0])

		effectiveWidth := resolveWidth(explicitWidth, attachments[0].Width)
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
				attachments[0].Width,
				attachments[0].Height,
				displayWidth,
				explicitHeight,
				string(n.Title),
				string(nodeToHTMLText(n, source)),
				attachments[0].Filename,
				"",
			},
		)
	}

	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkSkipChildren, nil
}

// https://github.com/yuin/goldmark/blob/c446c414ef3a41fb562da0ae5badd18f1502c42f/renderer/html/html.go
func nodeToHTMLText(n ast.Node, source []byte) []byte {
	var buf bytes.Buffer
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if s, ok := c.(*ast.String); ok {
			// The <img> transformer hands the alt text over as a plain String,
			// and only the IsCode branch existed, so every alt written as an
			// HTML attribute was silently dropped while the Markdown spelling
			// kept its own.
			//
			// Written out unescaped, like the Text branch below and for the
			// same reason: the ac:image template escapes what it interpolates,
			// and doing it here as well puts a literal &amp;amp; on the page.
			buf.Write(s.Value)
		} else if t, ok := c.(*ast.Text); ok {
			// Not escaped here: every consumer interpolates the result into an
			// attribute through a template that escapes, and escaping twice
			// puts a literal &amp;amp; on the page.
			buf.Write(t.Value(source))
		} else {
			buf.Write(nodeToHTMLText(c, source))
		}
	}
	return buf.Bytes()
}
