package renderer

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/internal/syntax"
	"github.com/stretchr/testify/assert"
)

// admonitionSyntaxes is each admonition word, the macro it is published as,
// and the syntaxes that accept it: every word means one macro wherever it is
// written.
var admonitionSyntaxes = []struct {
	word           string
	macro          string
	github, mkdocs bool
}{
	{"note", "info", true, true},
	{"info", "info", false, true},
	{"important", "info", true, true},
	{"tip", "tip", true, true},
	{"hint", "tip", false, true},
	{"success", "tip", false, true},
	{"warning", "note", true, true},
	{"warn", "note", false, false},
	{"attention", "note", false, true},
	{"caution", "warning", true, true},
	{"danger", "warning", false, true},
	{"error", "warning", false, true},
}

func TestAdmonitionWordsHaveOneMacro(t *testing.T) {
	for _, s := range admonitionSyntaxes {
		assert.Equal(t, s.macro, ClassifyBlockQuote(s.word+": text").String(), "colon %q", s.word)
		assert.Equal(t, s.macro, ClassifyBlockQuote(strings.ToUpper(s.word)+":").String(), "colon %q", s.word)

		mkdocs := "none"
		if s.mkdocs {
			mkdocs = s.macro
		}
		assert.Equal(t, mkdocs, mkDocsAdmonitionType(s.word).String(), "MkDocs %q", s.word)

		// The transformer hands over only GitHub's five types; the rest
		// never reach ghAlertType.
		if s.github {
			assert.Equal(t, s.macro, ghAlertType(s.word).String(), "GitHub %q", s.word)
		}
	}

	assert.Equal(t, "info", ghAlertType("unknown").String())
	for _, class := range []string{"abstract", "question", "example", "quote", "bug", "failure", "Note"} {
		assert.Equal(t, "none", mkDocsAdmonitionType(class).String(), "MkDocs %q", class)
	}
}

// TestAdmonitionWordsAgree keeps the words the colon syntax reads and the
// words with a macro one list.
func TestAdmonitionWordsAgree(t *testing.T) {
	assert.ElementsMatch(t, syntax.AdmonitionWords, slices.Collect(maps.Keys(admonitionTypes)))
	assert.Len(t, admonitionSyntaxes, len(admonitionTypes))
}

// TestAdmonitionTypeOutOfRange covers a value outside the table. The name is
// interpolated into ac:name, so it answers with a fixed word, not a panic.
func TestAdmonitionTypeOutOfRange(t *testing.T) {
	assert.Equal(t, "none", AdmonitionType(-1).String())
	assert.Equal(t, "none", AdmonitionType(99).String())
}
