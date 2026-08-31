package renderer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEmojiID covers the id Confluence Cloud identifies an emoji by. The
// fallback attribute carries the character itself, so a wrong id is not fatal
// -- but it is what Cloud reads first, and the sequences below are the ones
// where "the codepoints in hex" is not the whole rule.
func TestEmojiID(t *testing.T) {
	tests := []struct {
		name  string
		runes []rune
		want  string
	}{
		{"a single codepoint", []rune{0x1F604}, "1f604"},
		{"the variation selector is left out", []rune{0x2764, 0xFE0F}, "2764"},
		{"a skin tone modifier is part of the id", []rune{0x1F44D, 0x1F3FD}, "1f44d-1f3fd"},
		{"a ZWJ sequence keeps every joiner", []rune{0x1F468, 0x200D, 0x1F469, 0x200D, 0x1F467}, "1f468-200d-1f469-200d-1f467"},
		{"a flag is two regional indicators", []rune{0x1F1E9, 0x1F1EA}, "1f1e9-1f1ea"},
		{"nothing", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, emojiID(tt.runes))
		})
	}
}

// TestConfluenceEmoticonNames pins the values of the legacy mapping against the
// names Confluence Data Center actually knows, from its storage format
// reference.
//
// A name outside that list is not an error anywhere: Data Center renders an
// unknown ac:emoticon as nothing at all, so a typo would silently delete the
// emoji from the page for every Data Center reader while looking correct on
// Cloud, which goes by ac:emoji-id instead.
func TestConfluenceEmoticonNames(t *testing.T) {
	known := map[string]bool{
		"smile": true, "sad": true, "cheeky": true, "laugh": true, "wink": true,
		"thumbs-up": true, "thumbs-down": true, "information": true, "tick": true,
		"cross": true, "warning": true, "plus": true, "minus": true, "question": true,
		"light-on": true, "light-off": true, "yellow-star": true, "red-star": true,
		"green-star": true, "blue-star": true, "heart": true, "broken-heart": true,
	}

	for id, name := range confluenceEmoticons {
		assert.True(t, known[name], "%q (id %s) is not a name Data Center knows", name, id)
	}
}

// TestFootnoteAnchorNames pins the anchor names, which are also the fragment a
// reader copies out of the address bar. They share one namespace with the
// page's heading anchors, hence the prefix, and a note cited more than once
// needs one anchor per citation for the way back to land where it started.
func TestFootnoteAnchorNames(t *testing.T) {
	assert.Equal(t, "footnote-1", footnoteAnchor(1))
	assert.Equal(t, "footnote-12", footnoteAnchor(12))

	assert.Equal(t, "footnote-ref-3", footnoteRefAnchor(3, 0),
		"the first citation carries no suffix, so the common case stays readable")
	assert.Equal(t, "footnote-ref-3-1", footnoteRefAnchor(3, 1))
	assert.Equal(t, "footnote-ref-3-2", footnoteRefAnchor(3, 2))
}
