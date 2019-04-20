package mark

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/russross/blackfriday"
)

type ConfluenceRenderer struct {
	blackfriday.Renderer
}

func (renderer ConfluenceRenderer) BlockCode(
	out *bytes.Buffer,
	text []byte,
	lang string,
) {
	out.WriteString(MacroCode{lang, text}.Render())
}

type MacroCode struct {
	lang string
	code []byte
}

func (code MacroCode) Render() string {
	return fmt.Sprintf(
		`<ac:structured-macro ac:name="code">`+
			`<ac:parameter ac:name="language">%s</ac:parameter>`+
			`<ac:parameter ac:name="collapse">false</ac:parameter>`+
			`<ac:plain-text-body><![CDATA[%s]]></ac:plain-text-body>`+
			`</ac:structured-macro>`,
		code.lang, code.code,
	)
}

// compileMarkdown will replace tags like <ac:rich-tech-body> with escaped
// equivalent, because blackfriday markdown parser replaces that tags with
// <a href="ac:rich-text-body">ac:rich-text-body</a> for whatever reason.
func CompileMarkdown(
	markdown []byte,
) []byte {
	log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	colon := regexp.MustCompile(`---BLACKFRIDAY-COLON---`)

	tags := regexp.MustCompile(`<(/?\S+):(\S+)>`)

	markdown = tags.ReplaceAll(
		markdown,
		[]byte(`<$1`+colon.String()+`$2>`),
	)

	renderer := ConfluenceRenderer{
		blackfriday.HtmlRenderer(
			blackfriday.HTML_USE_XHTML|
				blackfriday.HTML_USE_SMARTYPANTS|
				blackfriday.HTML_SMARTYPANTS_FRACTIONS|
				blackfriday.HTML_SMARTYPANTS_DASHES|
				blackfriday.HTML_SMARTYPANTS_LATEX_DASHES,
			"", "",
		),
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

	return html
}
