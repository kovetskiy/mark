package mark

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// titledDoc is a page document under DOCS > Parent with a body of its own.
func titledDoc(title, body string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: " + title + " -->\n\n" + body + "\n"
}

// TestATitleChangedOnlyInCaseIsRetitled: the title lookup ignores case, so
// the page is found; it still takes the title as the document now writes it,
// also when the body has not changed.
func TestATitleChangedOnlyInCaseIsRetitled(t *testing.T) {
	for name, changesOnly := range map[string]bool{"every run writes": false, "changes only": true} {
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			file := writeFile(t, dir, "doc.md", titledDoc("Api guide", "Body."))

			config := publishConfig(server.URL, file)
			config.Update.ChangesOnly = changesOnly
			require.NoError(t, Run(context.Background(), config))

			id := pageIDTitled(t, server, "Api guide")
			require.NotEmpty(t, id)

			writeFile(t, dir, "doc.md", titledDoc("API Guide", "Body."))
			require.NoError(t, Run(context.Background(), config))

			assert.Equal(t, "API Guide", server.Page(id).Title)

			version := server.Page(id).Version
			require.NoError(t, Run(context.Background(), config))
			if changesOnly {
				assert.Equal(t, version, server.Page(id).Version, "a title already as written is not written again")
			}
		})
	}
}
