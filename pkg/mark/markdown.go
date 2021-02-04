package mark

import (
	"io"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/reconquest/pkg/log"
	bf "github.com/russross/blackfriday/v2"
)

type ConfluenceRenderer struct {
	bf.Renderer

	Stdlib *stdlib.Lib
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

func (renderer ConfluenceRenderer) RenderNode(
	writer io.Writer,
	node *bf.Node,
	entering bool,
) bf.WalkStatus {
	if node.Type == bf.CodeBlock {
		lang := string(node.Info)

		renderer.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:code",
			struct {
				Language string
				Collapse bool
				Title    string
				Text     string
			}{
				ParseLanguage(lang),
				strings.Contains(lang, "collapse"),
				ParseTitle(lang),
				strings.TrimSuffix(string(node.Literal), "\n"),
			},
		)

		return bf.GoToNext
	}
	return renderer.Renderer.RenderNode(writer, node, entering)
}

// compileMarkdown will replace tags like <ac:rich-tech-body> with escaped
// equivalent, because bf markdown parser replaces that tags with
// <a href="ac:rich-text-body">ac:rich-text-body</a> for whatever reason.
func CompileMarkdown(
	markdown []byte,
	stdlib *stdlib.Lib,
) string {
	log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	colon := regexp.MustCompile(`---bf-COLON---`)

	tags := regexp.MustCompile(`<(/?\S+?):(\S+?)>`)

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

		Stdlib: stdlib,
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
				bf.NoEmptyLineBeforeBlock,
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
