package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdmonitionMarker(t *testing.T) {
	markers := map[string]string{
		"Info: text":     "info",
		"note:":          "note",
		"NOTES: text":    "note",
		"Warn:\ttext":    "warn",
		"Warnings: text": "warning",
		"tIpS: text":     "tip",
		"<!-- Info -->":  "info",
		"<!-- Tip: -->":  "tip",
		"<!-- Warn":      "warn",
	}
	for line, word := range markers {
		marker := AdmonitionMarker(line)
		assert.NotEmpty(t, marker, "%q is a marker", line)
		assert.Equal(t, word, AdmonitionWord(marker), "%q", line)
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
		assert.Empty(t, AdmonitionMarker(line), "%q is not a marker", line)
	}
}
