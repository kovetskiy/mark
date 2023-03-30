package mark

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/pkg/mark/stdlib"
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
	Stdlib *stdlib.Lib

	LevelMap BlockQuoteLevelMap
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceRenderer(stdlib *stdlib.Lib, opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceRenderer{
		Config:   html.NewConfig(),
		Stdlib:   stdlib,
		LevelMap: nil,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// blocks
	// reg.Register(ast.KindDocument, r.renderNode)
	// reg.Register(ast.KindHeading, r.renderNode)
	reg.Register(ast.KindBlockquote, r.renderBlockQuote)
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
	// reg.Register(ast.KindHTMLBlock, r.renderNode)
	// reg.Register(ast.KindList, r.renderNode)
	// reg.Register(ast.KindListItem, r.renderNode)
	// reg.Register(ast.KindParagraph, r.renderNode)
	// reg.Register(ast.KindTextBlock, r.renderNode)
	// reg.Register(ast.KindThematicBreak, r.renderNode)

	// inlines
	// reg.Register(ast.KindAutoLink, r.renderNode)
	// reg.Register(ast.KindCodeSpan, r.renderNode)
	// reg.Register(ast.KindEmphasis, r.renderNode)
	// reg.Register(ast.KindImage, r.renderNode)
	reg.Register(ast.KindLink, r.renderLink)
	// reg.Register(ast.KindRawHTML, r.renderNode)
	// reg.Register(ast.KindText, r.renderNode)
	// reg.Register(ast.KindString, r.renderNode)
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

			return ast.WalkSkipChildren, nil
		}
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

// compileMarkdown will replace tags like <ac:rich-tech-body> with escaped
// equivalent, because goldmark markdown parser replaces that tags with
// <a href="ac:rich-text-body">ac:rich-text-body</a> because of the autolink
// rule.
func CompileMarkdown(markdown []byte, stdlib *stdlib.Lib) string {
	log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	colon := []byte("---bf-COLON---")

	tags := regexp.MustCompile(`</?ac:[^>]+>`)

	for _, sm := range tags.FindAll(markdown, -1) {
		// Replace the colon in all "<ac:*>" tags with the colon bytes to avoid having Goldmark escape the HTML output.
		markdown = bytes.ReplaceAll(markdown, sm, bytes.ReplaceAll(sm, []byte(":"), colon))
	}

	converter := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.DefinitionList,
			extension.Typographer,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithXHTML(),
			html.WithUnsafe(),
		))

	converter.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(NewConfluenceRenderer(stdlib), 100),
	))

	var buf bytes.Buffer
	err := converter.Convert(markdown, &buf)

	if err != nil {
		panic(err)
	}

	// Restore all the colons we previously replaced.
	html := bytes.ReplaceAll(buf.Bytes(), colon, []byte(":"))

	log.Tracef(nil, "rendered markdown to html:\n%s", string(html))

	return string(html)
}

// DropDocumentLeadingH1 will drop leading H1 headings to prevent
// duplication of or visual conflict with page titles.
// NOTE: This is intended only to operate on the whole markdown document.
// Operating on individual lines will clear them if the begin with `#`.
func DropDocumentLeadingH1(
	markdown []byte,
) []byte {
	h1 := regexp.MustCompile(`^#[^#].*\n`)
	markdown = h1.ReplaceAll(markdown, []byte(""))
	return markdown
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
