package mark

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	bf "github.com/kovetskiy/blackfriday/v2"
	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/reconquest/pkg/log"
)

var reBlockDetails = regexp.MustCompile(
	// (<Lang>|-) (collapse|<theme>|\d)* (title <title>)?

	`^(?:(\w*)|-)\s*\b(\S.*?\S?)??\s*(?:\btitle\s+(\S.*\S?))?$`,
)

type BlockQuoteLevelMap map[*bf.Node]int

func (m BlockQuoteLevelMap) Level(node *bf.Node) int {
	return m[node]
}

type ConfluenceRenderer struct {
	bf.Renderer

	Stdlib *stdlib.Lib

	LevelMap BlockQuoteLevelMap
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

func ClasifyingBlockQuote(literal string) BlockQuoteType {
	infoPattern := regexp.MustCompile(`info|Info|INFO`)
	notePattern := regexp.MustCompile(`note|Note|NOTE`)
	warnPattern := regexp.MustCompile(`warn|Warn|WARN`)

	var t BlockQuoteType = None
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

func ParseBlockQuoteType(node *bf.Node) BlockQuoteType {
	var t BlockQuoteType = None

	countParagraphs := 0
	node.Walk(func(node *bf.Node, entering bool) bf.WalkStatus {

		if node.Type == bf.Paragraph && entering {
			countParagraphs += 1
		}
		// Type of block quote should be defined on the first blockquote line
		if node.Type == bf.Text && countParagraphs < 2 {
			t = ClasifyingBlockQuote(string(node.Literal))
		} else if countParagraphs > 1 {
			return bf.Terminate
		}
		return bf.GoToNext
	})
	return t
}

func GenerateBlockQuoteLevel(someNode *bf.Node) BlockQuoteLevelMap {

	// We define state variable that track BlockQuote level while we walk the tree
	blockQuoteLevel := 0
	blockQuoteLevelMap := make(map[*bf.Node]int)

	rootNode := someNode
	for rootNode.Parent != nil {
		rootNode = rootNode.Parent
	}
	rootNode.Walk(func(node *bf.Node, entering bool) bf.WalkStatus {
		if node.Type == bf.BlockQuote && entering {
			blockQuoteLevelMap[node] = blockQuoteLevel
			blockQuoteLevel += 1
		}
		if node.Type == bf.BlockQuote && !entering {
			blockQuoteLevel -= 1
		}
		return bf.GoToNext
	})
	return blockQuoteLevelMap
}

func (renderer ConfluenceRenderer) RenderNode(
	writer io.Writer,
	node *bf.Node,
	entering bool,
) bf.WalkStatus {
	// Initialize BlockQuote level map
	if renderer.LevelMap == nil {
		renderer.LevelMap = GenerateBlockQuoteLevel(node)
	}

	if node.Type == bf.CodeBlock {

		groups := reBlockDetails.FindStringSubmatch(string(node.Info))
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
		err := renderer.Stdlib.Templates.ExecuteTemplate(
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
				strings.TrimSuffix(string(node.Literal), "\n"),
			},
		)
		if err != nil {
			panic(err)
		}

		return bf.GoToNext
	}
	if node.Type == bf.Link && string(node.Destination[0:3]) == "ac:" {
		if entering {
			_, err := writer.Write([]byte("<ac:link><ri:page ri:content-title=\""))
			if err != nil {
				panic(err)
			}

			if len(node.Destination) < 4 {
				_, err := writer.Write(node.FirstChild.Literal)
				if err != nil {
					panic(err)
				}
			} else {
				_, err := writer.Write(node.Destination[3:])
				if err != nil {
					panic(err)
				}

			}
			_, err = writer.Write([]byte("\"/><ac:plain-text-link-body><![CDATA["))
			if err != nil {
				panic(err)
			}

			_, err = writer.Write(node.FirstChild.Literal)
			if err != nil {
				panic(err)
			}

			_, err = writer.Write([]byte("]]></ac:plain-text-link-body></ac:link>"))
			if err != nil {
				panic(err)
			}

			return bf.SkipChildren
		}
		return bf.GoToNext
	}
	if node.Type == bf.BlockQuote {
		quoteType := ParseBlockQuoteType(node)
		quoteLevel := renderer.LevelMap.Level(node)

		re := regexp.MustCompile(`[\n\t]`)

		if quoteLevel == 0 && entering && quoteType != None {
			if _, err := writer.Write([]byte(re.ReplaceAllString(fmt.Sprintf(`
			<ac:structured-macro ac:name="%s">
			<ac:parameter ac:name="icon">true</ac:parameter>
			<ac:rich-text-body>
			`, quoteType), ""))); err != nil {
				panic(err)
			}
			return bf.GoToNext
		}
		if quoteLevel == 0 && !entering && quoteType != None {
			if _, err := writer.Write([]byte(re.ReplaceAllString(`
			</ac:rich-text-body>
			</ac:structured-macro>
			`, ""))); err != nil {
				panic(err)
			}
			return bf.GoToNext
		}
	}
	return renderer.Renderer.RenderNode(writer, node, entering)
}

// compileMarkdown will replace tags like <ac:rich-tech-body> with escaped
// equivalent, because bf markdown parser replaces that tags with
// <a href="ac:rich-text-body">ac:rich-text-body</a> because of the autolink
// rule.
func CompileMarkdown(
	markdown []byte,
	stdlib *stdlib.Lib,
) string {
	log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	colon := regexp.MustCompile(`---bf-COLON---`)

	tags := regexp.MustCompile(`<(/?ac):(\S+?)>`)

	markdown = tags.ReplaceAll(
		markdown,
		[]byte(`<$1`+colon.String()+`$2>`),
	)

	renderer := ConfluenceRenderer{
		Renderer: bf.NewHTMLRenderer(
			bf.HTMLRendererParameters{
				Flags: bf.UseXHTML |
					bf.Smartypants |
					bf.SmartypantsFractions |
					bf.SmartypantsDashes |
					bf.SmartypantsLatexDashes,
			},
		),
		Stdlib:   stdlib,
		LevelMap: nil,
	}

	html := bf.Run(
		markdown,
		bf.WithRenderer(renderer),
		bf.WithExtensions(
			bf.NoIntraEmphasis|
				bf.Tables|
				bf.FencedCode|
				bf.Autolink|
				bf.LaxHTMLBlocks|
				bf.Strikethrough|
				bf.SpaceHeadings|
				bf.HeadingIDs|
				bf.AutoHeadingIDs|
				bf.Titleblock|
				bf.BackslashLineBreak|
				bf.DefinitionLists|
				bf.NoEmptyLineBeforeBlock|
				bf.Footnotes,
		),
	)

	html = colon.ReplaceAll(html, []byte(`:`))

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
