package markdown

import (
	"context"
	"slices"

	"github.com/kovetskiy/mark/v17/attachment"
	cparser "github.com/kovetskiy/mark/v17/parser"
	crenderer "github.com/kovetskiy/mark/v17/renderer"
	"github.com/kovetskiy/mark/v17/stdlib"
	ctransformer "github.com/kovetskiy/mark/v17/transformer"
	emoji "github.com/yuin/goldmark-emoji/v2"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// featureDeps is what a feature's renderer is built from.
type featureDeps struct {
	ctx         context.Context
	stdlib      *stdlib.Lib
	attachments attachment.Attacher
	opts        Options
}

// markdownFeature is the part of the goldmark assembly a --features name
// switches on: its parser options, its renderer, or both.
type markdownFeature struct {
	parserOptions func(cfg *parser.Config) []parser.Option
	renderer      func(deps featureDeps) html.Extension
}

// markdownFeatures maps each feature that touches goldmark to what it adds.
// Every renderer here names node kinds of its own, so the order they are
// registered in does not matter; each compile path lists its features in the
// order it has always registered them all the same, since goldmark sorts its
// parsers with an unstable sort and two of them share a trigger and a priority.
var markdownFeatures = map[string]markdownFeature{
	"emoji": {
		// The options of goldmark-emoji's parser extension, which registers
		// its parser at 999. An extension cannot hand goldmark another to run,
		// so they are added here directly. Nothing else in the set triggers on
		// ':'. Only the parser is taken from goldmark-emoji; its own renderer
		// is never registered.
		parserOptions: func(cfg *parser.Config) []parser.Option {
			return emoji.NewParser().ParserOptions(cfg)
		},
		renderer: func(deps featureDeps) html.Extension {
			return crenderer.NewConfluenceEmojiRenderer(deps.stdlib)
		},
	},
	"mkdocsadmonitions": {
		parserOptions: func(*parser.Config) []parser.Option {
			return []parser.Option{parser.WithBlockParsers(
				util.Prioritized(cparser.NewAdmonitionParser(), 100),
			)}
		},
		renderer: func(featureDeps) html.Extension {
			return crenderer.NewConfluenceMkDocsAdmonitionRenderer()
		},
	},
	"date": {
		parserOptions: func(*parser.Config) []parser.Option {
			return []parser.Option{parser.WithInlineParsers(
				util.Prioritized(cparser.NewDateParser(), 99),
			)}
		},
		renderer: func(featureDeps) html.Extension {
			return crenderer.NewConfluenceDateRenderer()
		},
	},
	"mention": {
		parserOptions: func(*parser.Config) []parser.Option {
			return []parser.Option{parser.WithInlineParsers(
				util.Prioritized(cparser.NewMentionParser(), 99),
			)}
		},
		renderer: func(deps featureDeps) html.Extension {
			return crenderer.NewConfluenceMentionRenderer(deps.stdlib)
		},
	},
	"math": {
		parserOptions: func(*parser.Config) []parser.Option {
			return []parser.Option{
				parser.WithBlockParsers(
					// Between goldmark's fenced code block (700) and its block
					// quote (800). Below the fence so that a "$$" shown inside
					// one stays a code sample, and above the paragraph (1000) it
					// would otherwise become.
					util.Prioritized(cparser.NewMathBlockParser(), 750),
				),
				parser.WithInlineParsers(
					// Ahead of goldmark's own inline parsers, so that a formula
					// holding markup characters -- and TeX is made of them -- is
					// taken as a formula rather than partly as emphasis or a
					// link.
					util.Prioritized(cparser.NewMathParser(), 99),
				),
			}
		},
		renderer: func(deps featureDeps) html.Extension {
			return crenderer.NewConfluenceMathRenderer(deps.ctx, deps.stdlib, deps.attachments, deps.opts.mathConfig())
		},
	},
	// Renders auto-detected bare URLs with the data-card-appearance="inline"
	// hint, prompting Confluence Cloud to display them as inline smart cards
	// (page mentions, Jira issues, GitHub references, etc.) instead of plain
	// hyperlinks. At the priority of the <details> transformer and registered
	// after it.
	"inline-link-card": {
		parserOptions: func(*parser.Config) []parser.Option {
			return []parser.Option{parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](ctransformer.NewAutoLinkTransformer(), 110),
			)}
		},
	},
}

// knownFeatures are the feature names mark understands: the goldmark ones in
// markdownFeatures, and those read where they take effect (d2, mermaid and
// plantuml by the fenced code renderer, frontmatter by the metadata reader).
var knownFeatures = []string{
	"d2",
	"date",
	"emoji",
	"frontmatter",
	"inline-link-card",
	"math",
	"mention",
	"mermaid",
	"mkdocsadmonitions",
	"plantuml",
}

// defaultFeatures are the features mark's command line enables when
// --features is not given.
var defaultFeatures = []string{"mermaid", "mention"}

// KnownFeatures returns the feature names mark understands, sorted. A name
// not on it is never switched on, and nothing is said about it.
func KnownFeatures() []string {
	return slices.Clone(knownFeatures)
}

// DefaultFeatures returns the features mark's command line enables when
// --features is not given.
func DefaultFeatures() []string {
	return slices.Clone(defaultFeatures)
}

// featureOrder is the features in markdownFeatures, in the order
// CompileMarkdown registers them.
var featureOrder = []string{"date", "emoji", "mkdocsadmonitions", "mention", "math", "inline-link-card"}

// featureParserOptions is the parser options of the features that enabled
// switches on.
func featureParserOptions(enabled []string, cfg *parser.Config) []parser.Option {
	var opts []parser.Option
	for _, name := range featureOrder {
		if f := markdownFeatures[name]; f.parserOptions != nil && slices.Contains(enabled, name) {
			opts = append(opts, f.parserOptions(cfg)...)
		}
	}

	return opts
}

// featureRenderers is the renderers of the features that enabled switches on.
func featureRenderers(enabled []string, deps featureDeps) []html.Extension {
	var renderers []html.Extension
	for _, name := range featureOrder {
		if f := markdownFeatures[name]; f.renderer != nil && slices.Contains(enabled, name) {
			renderers = append(renderers, f.renderer(deps))
		}
	}

	return renderers
}
