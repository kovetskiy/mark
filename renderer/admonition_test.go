package renderer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAdmonitionSyntaxesDiverge pins the per-syntax tables, which do not agree
// on what a word means: `!!! note` is the note macro, `> [!NOTE]` the info
// macro. The divergence is published behaviour; this is here so that
// unifying it is a decision rather than an accident of a refactor.
func TestAdmonitionSyntaxesDiverge(t *testing.T) {
	gh := map[string]string{
		"note":      "info",
		"tip":       "tip",
		"important": "info",
		"warning":   "note",
		"caution":   "warning",
		"unknown":   "info",
	}
	for alert, macro := range gh {
		assert.Equal(t, macro, ghAlertType(alert).String(), "GitHub alert %q", alert)
	}

	mkdocs := map[string]string{
		"info":    "info",
		"note":    "note",
		"warning": "warning",
		"tip":     "tip",
	}
	for class, macro := range mkdocs {
		assert.Equal(t, macro, mkDocsAdmonitionTypes[class].String(), "MkDocs class %q", class)
	}
}

// TestAdmonitionTypeOutOfRange covers a value outside the table. The name is
// interpolated into ac:name, so it answers with a fixed word, not a panic.
func TestAdmonitionTypeOutOfRange(t *testing.T) {
	assert.Equal(t, "none", AdmonitionType(-1).String())
	assert.Equal(t, "none", AdmonitionType(99).String())
}
