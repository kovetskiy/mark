package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractFrontMatterDocument(t *testing.T, document string) (*Meta, string, error) {
	t.Helper()

	meta, body, err := ExtractMeta([]byte(document), ExtractOptions{Filename: "doc.md", FrontMatter: true})

	return meta, string(body), err
}

// TestFrontMatterOpensOnTheFirstLineOnly covers a document whose second line
// is a dash rule. Front matter opens only on the first line: accepting a
// delimiter anywhere else fails a page that opens with a heading and a
// horizontal rule with "unterminated YAML front matter", and loses everything
// above a repeat of the first line further down.
func TestFrontMatterOpensOnTheFirstLineOnly(t *testing.T) {
	for name, document := range map[string]string{
		"heading then rule":  "# Heading\n---\n\nBody.\n",
		"blank line first":   "\n---\nspace: DOCS\n---\n\nBody.\n",
		"header then rule":   "<!-- Space: DOCS -->\n---\n<!-- Space: DOCS -->\nBody.\n",
		"indented delimiter": " ---\nspace: DOCS\n---\n",
		"trailing blank":     "--- \nspace: DOCS\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			meta, body, err := extractFrontMatterDocument(t, document)

			require.NoError(t, err)
			if meta != nil {
				assert.Equal(t, "DOCS", meta.Space)
			}
			assert.Contains(t, body, "---")
		})
	}
}

// TestFrontMatterClosingLineMayEndInBlanks covers a closing delimiter with
// trailing blanks. The YAML has to end where the body is cut, or the page's
// own text is taken for metadata.
func TestFrontMatterClosingLineMayEndInBlanks(t *testing.T) {
	meta, body, err := extractFrontMatterDocument(t,
		"---\nspace: DOCS\ntitle: Doc\n--- \t\ntitle: Not metadata\n",
	)

	require.NoError(t, err)
	assert.Equal(t, "Doc", meta.Title)
	assert.Equal(t, "title: Not metadata\n", body)
}

func TestFrontMatterDelimiters(t *testing.T) {
	t.Run("longer run closes on the same run", func(t *testing.T) {
		meta, body, err := extractFrontMatterDocument(t,
			"-----\nspace: DOCS\ntitle: Doc\n---\nstill: yaml\n-----\nBody.\n",
		)

		require.NoError(t, err)
		assert.Equal(t, "Doc", meta.Title)
		assert.Equal(t, "Body.\n", body)
	})

	t.Run("CRLF", func(t *testing.T) {
		meta, body, err := extractFrontMatterDocument(t,
			"---\r\nspace: DOCS\r\ntitle: Doc\r\n---\r\nBody.\r\n",
		)

		require.NoError(t, err)
		assert.Equal(t, "Doc", meta.Title)
		assert.Equal(t, "Body.\r\n", body)
	})

	t.Run("empty", func(t *testing.T) {
		meta, body, err := extractFrontMatterDocument(t, "---\n---\nBody.\n")

		require.NoError(t, err)
		assert.Equal(t, "page", meta.Type)
		assert.Equal(t, "Body.\n", body)
	})

	t.Run("no newline after the closing line", func(t *testing.T) {
		meta, body, err := extractFrontMatterDocument(t, "---\ntitle: Doc\n---")

		require.NoError(t, err)
		assert.Equal(t, "Doc", meta.Title)
		assert.Empty(t, body)
	})

	for name, document := range map[string]string{
		"no closing line":   "---\nspace: DOCS\n\nBody.\n",
		"opening line only": "---",
		"invalid YAML too":  "---\nspace: [DOCS\nBody.\n",
	} {
		t.Run("unterminated: "+name, func(t *testing.T) {
			_, _, err := extractFrontMatterDocument(t, document)

			require.EqualError(t, err, "unterminated YAML front matter")
		})
	}
}

// TestFrontMatterHeadersAfterIt covers the HTML headers that follow front
// matter, which are found in the document after it rather than in the whole
// file.
func TestFrontMatterHeadersAfterIt(t *testing.T) {
	meta, body, err := extractFrontMatterDocument(t,
		"---\nspace: DOCS\n---\n<!-- Macro: x\n     Template: y -->\n<!-- Title: Doc -->\n<!-- Label: one -->\n\nBody.\n",
	)

	require.NoError(t, err)
	assert.Equal(t, "DOCS", meta.Space)
	assert.Equal(t, "Doc", meta.Title)
	assert.Equal(t, []string{"one"}, meta.Labels)
	assert.Equal(t, "<!-- Macro: x\n     Template: y -->\n\nBody.\n", body)
}
