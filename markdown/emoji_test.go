package mark_test

import (
	"testing"

	mark "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
)

func compileEmoji(t *testing.T, markdown string, features ...string) string {
	t.Helper()

	lib, err := stdlib.New(nil)
	assert.NoError(t, err)

	actual, _, err := mark.CompileMarkdown([]byte(markdown), lib, "testdata/test.md", types.MarkConfig{
		Features: features,
	})
	assert.NoError(t, err)

	return actual
}

// TestEmojiFeature covers the two ways an emoji reaches the page and why there
// are two. Confluence Data Center goes by ac:emoticon's ac:name and knows only
// the names its storage-format reference lists, so a shortcode outside that set
// is written as the character itself rather than as a macro Data Center would
// render as nothing.
func TestEmojiFeature(t *testing.T) {
	enabled := compileEmoji(t, "Green :white_check_mark: and shipped :tada:\n", "emoji")
	assertWellFormed(t, enabled)

	assert.Contains(t, enabled, `<ac:emoticon ac:name="tick" ac:emoji-shortname=":white_check_mark:" ac:emoji-id="2705" ac:emoji-fallback="✅"/>`)
	assert.Contains(t, enabled, "shipped 🎉", "an emoji with no legacy name is written as the character")
	assert.NotContains(t, enabled, `ac:emoji-shortname=":tada:"`)

	disabled := compileEmoji(t, "Green :white_check_mark: and shipped :tada:\n", "mermaid", "mention")
	assert.Contains(t, disabled, ":white_check_mark:", "without the feature a shortcode is text")
	assert.Contains(t, disabled, ":tada:")
	assert.NotContains(t, disabled, "ac:emoticon")
}

// TestEmojiAliases pins that the legacy name is chosen by the emoji rather than
// by the shortcode that named it: :+1: and :thumbsup: are one character, and
// both have to reach Data Center as thumbs-up.
func TestEmojiAliases(t *testing.T) {
	for _, shortcode := range []string{":+1:", ":thumbsup:"} {
		actual := compileEmoji(t, shortcode+"\n", "emoji")
		assertWellFormed(t, actual)

		assert.Contains(t, actual, `ac:name="thumbs-up"`)
		assert.Contains(t, actual, `ac:emoji-id="1f44d"`)
		// The shortname is the one the author typed, which is the one the Cloud
		// editor offers back when the emoji is edited.
		assert.Contains(t, actual, `ac:emoji-shortname="`+shortcode+`"`)
	}
}

// TestEmojiVariationSelector covers an emoji written with U+FE0F. The selector
// asks for the colour glyph and is part of the character, so it belongs in the
// fallback; Cloud's ids are written without it, so it must not reach the id.
func TestEmojiVariationSelector(t *testing.T) {
	actual := compileEmoji(t, ":heart:\n", "emoji")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `ac:emoji-id="2764"`)
	assert.NotContains(t, actual, "2764-fe0f")
	assert.Contains(t, actual, "ac:emoji-fallback=\"❤️\"")
}

// TestEmojiLeavesTextAlone covers the three ways a colon appears in a document
// without naming an emoji. The parser triggers on every one of them, so each is
// a chance to eat text that was not a shortcode.
func TestEmojiLeavesTextAlone(t *testing.T) {
	actual := compileEmoji(t, "At 10:30, `:tada:` in code, and :not_an_emoji: unknown.\n", "emoji")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "At 10:30,")
	assert.Contains(t, actual, "<code>:tada:</code>", "a shortcode in a code span is literal")
	assert.Contains(t, actual, ":not_an_emoji:")
	assert.NotContains(t, actual, "ac:emoticon")

	// A URL is the colon that matters most: the emoji parser triggers on the
	// one in the scheme, and eating it would break the link rather than the
	// text around it.
	link := compileEmoji(t, "See https://example.com/a:b\n", "emoji")
	assert.Contains(t, link, `<a href="https://example.com/a:b">https://example.com/a:b</a>`)
}

// TestEmojiInCodeBlockUntouched covers the same thing for a fenced block, where
// the content is CDATA rather than markup and an expanded emoji would change a
// code sample the author meant literally.
func TestEmojiInCodeBlockUntouched(t *testing.T) {
	actual := compileEmoji(t, "```\nprintln(\":tada:\")\n```\n", "emoji")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `println(":tada:")`)
	assert.NotContains(t, actual, "🎉")
}

// TestEmojiHeadingAnchor covers an emoji in a heading, which is also a heading
// anchor. Ids are derived from the heading's source text before any inline node
// is rendered, so it is the shortcode that shapes the anchor and not the emoji
// -- worth knowing when writing a link to such a heading by hand.
func TestEmojiHeadingAnchor(t *testing.T) {
	actual := compileEmoji(t, "## Release :tada:\n", "emoji")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<h2 id="Release-tada">Release 🎉</h2>`)
}
