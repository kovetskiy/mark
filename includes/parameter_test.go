package includes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTrimElementParameters(t *testing.T) {
	for name, tt := range map[string]struct{ in, expected string }{
		"element across lines": {
			"<ac:parameter ac:name=\"name\">\n  <ri:attachment ri:filename=\"a.drawio\"/>\n</ac:parameter>",
			`<ac:parameter ac:name="name"><ri:attachment ri:filename="a.drawio"/></ac:parameter>`,
		},
		"element already tight": {
			`<ac:parameter ac:name="name"><ri:attachment ri:filename="a.drawio"/></ac:parameter>`,
			`<ac:parameter ac:name="name"><ri:attachment ri:filename="a.drawio"/></ac:parameter>`,
		},
		"element with children": {
			"<ac:parameter ac:name=\"n\">\n<ac:link><ri:page ri:content-title=\"T\"/></ac:link>\n</ac:parameter>",
			`<ac:parameter ac:name="n"><ac:link><ri:page ri:content-title="T"/></ac:link></ac:parameter>`,
		},
		"string keeps its spacing": {
			`<ac:parameter ac:name="title">  spaced  </ac:parameter>`,
			`<ac:parameter ac:name="title">  spaced  </ac:parameter>`,
		},
		"string that mentions a tag is still a string": {
			"<ac:parameter ac:name=\"t\">\n  see <ri:attachment/> here\n</ac:parameter>",
			"<ac:parameter ac:name=\"t\">\n  see <ri:attachment/> here\n</ac:parameter>",
		},
		"empty parameter": {
			`<ac:parameter ac:name="t"></ac:parameter>`,
			`<ac:parameter ac:name="t"></ac:parameter>`,
		},
		"self-closing parameter": {
			`<ac:parameter ac:name="t"/>`,
			`<ac:parameter ac:name="t"/>`,
		},
		"several parameters": {
			"<ac:parameter ac:name=\"a\">\n<ri:x/>\n</ac:parameter><ac:parameter ac:name=\"b\">  text  </ac:parameter>",
			`<ac:parameter ac:name="a"><ri:x/></ac:parameter><ac:parameter ac:name="b">  text  </ac:parameter>`,
		},
		"body whitespace is not a parameter": {
			"<ac:rich-text-body>\n\n- one\n\n</ac:rich-text-body>",
			"<ac:rich-text-body>\n\n- one\n\n</ac:rich-text-body>",
		},
		"nothing to do": {
			"just some text", "just some text",
		},
		"unclosed parameter is left alone": {
			`<ac:parameter ac:name="t">   <ri:x/>`,
			`<ac:parameter ac:name="t">   <ri:x/>`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(TrimElementParameters([]byte(tt.in))))
		})
	}
}
