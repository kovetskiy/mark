package mark

import (
	"bytes"
	"regexp"

	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/reconquest/pkg/log"
	"github.com/russross/blackfriday"
)

type ConfluenceRenderer struct {
	blackfriday.Renderer

	Stdlib *stdlib.Lib
}

func (renderer ConfluenceRenderer) BlockCode(
	out *bytes.Buffer,
	text []byte,
	lang string,
) {
	renderer.Stdlib.Templates.ExecuteTemplate(
		out,
		"ac:code",
		struct {
			Language string
			Text     string
		}{
			lang,
			string(text),
		},
	)
}

// compileMarkdown will replace tags like <ac:rich-tech-body> with escaped
// equivalent, because blackfriday markdown parser replaces that tags with
// <a href="ac:rich-text-body">ac:rich-text-body</a> for whatever reason.
func CompileMarkdown(
	markdown []byte,
	stdlib *stdlib.Lib,
) string {
	log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	colon := regexp.MustCompile(`---BLACKFRIDAY-COLON---`)

	tags := regexp.MustCompile(`<(/?\S+?):(\S+?)>`)

	markdown = tags.ReplaceAll(
		markdown,
		[]byte(`<$1`+colon.String()+`$2>`),
	)

	renderer := ConfluenceRenderer{
		Renderer: blackfriday.HtmlRenderer(
			blackfriday.HTML_USE_XHTML|
				blackfriday.HTML_USE_SMARTYPANTS|
				blackfriday.HTML_SMARTYPANTS_FRACTIONS|
				blackfriday.HTML_SMARTYPANTS_DASHES|
				blackfriday.HTML_SMARTYPANTS_LATEX_DASHES,
			"", "",
		),

		Stdlib: stdlib,
	}

	html := blackfriday.MarkdownOptions(
		markdown,
		renderer,
		blackfriday.Options{
			Extensions: blackfriday.EXTENSION_NO_INTRA_EMPHASIS |
				blackfriday.EXTENSION_TABLES |
				blackfriday.EXTENSION_FENCED_CODE |
				blackfriday.EXTENSION_AUTOLINK |
				blackfriday.EXTENSION_LAX_HTML_BLOCKS |
				blackfriday.EXTENSION_STRIKETHROUGH |
				blackfriday.EXTENSION_SPACE_HEADERS |
				blackfriday.EXTENSION_HEADER_IDS |
				blackfriday.EXTENSION_AUTO_HEADER_IDS |
				blackfriday.EXTENSION_TITLEBLOCK |
				blackfriday.EXTENSION_BACKSLASH_LINE_BREAK |
				blackfriday.EXTENSION_DEFINITION_LISTS |
				blackfriday.EXTENSION_NO_EMPTY_LINE_BEFORE_BLOCK,
		},
	)

	html = colon.ReplaceAll(html, []byte(`:`))

	log.Tracef(nil, "rendered markdown to html:\n%s", string(html))

	return string(html)
}

// dropH1Markdown will drop leading H1 headings to prevent
// duplication of or conflict with page titles.
func DropH1Markdown(
	markdown []byte,
) []byte {
	h1 := regexp.MustCompile(`^#[^#].*\n`)
	markdown = h1.ReplaceAll(markdown, []byte(""))
	return markdown
}
