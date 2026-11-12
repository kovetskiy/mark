package export

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nested is open, text and close, depth times one inside the other.
func nested(depth int, open, text, closing string) string {
	return strings.Repeat(open+text, depth) + strings.Repeat(closing, depth)
}

const (
	taskOpen  = `<ac:task-list><ac:task><ac:task-status>incomplete</ac:task-status><ac:task-body>`
	taskClose = `</ac:task-body></ac:task></ac:task-list>`
)

// TestConvertDeepNesting: a page nests lists and blockquotes as deep as it
// likes, and the time it takes to write has to stay in step with its size.
func TestConvertDeepNesting(t *testing.T) {
	const depth = 3000

	for name, storage := range map[string]string{
		"bullets":                nested(depth, "<ul><li>", "a", "</li></ul>"),
		"numbers":                nested(depth, "<ol><li>", "a", "</li></ol>"),
		"tasks":                  nested(depth, taskOpen, "a", taskClose),
		"blockquotes":            nested(depth, "<blockquote>", "<p>a</p>", "</blockquote>"),
		"blockquotes in bullets": nested(depth, "<ul><li><blockquote>", "<p>a</p>", "</blockquote></li></ul>"),
	} {
		t.Run(name, func(t *testing.T) {
			doc := within(t, storage)
			assert.Less(t, len(doc.Markdown), 3*len(storage))
		})
	}
}

// TestConvertNestingAsMarkdown: lists and blockquotes are Markdown down to
// maxNesting of them, and storage format from there on.
func TestConvertNestingAsMarkdown(t *testing.T) {
	var bullets, numbers, quotes strings.Builder
	for level := range maxNesting {
		bullets.WriteString(strings.Repeat("  ", level) + "- a\n")
		numbers.WriteString(strings.Repeat("   ", level) + "1. a\n")
		quotes.WriteString(strings.Repeat("> ", level+1) + "a\n")
		if level < maxNesting-1 {
			quotes.WriteString(strings.Repeat("> ", level) + ">\n")
		}
	}

	assert.Equal(t, bullets.String(), convert(t, nested(maxNesting, "<ul><li>", "a", "</li></ul>")))
	assert.Equal(t, numbers.String(), convert(t, nested(maxNesting, "<ol><li>", "a", "</li></ol>")))
	assert.Equal(t, quotes.String(), convert(t, nested(maxNesting, "<blockquote>", "<p>a</p>", "</blockquote>")))

	// A list item that holds storage format holds it as a block of its own,
	// after a paragraph.
	for name, test := range map[string]struct{ open, text, last, closing string }{
		"bullets":     {"<ul><li>", "a", "<p>a</p>", "</li></ul>"},
		"numbers":     {"<ol><li>", "a", "<p>a</p>", "</li></ol>"},
		"blockquotes": {"<blockquote>", "<p>a</p>", "<p>a</p>", "</blockquote>"},
	} {
		t.Run(name, func(t *testing.T) {
			kept := nested(2, test.open, test.text, test.closing)

			md, published := republish(t, nested(maxNesting+2, test.open, test.text, test.closing))
			assert.True(t, strings.HasSuffix(md, " "+kept+"\n"), md)
			assert.Equal(t, canonical(t, strings.Repeat(test.open+test.text, maxNesting-1)+test.open+test.last+
				kept+strings.Repeat(test.closing, maxNesting)), published)
		})
	}

	t.Run("tasks", func(t *testing.T) {
		kept := nested(2, taskOpen, "a", taskClose)
		md := convert(t, nested(maxNesting+2, taskOpen, "a", taskClose))
		assert.True(t, strings.HasSuffix(md, "- [ ] a\n\n"+strings.Repeat("  ", maxNesting)+kept+"\n"), md)
	})
}

// TestTextContentDeepNesting: a link reads the text it shows at every level
// of markup it is nested in, so reading it must not build a string per level.
func TestTextContentDeepNesting(t *testing.T) {
	const depth = 1000

	root, err := parseStorage(nested(depth, `<a href="https://example.com">`, "a", "</a>"))
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("a", depth), root.textContent())

	assert.Less(t, testing.AllocsPerRun(10, func() { _ = root.textContent() }), float64(depth/10))
}
