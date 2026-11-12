package mdsource

import (
	"strings"
	"sync"
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
		"block after a table's caption":  "| a | b |\n|---|---|\n| c | d |\n\n: Caption\n\n    MARK code\n",
		"block after a table and colon":  "| a | b |\n|---|---|\n| c | d |\n: x\n\n    MARK code\n",
		"span in a table cell":           "| a | b |\n|---|---|\n| `MARK code` | d |\n",
		"span with a pipe in a cell":     "| a | b |\n|---|---|\n| `x \\| MARK code` | d |\n",
	} {
		t.Run(name, func(t *testing.T) {
			offset := strings.Index(doc, "MARK")
			require.NotEqual(t, -1, offset)

			assert.True(t, InCode(CodeRegions([]byte(doc)), offset))
		})
	}
}

// One parser serves every call, so calls from several goroutines must not
// disturb each other; the race detector is what fails this.
func TestCodeRegionsConcurrentCalls(t *testing.T) {
	doc := []byte("Text[^1].\n\n[^1]: first\n\n    para\n\n| a |\n|---|\n| `x \\| y` |\n\n    code\n")
	want := CodeRegions(doc)
	require.Len(t, want, 2)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				assert.Equal(t, want, CodeRegions(doc))
			}
		})
	}
	wg.Wait()
}
