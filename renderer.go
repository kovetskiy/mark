package main

import (
	"bytes"

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
