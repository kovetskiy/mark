package parser_test

import (
	"testing"

	cparser "github.com/kovetskiy/mark/v16/parser"
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
		"??? note \"T\" {.a #b c=1}\n    body\n???\n",
		"???+ tip\n\t??? warning \"t\"\n\t\tx\n\t???\n???\n",
		"> ??? note \"x\"\n>     q\n",
		"- ???+ tip\n      x\n\n  ???? warning\n      y\n  ????\n",
		"???+\n???\n???+ a {\n    b\n}\n",
		"???+ note \"\\\"q\\\" <&> ]]>\"\n    x\n",
		"??? \"T\"\n    x\n",
		"??? \xff\xc3\n    x\n",
		"??? note \"\x00\x1b\"\n    x\n",
		"??? what\nnext\n\n??? Really?\n??? what\n",
		"??? what\n\n\n    spaced\n??? note\n???\n",
		"??? note \"unterminated\n    x\n??? note \"T\" tail\n",
		"??? note\t\"T\"\n\tx\n",
		"???+ note\r\n    x\r\n???+\r\n",
		"!!! note\n    ??? tip\n        x\n    ???\n!!!\n",
		"??? a\n    !!!! b\n    ???\n!!!!\n",
		"> ??? note \"x\"\n> ???\n",
		"- ??? what\n  next\n",
		"a ??? note \"T\"\n    b\n",
		"what???\n",
		"   ???+ note\n    b\n",
		"    ??? note\n    b\n",
		"- x\n??? note\n    b\n",
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
