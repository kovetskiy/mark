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
	word                  string
	macro                 string
	colon, github, mkdocs bool
}{
	{"note", "info", true, true, true},
	{"info", "info", true, false, true},
	{"important", "info", true, true, true},
	{"abstract", "info", false, false, true},
	{"summary", "info", false, false, true},
	{"tldr", "info", false, false, true},
	{"todo", "info", false, false, true},
	{"tip", "tip", true, true, true},
	{"hint", "tip", true, false, true},
	{"success", "tip", false, false, true},
	{"check", "tip", false, false, true},
	{"done", "tip", false, false, true},
	{"warning", "note", true, true, true},
	{"warn", "note", true, false, false},
	{"attention", "note", true, false, true},
	{"caution", "warning", true, true, true},
	{"danger", "warning", true, false, true},
	{"error", "warning", false, false, true},
	{"failure", "warning", false, false, true},
	{"fail", "warning", false, false, true},
	{"missing", "warning", false, false, true},
	{"bug", "warning", false, false, true},
}

// macroIf is macro when accepted, and "none" otherwise.
func macroIf(accepted bool, macro string) string {
	if accepted {
		return macro
	}
	return "none"
}

func TestAdmonitionWordsHaveOneMacro(t *testing.T) {
	for _, s := range admonitionSyntaxes {
		colon := macroIf(s.colon, s.macro)
		assert.Equal(t, colon, ClassifyBlockQuote(s.word+": text").String(), "colon %q", s.word)
		assert.Equal(t, colon, ClassifyBlockQuote(strings.ToUpper(s.word)+":").String(), "colon %q", s.word)

		mkdocs := macroIf(s.mkdocs, s.macro)
		assert.Equal(t, mkdocs, mkDocsAdmonitionType(s.word).String(), "MkDocs %q", s.word)
		assert.Equal(t, mkdocs, mkDocsAdmonitionType(strings.ToUpper(s.word)).String(), "MkDocs %q", s.word)

		// The transformer hands over only GitHub's five types; the rest
		// never reach ghAlertType.
		if s.github {
			assert.Equal(t, s.macro, ghAlertType(s.word).String(), "GitHub %q", s.word)
		}
	}

	assert.Equal(t, "info", ghAlertType("unknown").String())
	assert.Equal(t, "info", mkDocsAdmonitionType("Note").String())
	assert.Equal(t, "none", mkDocsAdmonitionType("Warn").String())
	for _, class := range []string{"question", "help", "faq", "example", "quote", "cite"} {
		assert.Equal(t, "none", mkDocsAdmonitionType(class).String(), "MkDocs %q", class)
	}
}

// TestAdmonitionWordsAgree keeps the colon syntax's words and the shared table
// one list, and admonitionSyntaxes in step with both tables.
func TestAdmonitionWordsAgree(t *testing.T) {
	assert.ElementsMatch(t, syntax.AdmonitionWords, slices.Collect(maps.Keys(admonitionTypes)))
	for word := range mkDocsOnlyTypes {
		assert.NotContains(t, admonitionTypes, word, "%q is in both tables", word)
	}

	assert.Len(t, admonitionSyntaxes, len(admonitionTypes)+len(mkDocsOnlyTypes))
	for _, s := range admonitionSyntaxes {
		_, shared := admonitionTypes[s.word]
		assert.Equal(t, s.colon, shared, "colon %q", s.word)
	}
}

// TestAdmonitionTypeOutOfRange covers a value outside the table. The name is
// interpolated into ac:name, so it answers with a fixed word, not a panic.
func TestAdmonitionTypeOutOfRange(t *testing.T) {
	assert.Equal(t, "none", AdmonitionType(-1).String())
	assert.Equal(t, "none", AdmonitionType(99).String())
}
