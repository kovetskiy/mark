package export

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// within fails the test when convert takes longer than limit, which is far
// more than it needs and far less than work that grows with the square of
// the page would take.
func within(t *testing.T, limit time.Duration, storage string) *Document {
	t.Helper()

	start := time.Now()
	doc, err := Convert(storage, Options{Space: "DOCS"})
	require.NoError(t, err)
	require.Less(t, time.Since(start), limit)

	return doc
}

func TestConvertManyDateOpenings(t *testing.T) {
	const count = 8000

	t.Run("never closed", func(t *testing.T) {
		text := strings.Repeat("@date(", count)
		doc := within(t, 2*time.Second, "<p>"+text+"</p>")
		assert.Equal(t, text+"\n", doc.Markdown)
	})

	t.Run("closed at the end", func(t *testing.T) {
		doc := within(t, 2*time.Second, "<p>"+strings.Repeat("@date(", count)+"x)</p>")
		assert.Equal(t, strings.Repeat(`\@date(`, count)+"x)\n", doc.Markdown)
	})

	t.Run("each followed by spaces", func(t *testing.T) {
		text := strings.Repeat("@date(  ", count)
		doc := within(t, 2*time.Second, "<p>"+text+")</p>")
		assert.Equal(t, strings.Repeat(`\@date(  `, count-1)+"@date(  )\n", doc.Markdown)
	})
}

// TestEscapeDateLikeTheParser holds the escaping to the date feature's own
// reading: its pattern, and a value that is more than spaces and quotes.
func TestEscapeDateLikeTheParser(t *testing.T) {
	pattern := regexp.MustCompile(`^@date\(([^)]+)\)`)
	isDate := func(text string) bool {
		m := pattern.FindStringSubmatch(text)
		return m != nil && strings.Trim(strings.TrimSpace(m[1]), `"'`) != ""
	}

	values := []string{""}
	for range 5 {
		var longer []string
		for _, value := range values {
			for _, r := range []string{`"`, `'`, " ", "\n", "x", ")"} {
				longer = append(longer, value+r)
			}
		}
		values = longer

		for _, value := range values {
			for _, text := range []string{"@date(" + value, "@date(" + value + ")", "@date" + value, "@dat(" + value + ")"} {
				want := text
				if isDate(text) {
					want = `\` + text
				}
				require.Equal(t, want, escapeInline(text, inlineOptions{}), "%q", text)
			}
		}
	}

	assert.Equal(t, `2020 \@date(2020-01-02) and \@date("2020-01-03") but @date() and @date( "' )`,
		escapeInline(`2020 @date(2020-01-02) and @date("2020-01-03") but @date() and @date( "' )`, inlineOptions{}))
}
