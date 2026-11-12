package export

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// convertLimit is far more than a conversion needs, under the race detector
// on a machine busy with other tests too, and far less than work that grows
// with the square of the page would take.
const convertLimit = 10 * time.Second

// within fails the test when convert takes longer than convertLimit.
func within(t *testing.T, storage string) *Document {
	t.Helper()

	start := time.Now()
	doc, err := Convert(storage, Options{Space: "DOCS"})
	require.NoError(t, err)
	require.Less(t, time.Since(start), convertLimit)

	return doc
}

func TestConvertManyDateOpenings(t *testing.T) {
	const count = 20000

	t.Run("never closed", func(t *testing.T) {
		text := strings.Repeat("@date(", count)
		doc := within(t, "<p>"+text+"</p>")
		assert.Equal(t, text+"\n", doc.Markdown)
	})

	t.Run("closed at the end", func(t *testing.T) {
		doc := within(t, "<p>"+strings.Repeat("@date(", count)+"x)</p>")
		assert.Equal(t, strings.Repeat(`\@date(`, count)+"x)\n", doc.Markdown)
	})

	t.Run("each followed by spaces", func(t *testing.T) {
		text := strings.Repeat("@date(  ", count)
		doc := within(t, "<p>"+text+")</p>")
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
