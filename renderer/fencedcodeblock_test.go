package renderer_test

import (
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/renderer"
)

// collectingAttacher stands in for the extension, which is what collects the
// attachments a diagram block produces. Nothing here renders a diagram -- that
// needs a browser -- so it only has to exist.
type collectingAttacher struct {
	attachments []attachment.Attachment
}

func (c *collectingAttacher) Attach(a attachment.Attachment) {
	c.attachments = append(c.attachments, a)
}

// fencedCode renders one fenced block with the given info string.
//
// The info string is parsed by reBlockDetails in fencedcodeblock.go. Note that
// the exported ParseLanguage and ParseTitle in the same file are not used by
// the renderer and do not agree with it -- ParseLanguage("collapse") returns no
// language, while the renderer takes "collapse" as the language name.
func fencedCode(t *testing.T, info string) string {
	t.Helper()

	return render(t, "```"+info+"\nsample\n```\n", []renderer.NodeRenderer{
		crenderer.NewConfluenceFencedCodeBlockRenderer(newStdlib(t), &collectingAttacher{}, types.MarkConfig{}),
	})
}

// TestFencedCodeBlockLanguage covers the language name, which is the part of
// the info string with the most ways to go wrong: real language names carry
// characters that are not word characters, and a \w-only class used to cut
// "c#" down to "c" and leave "#" behind as a theme.
func TestFencedCodeBlockLanguage(t *testing.T) {
	tests := []struct {
		info string
		want string
	}{
		{"go", "go"},
		{"c#", "c#"},
		{"c++", "c++"},
		{"objective-c", "objective-c"},
		{"html/xml", "html/xml"},
		{".NET", ".NET"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.info, func(t *testing.T) {
			actual := fencedCode(t, tt.info)
			assertWellFormed(t, actual)

			assert.Contains(t, actual, `<ac:parameter ac:name="language">`+tt.want+`</ac:parameter>`)
			assert.NotContains(t, actual, `ac:name="theme"`,
				"a language is not an option, and must not fall through to the theme catch-all")
		})
	}
}

// TestFencedCodeBlockOptions covers everything after the language: the options
// the README documents, and the numeric one that means two things at once.
func TestFencedCodeBlockOptions(t *testing.T) {
	tests := []struct {
		name        string
		info        string
		contains    []string
		notContains []string
	}{
		{
			name:        "collapse",
			info:        "go collapse",
			contains:    []string{`<ac:parameter ac:name="collapse">true</ac:parameter>`},
			notContains: []string{`ac:name="theme"`},
		},
		{
			name:     "nocollapse wins over the default",
			info:     "go nocollapse",
			contains: []string{`<ac:parameter ac:name="collapse">false</ac:parameter>`},
		},
		{
			name:     "linenumbers",
			info:     "go linenumbers",
			contains: []string{`<ac:parameter ac:name="linenumbers">true</ac:parameter>`},
		},
		{
			name: "a number sets the first line and turns numbering on",
			info: "go 10",
			contains: []string{
				`<ac:parameter ac:name="linenumbers">true</ac:parameter>`,
				`<ac:parameter ac:name="firstline">10</ac:parameter>`,
			},
		},
		{
			name:     "an unrecognised option is a theme",
			info:     "go Midnight",
			contains: []string{`<ac:parameter ac:name="theme">Midnight</ac:parameter>`},
		},
		{
			name:     "title takes the rest of the line",
			info:     "go title Some long title",
			contains: []string{`<ac:parameter ac:name="title">Some long title</ac:parameter>`},
		},
		{
			name: "everything at once",
			info: "bash 1 collapse midnight title Some long long bash function",
			contains: []string{
				`<ac:parameter ac:name="language">bash</ac:parameter>`,
				`<ac:parameter ac:name="collapse">true</ac:parameter>`,
				`<ac:parameter ac:name="theme">midnight</ac:parameter>`,
				`<ac:parameter ac:name="firstline">1</ac:parameter>`,
				`<ac:parameter ac:name="title">Some long long bash function</ac:parameter>`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := fencedCode(t, tt.info)
			assertWellFormed(t, actual)

			for _, want := range tt.contains {
				assert.Contains(t, actual, want)
			}
			for _, unwanted := range tt.notContains {
				assert.NotContains(t, actual, unwanted)
			}
		})
	}
}

// TestFencedCodeBlockDashLanguage covers the "-" the README offers as the way
// to write a block with no language but with the other options after it
// ("- 1 collapse midnight title ...").
//
// The regex always meant to allow it -- its comment reads (<Lang>|-) -- but
// widening the language class to accept the hyphens in real language names
// (objective-c) made the bare marker match as a language of its own, and the
// macro went out with ac:name="language" set to a literal "-".
func TestFencedCodeBlockDashLanguage(t *testing.T) {
	actual := fencedCode(t, "- 1 collapse title Some long long code")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:parameter ac:name="language"></ac:parameter>`,
		"the marker means no language, not a language called -")
	assert.Contains(t, actual, `<ac:parameter ac:name="title">Some long long code</ac:parameter>`)
	assert.Contains(t, actual, `<ac:parameter ac:name="collapse">true</ac:parameter>`,
		"and the options after it are still read")
}

// TestFencedCodeBlockDashAlone is the marker with nothing after it.
func TestFencedCodeBlockDashAlone(t *testing.T) {
	actual := fencedCode(t, "-")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:parameter ac:name="language"></ac:parameter>`)
}

// TestFencedCodeBlockLanguageWithAHyphenSurvives is the reason the class was
// widened in the first place, and has to keep working.
func TestFencedCodeBlockLanguageWithAHyphenSurvives(t *testing.T) {
	actual := fencedCode(t, "objective-c")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:parameter ac:name="language">objective-c</ac:parameter>`)
}

// TestFencedCodeBlockBraceInATitleIsKept: the attribute-block pattern is for
// the "{ .js hl_lines=... }" list Pandoc and MkDocs write at one end of the
// info string. Matching it anywhere took a brace out of a title written in the
// documented space form.
func TestFencedCodeBlockBraceInATitleIsKept(t *testing.T) {
	actual := fencedCode(t, "js title Some {Thing} Here")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:parameter ac:name="title">Some {Thing} Here</ac:parameter>`)
	assert.Contains(t, actual, `<ac:parameter ac:name="language">js</ac:parameter>`)
}

// TestFencedCodeBlockEscapesTheCDATATerminator covers a code sample carrying
// the one sequence CDATA cannot hold.
func TestFencedCodeBlockEscapesTheCDATATerminator(t *testing.T) {
	actual := render(t, "```xml\n<![CDATA[x]]>\n```\n", []renderer.NodeRenderer{
		crenderer.NewConfluenceFencedCodeBlockRenderer(newStdlib(t), &collectingAttacher{}, types.MarkConfig{}),
	})
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `]]><![CDATA[]]]]><![CDATA[>`)
}

// TestFencedCodeBlockTitleIsEscaped covers a title carrying markup characters:
// it is interpolated into an attribute, so an unescaped one would break the
// document rather than the block.
func TestFencedCodeBlockTitleIsEscaped(t *testing.T) {
	actual := fencedCode(t, `go title A & B <script>`)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `A &amp; B &lt;script&gt;`)
}

// TestFencedCodeBlockDiagramLanguagesNeedTheirFeature covers the guard in front
// of the diagram renderers: without the feature, a d2 or mermaid block is an
// ordinary code block, and nothing tries to start a browser.
func TestFencedCodeBlockDiagramLanguagesNeedTheirFeature(t *testing.T) {
	for _, lang := range []string{"d2", "mermaid"} {
		actual := fencedCode(t, lang)
		assertWellFormed(t, actual)

		assert.Contains(t, actual, `<ac:parameter ac:name="language">`+lang+`</ac:parameter>`)
		assert.NotContains(t, actual, "ac:image")
	}
}
