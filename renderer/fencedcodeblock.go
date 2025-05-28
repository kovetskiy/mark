package renderer

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/attachment"
	"github.com/kovetskiy/mark/d2"
	"github.com/kovetskiy/mark/mermaid"
	"github.com/kovetskiy/mark/stdlib"
	"github.com/kovetskiy/mark/types"
	"github.com/reconquest/pkg/log"

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

	`^(?:(\w*)|-)\s*\b(\S.*?\S?)??\s*(?:\btitle\s+(\S.*\S?))?$`,
)

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
			return lang[start:]
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

	if lang == "d2" && slices.Contains(r.MarkConfig.Features, "d2") {
		attachment, err := d2.ProcessD2(title, lval, r.MarkConfig.D2Scale)
		if err != nil {
			log.Debugf(nil, "error: %v", err)
			return ast.WalkStop, err
		}
		r.Attachments.Attach(attachment)
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

	} else if lang == "mermaid" && slices.Contains(r.MarkConfig.Features, "mermaid") && r.MarkConfig.MermaidProvider == "mermaid-go" {
		attachment, err := mermaid.ProcessMermaidLocally(title, lval, r.MarkConfig.MermaidScale)
		if err != nil {
			log.Debugf(nil, "error: %v", err)
			return ast.WalkStop, err
		}
		r.Attachments.Attach(attachment)
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
