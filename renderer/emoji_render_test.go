package renderer_test

import (
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	emoji "github.com/yuin/goldmark-emoji"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func emojiRender(t *testing.T, source string) string {
	t.Helper()

	return render(t, source,
		[]renderer.NodeRenderer{
			crenderer.NewConfluenceEmojiRenderer(newStdlib(t)),
			crenderer.NewConfluenceParagraphRenderer(),
			crenderer.NewConfluenceTextRenderer(false),
		},
		parser.WithInlineParsers(util.Prioritized(emoji.NewParser(), 999)))
}

// TestEmojiWithALegacyNameBecomesTheMacro covers the emoji Confluence Data
// Center has a name for. The macro carries the name for Data Center and the
// ac:emoji-* attributes for Cloud, so both render the same character.
func TestEmojiWithALegacyNameBecomesTheMacro(t *testing.T) {
	actual := emojiRender(t, "Green :white_check_mark:\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual,
		`<ac:emoticon ac:name="tick" ac:emoji-shortname=":white_check_mark:" ac:emoji-id="2705" ac:emoji-fallback="✅"/>`)
}

// TestEmojiWithoutALegacyNameBecomesTheCharacter covers everything else. Data
// Center renders an ac:emoticon whose name it does not know as nothing at all,
// so the character itself goes on the page instead -- text, which both flavours
// render without having to understand a macro.
func TestEmojiWithoutALegacyNameBecomesTheCharacter(t *testing.T) {
	actual := emojiRender(t, "Shipped :tada:\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "Shipped 🎉")
	assert.NotContains(t, actual, "ac:emoticon")
}

// TestEmojiAliasesShareTheLegacyName pins that the name is chosen by the emoji
// rather than by the shortcode: :+1: and :thumbsup: are one character.
func TestEmojiAliasesShareTheLegacyName(t *testing.T) {
	for _, shortcode := range []string{":+1:", ":thumbsup:"} {
		actual := emojiRender(t, shortcode+"\n")
		assertWellFormed(t, actual)

		assert.Contains(t, actual, `ac:name="thumbs-up"`)
		assert.Contains(t, actual, `ac:emoji-shortname="`+shortcode+`"`,
			"the shortname is the one the author typed")
	}
}

// TestEmojiUnknownShortcodeIsLeftAlone covers the text that only looks like a
// shortcode.
func TestEmojiUnknownShortcodeIsLeftAlone(t *testing.T) {
	actual := emojiRender(t, "At 10:30 and :not_an_emoji: too\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "At 10:30")
	assert.Contains(t, actual, ":not_an_emoji:")
	assert.NotContains(t, actual, "ac:emoticon")
}
