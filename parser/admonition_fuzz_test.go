package parser_test

import (
	"testing"

	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// FuzzAdmonitionParser parses arbitrary input with the admonition parser
// installed. The library it came from panicked on a numeric attribute and on
// an admonition inside a blockquote; neither showed up until someone wrote one.
func FuzzAdmonitionParser(f *testing.F) {
	for _, seed := range []string{
		"!!! note \"T\" {.a #b c=1 d=true e=[1]}\n    body\n!!!\n",
		"> !!! note \"x\"\n>     q\n",
		"- !!! tip\n      x\n\n  !!!! warning\n      y\n  !!!!\n",
		"!!! note\n\t!!! tip \"t\"\n\t\tx\n\t!!!\n!!!\n",
		"!!! a {\n    b\n}\n",
	} {
		f.Add(seed)
	}

	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithBlockParsers(util.Prioritized(cparser.NewAdmonitionParser(), 100)),
	))

	f.Fuzz(func(t *testing.T, source string) {
		md.Parser().Parse(text.NewReader([]byte(source)))
	})
}
