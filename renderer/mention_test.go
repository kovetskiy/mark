package renderer_test

import (
	"testing"

	cparser "github.com/kovetskiy/mark/v16/parser"
	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// TestMentionWithoutAnAPIFallsBackToTheName covers the path every offline run
// takes, --compile-only included: the ac:link:user template asks Confluence for
// the account behind the name, and with no API to ask it writes the name as
// text.
//
// The alternative would be an ac:link with an empty ri:user, which Confluence
// renders as a broken mention rather than as the word the author wrote.
func TestMentionWithoutAnAPIFallsBackToTheName(t *testing.T) {
	actual := render(t, "Hello @{username}!\n",
		[]html.Extension{
			crenderer.NewConfluenceMentionRenderer(newStdlib(t)),
			crenderer.NewConfluenceParagraphRenderer(),
			crenderer.NewConfluenceTextRenderer(false),
		},
		parser.WithInlineParsers(util.Prioritized(cparser.NewMentionParser(), 99)))
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "Hello username!")
	assert.NotContains(t, actual, "ri:user")
}
