package renderer

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/attachment"
	"github.com/kovetskiy/mark/stdlib"
	"github.com/kovetskiy/mark/vfs"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// calculateAlign determines the appropriate ac:align value based on width
// Images >= 760px wide use "wide", otherwise use the configured alignment
func calculateAlign(configuredAlign string, width string) string {
	if configuredAlign == "" {
		return ""
	}
	
	if width == "" {
		return configuredAlign
	}
	
	// Parse width and check if >= 760
	widthInt, err := strconv.Atoi(width)
	if err != nil {
		return configuredAlign
	}
	
	if widthInt >= 760 {
		return "wide"
	}
	
	return configuredAlign
}

type ConfluenceImageRenderer struct {
	html.Config
	Stdlib      *stdlib.Lib
	Path        string
	Attachments attachment.Attacher
	ImageAlign  string
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
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

	attachments, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(r.Path), []string{string(n.Destination)})

	// We were unable to resolve it locally, treat as URL
	if err != nil {
		escapedURL := string(n.Destination)
		escapedURL = strings.ReplaceAll(escapedURL, "&", "&amp;")

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Align      string
				Width      string
				Height     string
				Title      string
				Alt        string
				Attachment string
				Url        string
			}{
				r.ImageAlign,
				"",
				"",
				string(n.Title),
				string(nodeToHTMLText(n, source)),
				"",
				escapedURL,
			},
		)
	} else {

		r.Attachments.Attach(attachments[0])

		effectiveAlign := calculateAlign(r.ImageAlign, attachments[0].Width)

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Align      string
				Width      string
				Height     string
				Title      string
				Alt        string
				Attachment string
				Url        string
			}{
				effectiveAlign,
				attachments[0].Width,
				attachments[0].Height,
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
		if s, ok := c.(*ast.String); ok && s.IsCode() {
			buf.Write(s.Value)
		} else if t, ok := c.(*ast.Text); ok {
			buf.Write(util.EscapeHTML(t.Value(source)))
		} else {
			buf.Write(nodeToHTMLText(c, source))
		}
	}
	return buf.Bytes()
}
