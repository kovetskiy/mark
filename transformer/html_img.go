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
// ast.KindHTMLBlock and ast.KindRawHTML nodes, and in the markup an earlier
// transformer left as replacement content, into ast.KindImage nodes with
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
	var replaced []*ast.Text

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.HTMLBlock:
			blocks = append(blocks, n)
		case *ast.RawHTML:
			inlines = append(inlines, n)
		case *ast.Text:
			// The <details> and layout transformers run first and leave the
			// block they rewrote as a Text node carrying the result. Whatever
			// <img> the author wrote inside it is still there, still raw.
			if _, ok := n.Attribute(replacementContent); ok {
				replaced = append(replaced, n)
			}
		}

		return ast.WalkContinue, nil
	})

	for _, n := range blocks {
		t.transformHTMLBlock(n, reader)
	}
	for _, n := range inlines {
		t.transformRawHTML(n, reader)
	}
	for _, n := range replaced {
		t.transformReplacedText(n)
	}
}

func (t *HTMLImgTransformer) transformHTMLBlock(n *ast.HTMLBlock, reader text.Reader) {
	parent := n.Parent()
	if parent == nil {
		return
	}

	// The closure line too: a block that ends on one, such as a comment or a
	// <pre>, holds its last line there rather than among the others.
	rawBytes := ExtractNodeRawContent(n, reader.Source())

	// A block holding nothing but images loses nothing by being replaced with
	// them, and renders as the paragraph of images a Markdown image would.
	if onlyImages(rawBytes) {
		imgNodes := t.parseHTMLImages(rawBytes)
		if len(imgNodes) == 0 {
			return
		}

		p := ast.NewParagraph()
		for _, imgNode := range imgNodes {
			p.AppendChild(p, imgNode)
		}

		parent.ReplaceChild(parent, n, p)

		return
	}

	// Anything else is the author's markup around the images -- above all the
	// README idiom
	//
	//	<p align="center">
	//	  <img src="logo.png" width="200">
	//	</p>
	//
	// An HTML block has no RawHTML children for the inline path to find, so the
	// <img> was published as written: a relative <img> Confluence does not
	// render, and a file that was never uploaded. Each tag is swapped for an
	// image node where it stands and the markup around it kept as written.
	pieces := t.splitImages(rawBytes)
	if pieces == nil {
		return
	}

	block := ast.NewTextBlock()
	for _, piece := range pieces {
		block.AppendChild(block, piece)
	}

	parent.ReplaceChild(parent, n, block)
}

// transformReplacedText converts the <img> tags inside markup an earlier
// transformer rewrote, the same way transformHTMLBlock does for a block.
func (t *HTMLImgTransformer) transformReplacedText(n *ast.Text) {
	parent := n.Parent()
	if parent == nil {
		return
	}

	existing, _ := n.Attribute(replacementContent)
	raw, ok := existing.([]byte)
	if !ok {
		return
	}

	pieces := t.splitImages(raw)
	if pieces == nil {
		return
	}

	for _, piece := range pieces {
		parent.InsertBefore(parent, n, piece)
	}
	parent.RemoveChild(parent, n)
}

// splitImages cuts raw at each <img> tag it can convert and returns the pieces
// in order: an image node for every such tag, and a Text node carrying the
// markup between them as replacement content, which the text renderer writes
// out as it is and the well-formedness pass still repairs. It returns nil when
// there is no tag to convert, and the caller then leaves raw alone.
//
// The markup is only ever cut at token boundaries, so every piece of it is
// still whole tags, comments and text. CDATA sections are copied through
// without looking inside: the tokenizer has no notion of them and would find
// tags in a code sample. An <img> inside a <picture> is the fallback for the
// <source> elements beside it, which have nothing to become, so that element
// is left as it was.
func (t *HTMLImgTransformer) splitImages(raw []byte) []ast.Node {
	if !bytes.Contains(bytes.ToLower(raw), []byte("<img")) {
		return nil
	}

	var pieces []ast.Node
	var markup []byte
	converted := false
	picture := 0

	flush := func() {
		if len(markup) == 0 {
			return
		}

		piece := ast.NewText()
		piece.SetAttribute(replacementContent, markup)
		pieces = append(pieces, piece)
		markup = nil
	}

	tokenize := func(span []byte) {
		z := html.NewTokenizer(bytes.NewReader(span))
		for {
			tokenType := z.Next()
			// Raw is only valid until the next call to Next. At the end it is
			// whatever an unterminated tag left unconsumed.
			token := z.Raw()
			if tokenType == html.ErrorToken {
				markup = append(markup, token...)
				return
			}

			name, _ := z.TagName()
			switch {
			case tokenType == html.StartTagToken && string(name) == "picture":
				picture++
			case tokenType == html.EndTagToken && string(name) == "picture" && picture > 0:
				picture--
			case (tokenType == html.StartTagToken || tokenType == html.SelfClosingTagToken) &&
				string(name) == "img" && picture == 0:
				// One tag is at most one image, and none without a src; that
				// one is left as written, as the other paths leave it.
				if images := t.parseHTMLImages(token); len(images) == 1 {
					flush()
					pieces = append(pieces, images[0])
					converted = true

					continue
				}
			}

			markup = append(markup, token...)
		}
	}

	rest := raw
	for len(rest) > 0 {
		start := bytes.Index(rest, cdataOpen)
		if start == -1 {
			tokenize(rest)

			break
		}

		tokenize(rest[:start])

		end := bytes.Index(rest[start:], cdataClose)
		if end == -1 {
			markup = append(markup, rest[start:]...)

			break
		}

		stop := start + end + len(cdataClose)
		markup = append(markup, rest[start:stop]...)
		rest = rest[stop:]
	}

	if !converted {
		return nil
	}

	flush()

	return pieces
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
		markPlain(imgNode)

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
