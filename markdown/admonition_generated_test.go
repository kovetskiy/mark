package markdown

import (
	"context"
	"math/rand"
	"regexp"
	"testing"

	"github.com/kovetskiy/mark/v17/internal/admonitiongen"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/require"
)

// TestGeneratedAdmonitionDocumentsCompile compiles generated documents with
// the admonition feature on and requires of each a page Confluence takes,
// holding every word of the document's text once and in order.
//
// An admonition that ends on a line inside an HTML block leaves the block's
// opening tag in the box and its closing tag after it, which is not XML, so
// the documents are the ones whose HTML blocks only an admonition could cut:
// each written whole, and not where other indentation ends it early. The one
// such line the parser does take for a closing run, a run inside an HTML block
// that ends at a blank line, such as a <div>, is not written.
func TestGeneratedAdmonitionDocumentsCompile(t *testing.T) {
	const (
		seed      = 20240611
		documents = 400
	)

	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	options := admonitiongen.All()
	options.Unclosed, options.DivRuns, options.Ragged = false, false, false

	word := regexp.MustCompile(`\bw[0-9]+\b`)

	// A heading's words are its id as well.
	id := regexp.MustCompile(` id="[^"]*"`)

	for n := range documents {
		source := admonitiongen.New(rand.New(rand.NewSource(int64(seed+n))), options).Text

		out, _, err := CompileMarkdown(t.Context(), []byte(source), std, "test.md", Options{Features: []string{"mkdocsadmonitions"}})
		require.NoError(t, err, "seed %d, document %q", seed+n, source)
		require.NoError(t, CheckWellFormed(out), "seed %d, document %q, page %q", seed+n, source, out)
		require.Equal(t, word.FindAllString(source, -1), word.FindAllString(id.ReplaceAllString(out, ""), -1), "seed %d, document %q, page %q", seed+n, source, out)
	}
}
