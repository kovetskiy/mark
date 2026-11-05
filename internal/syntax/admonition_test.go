package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdmonitionWord(t *testing.T) {
	markers := map[string]string{
		"Info: text":       "info",
		"note:":            "note",
		"NOTES: text":      "note",
		"Warn:\ttext":      "warn",
		"Warns: text":      "warn",
		"Warnings: text":   "warning",
		"tIpS: text":       "tip",
		"Note:\nmore":      "note",
		"Important: text":  "important",
		"Hints: text":      "hint",
		"Success: text":    "success",
		"SUCCESSES: text":  "success",
		"Attention: text":  "attention",
		"Caution: text":    "caution",
		"Danger: text":     "danger",
		"Errors: text":     "error",
		"<!-- Info -->":    "info",
		"<!-- Tip: -->":    "tip",
		"<!-- Warn":        "warn",
		"<!-- danger -->":  "danger",
		"  Error:  spaced": "error",
	}
	for line, word := range markers {
		assert.Equal(t, word, AdmonitionWord(line), "%q", line)
	}

	for _, line := range []string{
		"Note that this is a quote.",
		"Tips are welcome.",
		"Information is power.",
		"Note",
		"Note : spaced",
		"Note:spaced",
		"Notice: not a word mark reads",
		"Success" + "s: \"es\" is its plural",
		"Notees: not a plural",
		"Errorss: not a plural",
		"Error-prone: not the word",
		"Danger : not the word",
		"<!-- Note that this is a draft -->",
		"<!-- Info: more -->",
		"<!-- -->",
		":",
		"",
	} {
		assert.Empty(t, AdmonitionWord(line), "%q is not a marker", line)
	}
}

// TestAdmonitionWordAllocs keeps the classifier, which runs on every
// blockquote, free of allocations.
func TestAdmonitionWordAllocs(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		_ = AdmonitionWord("<!-- WARNINGS: -->")
		_ = AdmonitionWord("Successes: done")
	})
	assert.Zero(t, allocs)
}
