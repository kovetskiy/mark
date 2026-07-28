package transformer

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTMLImgTransformer walks the AST and transforms HTML <img> tags found in
// ast.KindHTMLBlock and ast.KindRawHTML nodes into ast.KindImage nodes with
// attributes (width, height, alt, title, align) so that the image renderer
// can render them as Confluence <ac:image> macros uniformly.
type HTMLImgTransformer struct{}

// NewHTMLImgTransformer creates a new instance of HTMLImgTransformer.
func NewHTMLImgTransformer() *HTMLImgTransformer {
	return &HTMLImgTransformer{}
}

// Transform implements the parser.ASTTransformer interface.
func (t *HTMLImgTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	// Collect first, mutate after. Replacing a node mid-walk clears its
	// NextSibling, and ast.Walk is iterating over exactly that pointer, so it
	// would abandon every remaining sibling under the same parent: only the first
	// <img> per paragraph was converted and the rest were left as raw <img>,
	// which Confluence storage format has no element for.
	var blocks []*ast.HTMLBlock
	var inlines []*ast.RawHTML

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.HTMLBlock:
			blocks = append(blocks, n)
		case *ast.RawHTML:
			inlines = append(inlines, n)
		}

		return ast.WalkContinue, nil
	})

	for _, n := range blocks {
		t.transformHTMLBlock(n, reader)
	}
	for _, n := range inlines {
		t.transformRawHTML(n, reader)
	}
}

func (t *HTMLImgTransformer) transformHTMLBlock(n *ast.HTMLBlock, reader text.Reader) {
	var buf bytes.Buffer
	l := n.Lines().Len()
	for i := 0; i < l; i++ {
		line := n.Lines().At(i)
		buf.Write(line.Value(reader.Source()))
	}
	rawBytes := buf.Bytes()

	imgNodes := t.parseHTMLImages(rawBytes)
	if len(imgNodes) == 0 {
		return
	}

	parent := n.Parent()
	if parent == nil {
		return
	}

	// Replacing the block with only its images discards everything else it
	// contained. The common "centered image with caption" idiom
	//
	//	<div align="center"><img src="..."><b>caption</b></div>
	//
	// lost the div and the caption entirely, with no warning. Only take over the
	// block when the images are all it holds; otherwise leave it to the raw-HTML
	// path, which preserves the surrounding markup.
	if !onlyImages(rawBytes) {
		return
	}

	p := ast.NewParagraph()
	for _, imgNode := range imgNodes {
		p.AppendChild(p, imgNode)
	}

	parent.ReplaceChild(parent, n, p)
}

// onlyImages reports whether raw consists of nothing but <img> tags and
// whitespace, i.e. whether replacing it with just its images is lossless.
func onlyImages(raw []byte) bool {
	z := html.NewTokenizer(bytes.NewReader(raw))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return true
		case html.TextToken:
			if len(bytes.TrimSpace(z.Text())) != 0 {
				return false
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			if name, _ := z.TagName(); !bytes.Equal(name, []byte("img")) {
				return false
			}
		case html.EndTagToken:
			// A stray </img> is harmless; any other closing tag means the block
			// wraps content we would drop.
			if name, _ := z.TagName(); !bytes.Equal(name, []byte("img")) {
				return false
			}
		default:
			return false
		}
	}
}

func (t *HTMLImgTransformer) transformRawHTML(n *ast.RawHTML, reader text.Reader) {
	var buf bytes.Buffer
	l := n.Segments.Len()
	for i := 0; i < l; i++ {
		segment := n.Segments.At(i)
		buf.Write(segment.Value(reader.Source()))
	}
	rawBytes := buf.Bytes()

	imgNodes := t.parseHTMLImages(rawBytes)
	if len(imgNodes) == 0 {
		return
	}

	parent := n.Parent()
	if parent == nil {
		return
	}

	// Replace RawHTML node with converted image AST nodes
	for _, imgNode := range imgNodes {
		parent.InsertBefore(parent, n, imgNode)
	}
	parent.RemoveChild(parent, n)
}

func (t *HTMLImgTransformer) parseHTMLImages(rawBytes []byte) []*ast.Image {
	if !bytes.Contains(bytes.ToLower(rawBytes), []byte("<img")) {
		return nil
	}

	nodes, err := html.ParseFragment(bytes.NewReader(rawBytes), &html.Node{
		Type:     html.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err != nil || len(nodes) == 0 {
		return nil
	}

	var imgElements []*html.Node
	var findImg func(*html.Node)
	findImg = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "img") {
			imgElements = append(imgElements, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findImg(c)
		}
	}

	for _, n := range nodes {
		findImg(n)
	}

	if len(imgElements) == 0 {
		return nil
	}

	var astImages []*ast.Image
	for _, elem := range imgElements {
		var src, width, height, alt, title, align, style string
		for _, attr := range elem.Attr {
			switch strings.ToLower(attr.Key) {
			case "src":
				src = attr.Val
			case "width":
				width = attr.Val
			case "height":
				height = attr.Val
			case "alt":
				alt = attr.Val
			case "title":
				title = attr.Val
			case "align":
				align = attr.Val
			case "style":
				style = attr.Val
			}
		}

		if style != "" {
			sWidth, sHeight, sAlign := parseStyleAttr(style)
			if width == "" {
				width = sWidth
			}
			if height == "" {
				height = sHeight
			}
			if align == "" {
				align = sAlign
			}
		}

		width = strings.TrimSuffix(strings.TrimSpace(width), "px")
		height = strings.TrimSuffix(strings.TrimSpace(height), "px")

		if src == "" {
			continue
		}

		imgNode := ast.NewImage(ast.NewLink())
		imgNode.Destination = []byte(src)

		if title != "" {
			imgNode.Title = []byte(title)
		}
		if width != "" {
			imgNode.SetAttribute([]byte("width"), []byte(width))
		}
		if height != "" {
			imgNode.SetAttribute([]byte("height"), []byte(height))
		}
		if align != "" {
			imgNode.SetAttribute([]byte("align"), []byte(align))
		}
		if alt != "" {
			imgNode.AppendChild(imgNode, ast.NewString([]byte(alt)))
		}

		astImages = append(astImages, imgNode)
	}

	return astImages
}

func parseStyleAttr(styleStr string) (width, height, align string) {
	for _, declaration := range strings.Split(styleStr, ";") {
		parts := strings.SplitN(declaration, ":", 2)
		if len(parts) != 2 {
			continue
		}
		prop := strings.TrimSpace(strings.ToLower(parts[0]))
		val := strings.TrimSpace(strings.ToLower(parts[1]))
		val = strings.TrimSuffix(val, "px")

		switch prop {
		case "width", "max-width":
			if width == "" {
				width = val
			}
		case "height", "max-height":
			if height == "" {
				height = val
			}
		case "float":
			if val == "left" || val == "right" {
				align = val
			}
		}
	}
	return width, height, align
}
