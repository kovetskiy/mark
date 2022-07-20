package mark

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	bf "github.com/kovetskiy/blackfriday/v2"
	"github.com/kovetskiy/mark/pkg/mark/stdlib"
	"github.com/reconquest/pkg/log"
	"io"
	"regexp"
	"strings"
)

type ConfluenceRenderer struct {
	bf.Renderer

	Stdlib   *stdlib.Lib
	Attaches []Attachment
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
	entering bool) bf.WalkStatus {
	if node.Type == bf.CodeBlock {
		lang := string(node.Info)
		language := ParseLanguage(lang)
		embedded := false

		if language == MERMAID_LANG {
			checkSum := codeChecksum(node.Literal)
			if renderer.Attaches != nil {
				for _, attach := range renderer.Attaches {
					if attach.Checksum == checkSum {
						title := ""
						if !strings.HasPrefix(attach.Filename, attach.Checksum) {
							title = attach.Filename
						}
						_ = renderer.Stdlib.Templates.ExecuteTemplate(
							writer,
							"html:img",
							struct {
								Title  string
								URL    string
								Width  string
								Height string
							}{
								title,
								parseAttachmentLink(attach.Link),
								attach.Width,
								attach.Height,
							},
						)
						embedded = true
						language = "plaintext"
						break
					}
				}
			}
		}

		_ = renderer.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:code",
			struct {
				Language string
				Collapse bool
				Title    string
				Text     string
			}{
				language,
				embedded || strings.Contains(lang, "collapse"),
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
// <a href="ac:rich-text-body">ac:rich-text-body</a> because of the autolink
// rule.
func CompileMarkdown(
	markdown []byte,
	stdlib *stdlib.Lib,
	attaches []Attachment,
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
		Attaches: attaches,
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
	h1 := regexp.MustCompile(`^#[^#]\s*(.*)\s*\n`)
	groups := h1.FindSubmatch(markdown)
	if groups == nil {
		return ""
	} else {
		return string(groups[1])
	}
}

func codeChecksum(code []byte) string {
	codeBytes := bytes.TrimSuffix(code, []byte("\n"))
	checkSumBytes := sha1.Sum(codeBytes)
	return base64.URLEncoding.EncodeToString(checkSumBytes[:])
}
