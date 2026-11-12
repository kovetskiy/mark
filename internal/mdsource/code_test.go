package mdsource

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A footnote or a definition holds its later paragraphs indented by four
// spaces, which is also how an indented code block is written. The document is
// compiled with both extensions, so the regions have to be read with them too.
func TestCodeRegionsReadContinuationParagraphsAsText(t *testing.T) {
	for name, doc := range map[string]string{
		"footnote":        "Text[^1].\n\n[^1]: first\n\n    MARK para\n",
		"definition":      "Term\n: first\n\n    MARK para\n",
		"footnote, tab":   "Text[^1].\n\n[^1]: first\n\n\tMARK para\n",
		"nested in quote": "> Text[^1].\n>\n> [^1]: first\n>\n>     MARK para\n",
	} {
		t.Run(name, func(t *testing.T) {
			offset := strings.Index(doc, "MARK")
			require.NotEqual(t, -1, offset)

			assert.False(t, InCode(CodeRegions([]byte(doc)), offset))
		})
	}
}

func TestCodeRegionsStillFindCodeBesideContinuations(t *testing.T) {
	for name, doc := range map[string]string{
		"indented block":                 "Text.\n\n    MARK code\n",
		"indented block after a list":    "- item\n\nText.\n\n    MARK code\n",
		"block inside a footnote":        "Text[^1].\n\n[^1]: first\n\n        MARK code\n",
		"fence inside a footnote":        "Text[^1].\n\n[^1]: first\n\n    ```\n    MARK code\n    ```\n",
		"span inside a footnote":         "Text[^1].\n\n[^1]: first\n\n    a `MARK code` b\n",
		"block inside a definition":      "Term\n: first\n\n        MARK code\n",
		"span inside a definition":       "Term\n: first `MARK code`\n",
		"block after a footnote ends":    "Text[^1].\n\n[^1]: first\n\nNext.\n\n    MARK code\n",
		"block after a definition ends":  "Term\n: first\n\nNext.\n\n    MARK code\n",
		"a colon line that defines none": "Text.\n\n    : MARK code\n",
	} {
		t.Run(name, func(t *testing.T) {
			offset := strings.Index(doc, "MARK")
			require.NotEqual(t, -1, offset)

			assert.True(t, InCode(CodeRegions([]byte(doc)), offset))
		})
	}
}
