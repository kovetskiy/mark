package renderer

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/d2"
	"github.com/kovetskiy/mark/v16/mermaid"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceFencedCodeBlockRenderer struct {
	html.Config
	Stdlib      *stdlib.Lib
	MarkConfig  types.MarkConfig
	Attachments attachment.Attacher
}

var reBlockDetails = regexp.MustCompile(
	// (<Lang>|-) (collapse|<theme>|\d)* (title <title>)?
	//
	// The language class is [\w#+./-] rather than \w: real language names carry
	// non-word characters (c#, c++, objective-c, html/xml, .NET), and \w stopped
	// at the first of them. The remainder was not rejected -- it fell through to
	// the options group and then into the theme catch-all, so "```c#" compiled to
	// language "c" plus a bogus theme "#". The \b after the options group is
	// dropped too: it required a word character to follow, so it split "c#" even
	// once the class accepted "#".
	`^(?:([\w#+./-]*)|-)\s*(\S.*?\S?)??\s*(?:\btitle\s+(\S.*\S?))?$`,
)

// reAttributeBlock matches the "{...}" attribute list Pandoc and Material for
// MkDocs write in an info string -- "``` { .js hl_lines="1 2" }" or
// "```python {.line-numbers}".
//
// None of what it holds is mark's option vocabulary, and left in place every
// word of it fell through to the theme catch-all: the two examples above
// published a Confluence code macro with a theme of "}" and of
// "{.line-numbers}" respectively, and the first also lost its language and had
// its line numbering turned on by the digits inside hl_lines.
var reAttributeBlock = regexp.MustCompile(`\{[^}]*\}`)

// reAttributeLang matches the ".language" class inside an attribute block,
// which is where the language is named when the block is the whole info
// string.
var reAttributeLang = regexp.MustCompile(`\.([\w#+./-]+)`)

// reAssignedTitle matches a title written with "=" rather than a space.
//
// Material for MkDocs' title="config.yml" is the commonest way to caption a
// code block in a docs-as-code repository, and mark accepted only its own
// "title config.yml": the caption was dropped and the whole token was
// published as the macro's theme.
var reAssignedTitle = regexp.MustCompile(`\btitle\s*=\s*(?:"([^"]*)"|'([^']*)'|(\S+))`)

// parseBlockDetails reads a fence info string into the parts the ac:code macro
// is given.
func parseBlockDetails(info string) (lang string, options []string, title string) {
	info = strings.TrimSpace(info)

	if groups := reAssignedTitle.FindStringSubmatch(info); groups != nil {
		for _, group := range groups[1:] {
			if group != "" {
				title = group
				break
			}
		}
		info = strings.TrimSpace(reAssignedTitle.ReplaceAllString(info, " "))
	}

	if block := reAttributeBlock.FindString(info); block != "" {
		// Only a leading block names the language: Pandoc writes the whole
		// info string as one attribute list, while MkDocs appends the list
		// after a language it has already given in the ordinary place.
		if strings.HasPrefix(info, "{") {
			if class := reAttributeLang.FindStringSubmatch(block); class != nil {
				lang = class[1]
			}
		}
		info = strings.TrimSpace(strings.Replace(info, block, " ", 1))
	}

	groups := reBlockDetails.FindStringSubmatch(info)
	if len(groups) == 0 {
		return lang, nil, title
	}

	if lang == "" {
		lang = groups[1]
	}
	if title == "" {
		title = groups[3]
	}

	return lang, strings.Fields(groups[2]), title
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceFencedCodeBlockRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher, cfg types.MarkConfig, opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceFencedCodeBlockRenderer{
		Config:      html.NewConfig(),
		Stdlib:      stdlib,
		MarkConfig:  cfg,
		Attachments: attachments,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceFencedCodeBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
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
			return strings.TrimSpace(lang[start:])
		}
	}
	return ""
}

// renderFencedCodeBlock renders a FencedCodeBlock
func (r *ConfluenceFencedCodeBlockRenderer) renderFencedCodeBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var info []byte
	nodeFencedCodeBlock := node.(*ast.FencedCodeBlock)
	if nodeFencedCodeBlock.Info != nil {
		segment := nodeFencedCodeBlock.Info.Segment
		info = segment.Value(source)
	}
	linenumbers := false
	firstline := 0
	theme := ""
	collapse := false

	lang, options, title := parseBlockDetails(string(info))
	for _, option := range options {
		if option == "collapse" {
			collapse = true
			continue
		}
		if option == "nocollapse" {
			collapse = false
			continue
		}
		if option == "linenumbers" {
			linenumbers = true
			continue
		}

		// strconv.Atoi rather than fmt.Sscanf("%d"): Sscanf stops at the first
		// byte it cannot use and reports success for what it read, so an
		// option that merely begins with a digit -- "1;" or the `2"` left by a
		// split hl_lines="1 2" -- silently turned line numbering on and moved
		// the first line. Only an option that is a number all through is one.
		if i, err := strconv.Atoi(option); err == nil {
			linenumbers = i > 0
			firstline = i
			continue
		}
		theme = option
	}

	var lval []byte

	lines := node.Lines().Len()
	for i := 0; i < lines; i++ {
		line := node.Lines().At(i)
		lval = append(lval, line.Value(source)...)
	}

	if lang == "d2" && slices.Contains(r.MarkConfig.Features, "d2") {
		attachment, err := d2.ProcessD2(title, lval, r.MarkConfig.D2Scale)
		if err != nil {
			line, col := GetLineCol(source, node.Pos())
			return ast.WalkStop, fmt.Errorf("line %d, col %d: d2 rendering failed: %w", line, col, err)
		}
		r.Attachments.Attach(attachment)

		effectiveAlign := calculateAlign(r.MarkConfig.ImageAlign, attachment.Width)
		effectiveLayout := calculateLayout(effectiveAlign, attachment.Width)
		displayWidth := calculateDisplayWidth(attachment.Width, effectiveLayout)

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Align          string
				Layout         string
				OriginalWidth  string
				OriginalHeight string
				Width          string
				Height         string
				Title          string
				Alt            string
				Attachment     string
				Url            string
			}{
				effectiveAlign,
				effectiveLayout,
				attachment.Width,
				attachment.Height,
				displayWidth,
				"",
				// The display title, not the attachment name: when the author gave
				// no "title" in the info string, ProcessD2/ProcessMermaid fall back
				// to the content checksum for the filename, and passing that here
				// made Confluence render a 64-character hash as a caption under the
				// diagram. The attachment keeps its checksum-derived name.
				title,
				"",
				attachment.Filename,
				"",
			},
		)

		if err != nil {
			return ast.WalkStop, err
		}

	} else if lang == "mermaid" && slices.Contains(r.MarkConfig.Features, "mermaid") {
		attachment, err := mermaid.ProcessMermaidLocally(title, lval, r.MarkConfig.MermaidScale)
		if err != nil {
			line, col := GetLineCol(source, node.Pos())
			return ast.WalkStop, fmt.Errorf("line %d, col %d: mermaid rendering failed: %w", line, col, err)
		}
		r.Attachments.Attach(attachment)

		effectiveAlign := calculateAlign(r.MarkConfig.ImageAlign, attachment.Width)
		effectiveLayout := calculateLayout(effectiveAlign, attachment.Width)
		displayWidth := calculateDisplayWidth(attachment.Width, effectiveLayout)

		err = r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:image",
			struct {
				Align          string
				Layout         string
				OriginalWidth  string
				OriginalHeight string
				Width          string
				Height         string
				Title          string
				Alt            string
				Attachment     string
				Url            string
			}{
				effectiveAlign,
				effectiveLayout,
				attachment.Width,
				attachment.Height,
				displayWidth,
				"",
				// The display title, not the attachment name: when the author gave
				// no "title" in the info string, ProcessD2/ProcessMermaid fall back
				// to the content checksum for the filename, and passing that here
				// made Confluence render a 64-character hash as a caption under the
				// diagram. The attachment keeps its checksum-derived name.
				title,
				"",
				attachment.Filename,
				"",
			},
		)

		if err != nil {
			return ast.WalkStop, err
		}

	} else if lang == "plantuml" && slices.Contains(r.MarkConfig.Features, "plantuml") {
		err := r.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:plantuml",
			struct {
				Text string
			}{
				strings.TrimSuffix(string(lval), "\n"),
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
