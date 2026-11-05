package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdmonitionWord(t *testing.T) {
	markers := map[string]string{
		"Info: text":     "info",
		"note:":          "note",
		"NOTES: text":    "note",
		"Warn:\ttext":    "warn",
		"Warns: text":    "warn",
		"Warnings: text": "warning",
		"tIpS: text":     "tip",
		"Note:\nmore":    "note",
		"<!-- Info -->":  "info",
		"<!-- Tip: -->":  "tip",
		"<!-- Warn":      "warn",
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
		"<!-- Note that this is a draft -->",
		"<!-- Info: more -->",
		"",
	} {
		assert.Empty(t, AdmonitionWord(line), "%q is not a marker", line)
	}
}
