package mark

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	cparser "github.com/kovetskiy/mark/pkg/mark/parser"
	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/kovetskiy/mark/pkg/mark/vfs"
	"github.com/reconquest/pkg/log"
	"github.com/yuin/goldmark"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

var reBlockDetails = regexp.MustCompile(
	// (<Lang>|-) (collapse|<theme>|\d)* (title <title>)?

	`^(?:(\w*)|-)\s*\b(\S.*?\S?)??\s*(?:\btitle\s+(\S.*\S?))?$`,
)

// Define BlockQuoteType enum
type BlockQuoteType int

const (
	Info BlockQuoteType = iota
	Note
	Warn
	None
)

func (t BlockQuoteType) String() string {
	return []string{"info", "note", "warn", "none"}[t]
}

type BlockQuoteLevelMap map[ast.Node]int

func (m BlockQuoteLevelMap) Level(node ast.Node) int {
	return m[node]
}

// Renderer renders anchor [Node]s.
type ConfluenceRenderer struct {
	html.Config
	Stdlib          *stdlib.Lib
	Path            string
	MermaidProvider string
	DropFirstH1     bool
	LevelMap        BlockQuoteLevelMap
	Attachments     []Attachment
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceRenderer(stdlib *stdlib.Lib, path string, mermaidProvider string, dropFirstH1 bool, opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceRenderer{
		Config:          html.NewConfig(),
		Stdlib:          stdlib,
		Path:            path,
		MermaidProvider: mermaidProvider,
		DropFirstH1:     dropFirstH1,
		LevelMap:        nil,
		Attachments:     []Attachment{},
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// blocks
	// reg.Register(ast.KindDocument, r.renderNode)
	reg.Register(ast.KindHeading, r.renderHeading)
	reg.Register(ast.KindBlockquote, r.renderBlockQuote)
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
	reg.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
	// reg.Register(ast.KindList, r.renderNode)
	// reg.Register(ast.KindListItem, r.renderNode)
	// reg.Register(ast.KindParagraph, r.renderNode)
	// reg.Register(ast.KindTextBlock, r.renderNode)
	// reg.Register(ast.KindThematicBreak, r.renderNode)

	// inlines
	// reg.Register(ast.KindAutoLink, r.renderNode)
	// reg.Register(ast.KindCodeSpan, r.renderNode)
	// reg.Register(ast.KindEmphasis, r.renderNode)
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindLink, r.renderLink)
	// reg.Register(ast.KindRawHTML, r.renderNode)
	// reg.Register(ast.KindText, r.renderNode)
	// reg.Register(ast.KindString, r.renderNode)
}

func (r *ConfluenceRenderer) renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)

	// If this is the first h1 heading of the document and we want to drop it, let's not render it at all.
	if n.Level == 1 && r.DropFirstH1 {
		if !entering {
			r.DropFirstH1 = false
		}
		return ast.WalkSkipChildren, nil
	}

	return r.goldmarkRenderHeading(w, source, node, entering)
}

func (r *ConfluenceRenderer) goldmarkRenderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)
	if entering {
		_, _ = w.WriteString("<h")
		_ = w.WriteByte("0123456"[n.Level])
		if n.Attributes() != nil {
			html.RenderAttributes(w, node, html.HeadingAttributeFilter)
		}
		_ = w.WriteByte('>')
	} else {
		_, _ = w.WriteString("</h")
		_ = w.WriteByte("0123456"[n.Level])
		_, _ = w.WriteString(">\n")
	}
	return ast.WalkContinue, nil
}

func ParseLanguage(lang string) string {
	// lang takes the following form: language? "collapse"? ("title"? <any string>*)?
	// let's split it by spaces
	paramlist := strings.Fields(lang)

	// get the word in question, aka the first one
	first := lang
	if len(paramlist) > 0 {
		first = paramlist[0]
	}

	if first == "collapse" || first == "title" {
		// collapsing or including a title without a language
		return ""
	}
	// the default case with language being the first one
	return first
}

func ParseTitle(lang string) string {
	index := strings.Index(lang, "title")
	if index >= 0 {
		// it's found, check if title is given and return it
		start := index + 6
		if len(lang) > start {
			return lang[start:]
		}
	}
	return ""
}

// ClassifyingBlockQuote compares a string against a set of patterns and returns a BlockQuoteType
func ClassifyingBlockQuote(literal string) BlockQuoteType {
	infoPattern := regexp.MustCompile(`info|Info|INFO`)
	notePattern := regexp.MustCompile(`note|Note|NOTE`)
	warnPattern := regexp.MustCompile(`warn|Warn|WARN`)

	var t = None
	switch {
	case infoPattern.MatchString(literal):
		t = Info
	case notePattern.MatchString(literal):
		t = Note
	case warnPattern.MatchString(literal):
		t = Warn
	}
	return t
}

// ParseBlockQuoteType parses the first line of a blockquote and returns its type
func ParseBlockQuoteType(node ast.Node, source []byte) BlockQuoteType {
	var t = None

	countParagraphs := 0
	_ = ast.Walk(node, func(node ast.Node, entering bool) (ast.WalkStatus, error) {

		if node.Kind() == ast.KindParagraph && entering {
			countParagraphs += 1
		}
		// Type of block quote should be defined on the first blockquote line
		if countParagraphs < 2 && entering {
			if node.Kind() == ast.KindText {
				n := node.(*ast.Text)
				t = ClassifyingBlockQuote(string(n.Text(source)))
				countParagraphs += 1
			}
			if node.Kind() == ast.KindHTMLBlock {

				n := node.(*ast.HTMLBlock)
				for i := 0; i < n.BaseBlock.Lines().Len(); i++ {
					line := n.BaseBlock.Lines().At(i)
					t = ClassifyingBlockQuote(string(line.Value(source)))
					if t != None {
						break
					}
				}
				countParagraphs += 1
			}
		} else if countParagraphs > 1 && entering {
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})

	return t
}

// GenerateBlockQuoteLevel walks a given node and returns a map of blockquote levels
func GenerateBlockQuoteLevel(someNode ast.Node) BlockQuoteLevelMap {

	// We define state variable that track BlockQuote level while we walk the tree
	blockQuoteLevel := 0
	blockQuoteLevelMap := make(map[ast.Node]int)

	rootNode := someNode
	for rootNode.Parent() != nil {
		rootNode = rootNode.Parent()
	}
	_ = ast.Walk(rootNode, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if node.Kind() == ast.KindBlockquote && entering {
			blockQuoteLevelMap[node] = blockQuoteLevel
			blockQuoteLevel += 1
		}
		if node.Kind() == ast.KindBlockquote && !entering {
			blockQuoteLevel -= 1
		}
		return ast.WalkContinue, nil
	})
	return blockQuoteLevelMap
}

// renderBlockQuote will render a BlockQuote
func (r *ConfluenceRenderer) renderBlockQuote(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	// Initialize BlockQuote level map
	if r.LevelMap == nil {
		r.LevelMap = GenerateBlockQuoteLevel(node)
	}

	quoteType := ParseBlockQuoteType(node, source)
	quoteLevel := r.LevelMap.Level(node)

	if quoteLevel == 0 && entering && quoteType != None {
		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", quoteType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	if quoteLevel == 0 && !entering && quoteType != None {
		suffix := "</ac:rich-text-body></ac:structured-macro>\n"
		if _, err := writer.Write([]byte(suffix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}
	return r.goldmarkRenderBlockquote(writer, source, node, entering)
}

// goldmarkRenderBlockquote is the default renderBlockquote implementation from https://github.com/yuin/goldmark/blob/9d6f314b99ca23037c93d76f248be7b37de6220a/renderer/html/html.go#L286
func (r *ConfluenceRenderer) goldmarkRenderBlockquote(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		if n.Attributes() != nil {
			_, _ = w.WriteString("<blockquote")
			html.RenderAttributes(w, n, html.BlockquoteAttributeFilter)
			_ = w.WriteByte('>')
		} else {
			_, _ = w.WriteString("<blockquote>\n")
		}
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}

// renderLink renders links specifically for confluence
func (r *ConfluenceRenderer) renderLink(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if string(node.(*ast.Link).Destination[0:3]) == "ac:" {
		if entering {
			_, err := writer.Write([]byte("<ac:link><ri:page ri:content-title=\""))
			if err != nil {
				return ast.WalkStop, err
			}

			if len(node.(*ast.Link).Destination) < 4 {
				_, err := writer.Write(node.FirstChild().Text(source))
				if err != nil {
					return ast.WalkStop, err
				}
			} else {
				_, err := writer.Write(node.(*ast.Link).Destination[3:])
				if err != nil {
					return ast.WalkStop, err
				}

			}
			_, err = writer.Write([]byte("\"/><ac:plain-text-link-body><![CDATA["))
			if err != nil {
				return ast.WalkStop, err
			}

			_, err = writer.Write(node.FirstChild().Text(source))
			if err != nil {
				return ast.WalkStop, err
			}

			_, err = writer.Write([]byte("]]></ac:plain-text-link-body></ac:link>"))
			if err != nil {
				return ast.WalkStop, err
			}
		}
		return ast.WalkSkipChildren, nil
	}
	return r.goldmarkRenderLink(writer, source, node, entering)
}

// goldmarkRenderLink is the default renderLink implementation from https://github.com/yuin/goldmark/blob/9d6f314b99ca23037c93d76f248be7b37de6220a/renderer/html/html.go#L552
func (r *ConfluenceRenderer) goldmarkRenderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if entering {
		_, _ = w.WriteString("<a href=\"")
		if r.Unsafe || !html.IsDangerousURL(n.Destination) {
			_, _ = w.Write(util.EscapeHTML(util.URLEscape(n.Destination, true)))
		}
		_ = w.WriteByte('"')
		if n.Title != nil {
			_, _ = w.WriteString(` title="`)
			r.Writer.Write(w, n.Title)
			_ = w.WriteByte('"')
		}
		if n.Attributes() != nil {
			html.RenderAttributes(w, n, html.LinkAttributeFilter)
		}
		_ = w.WriteByte('>')
	} else {
		_, _ = w.WriteString("</a>")
	}
	return ast.WalkContinue, nil
}

// renderFencedCodeBlock renders a FencedCodeBlock
func (r *ConfluenceRenderer) renderFencedCodeBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var info []byte
	nodeFencedCodeBlock := node.(*ast.FencedCodeBlock)
	if nodeFencedCodeBlock.Info != nil {
		segment := nodeFencedCodeBlock.Info.Segment
		info = segment.Value(source)
	}
	groups := reBlockDetails.FindStringSubmatch(string(info))
	linenumbers := false
	firstline := 0
	theme := ""
	collapse := false
	lang := ""
	var options []string
	title := ""
	if len(groups) > 0 {
		lang, options, title = groups[1], strings.Fields(groups[2]), groups[3]
		for _, option := range options {
			if option == "collapse" {
				collapse = true
				continue
			}
			if option == "nocollapse" {
				collapse = false
				continue
			}
			var i int
			if _, err := fmt.Sscanf(option, "%d", &i); err == nil {
				linenumbers = i > 0
				firstline = i
				continue
			}
			theme = option
		}

	}

	var lval []byte

	lines := node.Lines().Len()
	for i := 0; i < lines; i++ {
		line := node.Lines().At(i)
		lval = append(lval, line.Value(source)...)
	}

	if lang == "mermaid" && r.MermaidProvider == "mermaid-go" {
		attachment, err := processMermaidLocally(title, lval)
		if err != nil {
			return ast.WalkStop, err
		}
		r.Attachments = append(r.Attachments, attachment)
		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Width      string
				Height     string
				Title      string
				Alt        string
				Attachment string
				Url        string
			}{
				attachment.Width,
				attachment.Height,
				attachment.Name,
				"",
				attachment.Filename,
				"",
			},
		)

		if err != nil {
			return ast.WalkStop, err
		}

	} else {
		err := r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:code",
			struct {
				Language    string
				Collapse    bool
				Title       string
				Theme       string
				Linenumbers bool
				Firstline   int
				Text        string
			}{
				lang,
				collapse,
				title,
				theme,
				linenumbers,
				firstline,
				strings.TrimSuffix(string(lval), "\n"),
			},
		)

		if err != nil {
			return ast.WalkStop, err
		}
	}

	return ast.WalkContinue, nil
}

// renderCodeBlock renders a CodeBlock
func (r *ConfluenceRenderer) renderCodeBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	linenumbers := false
	firstline := 0
	theme := ""
	collapse := false
	lang := ""
	title := ""

	var lval []byte

	lines := node.Lines().Len()
	for i := 0; i < lines; i++ {
		line := node.Lines().At(i)
		lval = append(lval, line.Value(source)...)
	}
	err := r.Stdlib.Templates.ExecuteTemplate(
		writer,
		"ac:code",
		struct {
			Language    string
			Collapse    bool
			Title       string
			Theme       string
			Linenumbers bool
			Firstline   int
			Text        string
		}{
			lang,
			collapse,
			title,
			theme,
			linenumbers,
			firstline,
			strings.TrimSuffix(string(lval), "\n"),
		},
	)
	if err != nil {
		return ast.WalkStop, err
	}

	return ast.WalkContinue, nil
}

// renderImage renders an inline image
func (r *ConfluenceRenderer) renderImage(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)

	attachments, err := ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(r.Path), []string{string(n.Destination)})

	// We were unable to resolve it locally, treat as URL
	if err != nil {
		escapedURL := string(n.Destination)
		escapedURL = strings.ReplaceAll(escapedURL, "&", "&amp;")

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Width      string
				Height     string
				Title      string
				Alt        string
				Attachment string
				Url        string
			}{
				"",
				"",
				string(n.Title),
				string(nodeToHTMLText(n, source)),
				"",
				escapedURL,
			},
		)
	} else {

		r.Attachments = append(r.Attachments, attachments[0])

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Width      string
				Height     string
				Title      string
				Alt        string
				Attachment string
				Url        string
			}{
				"",
				"",
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

func (r *ConfluenceRenderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return r.goldmarkRenderHTMLBlock(w, source, node, entering)
	}

	n := node.(*ast.HTMLBlock)
	l := n.Lines().Len()
	for i := 0; i < l; i++ {
		line := n.Lines().At(i)

		switch strings.Trim(string(line.Value(source)), "\n") {
		case "<!-- ac:layout -->":
			_, _ = w.WriteString("<ac:layout>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout end -->":
			_, _ = w.WriteString("</ac:layout>\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:single -->":
			_, _ = w.WriteString("<ac:layout-section type=\"single\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_equal -->":
			_, _ = w.WriteString("<ac:layout-section type=\"two_equal\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_left_sidebar -->":
			_, _ = w.WriteString("<ac:layout-section type=\"two_left_sidebar\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:two_right_sidebar -->":
			_, _ = w.WriteString("<ac:layout-section type=\"two_right_sidebar\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:three -->":
			_, _ = w.WriteString("<ac:layout-section type=\"three\">\n")
			return ast.WalkContinue, nil
		case "<!-- ac:layout-section type:three_with_sidebars -->":
			_, _ = w.WriteString("<ac:layout-section type=\"three_with_sidebars\">\n")
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

		}
	}
	return r.goldmarkRenderHTMLBlock(w, source, node, entering)

}

func (r *ConfluenceRenderer) goldmarkRenderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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

func CompileMarkdown(markdown []byte, stdlib *stdlib.Lib, path string, mermaidProvider string, dropFirstH1 bool) (string, []Attachment) {
	log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	confluenceRenderer := NewConfluenceRenderer(stdlib, path, mermaidProvider, dropFirstH1)

	converter := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.DefinitionList,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithXHTML(),
			html.WithUnsafe(),
		))

	converter.Parser().AddOptions(parser.WithInlineParsers(
		// Must be registered with a higher priority than goldmark's linkParser to make sure goldmark doesn't parse
		// the <ac:*/> tags.
		util.Prioritized(cparser.NewConfluenceTagParser(), 199),
	))

	converter.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(confluenceRenderer, 100),
	))

	var buf bytes.Buffer
	err := converter.Convert(markdown, &buf)

	if err != nil {
		panic(err)
	}

	html := buf.Bytes()

	log.Tracef(nil, "rendered markdown to html:\n%s", string(html))

	return string(html), confluenceRenderer.(*ConfluenceRenderer).Attachments

}

// ExtractDocumentLeadingH1 will extract leading H1 heading
func ExtractDocumentLeadingH1(markdown []byte) string {
	h1 := regexp.MustCompile(`#[^#]\s*(.*)\s*\n`)
	groups := h1.FindSubmatch(markdown)
	if groups == nil {
		return ""
	} else {
		return string(groups[1])
	}
}

// https://github.com/yuin/goldmark/blob/c446c414ef3a41fb562da0ae5badd18f1502c42f/renderer/html/html.go
func nodeToHTMLText(n ast.Node, source []byte) []byte {
	var buf bytes.Buffer
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if s, ok := c.(*ast.String); ok && s.IsCode() {
			buf.Write(s.Text(source))
		} else if !c.HasChildren() {
			buf.Write(util.EscapeHTML(c.Text(source)))
		} else {
			buf.Write(nodeToHTMLText(c, source))
		}
	}
	return buf.Bytes()
}
