package mark

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A page below its declared parent, but under a page between the two, counts
// as placed unless DirectParent is set, which moves it directly under the
// parent. It is what a page is left under when the document that published
// the page between them is deleted.
func TestDirectParentMovesAPageNestedBelowItsParent(t *testing.T) {
	for name, tc := range map[string]struct {
		direct bool
		moved  bool
	}{
		"off": {false, false},
		"on":  {true, true},
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)
			parent := mustFind(t, server, "Parent")
			middle := server.AddPage("DOCS", "Middle", "page", parent)
			doc := server.AddPage("DOCS", "Doc", "page", middle.ID)

			dir := t.TempDir()
			file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nbody\n")
			config := changesOnlyConfig(server, file)
			config.Page.DirectParent = tc.direct

			require.NoError(t, Run(context.Background(), config))
			if tc.moved {
				assert.Equal(t, parent, server.Page(doc.ID).ParentID)
			} else {
				assert.Equal(t, middle.ID, server.Page(doc.ID).ParentID)
			}
		})
	}
}

// A dry run reports the move DirectParent would make, and makes none.
func TestDirectParentDryRunReportsTheMove(t *testing.T) {
	server, _ := docsSpace(t)
	parent := mustFind(t, server, "Parent")
	middle := server.AddPage("DOCS", "Middle", "page", parent)
	doc := server.AddPage("DOCS", "Doc", "page", middle.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nbody\n")
	config := changesOnlyConfig(server, file)
	config.Page.DirectParent = true
	config.Mode.DryRun = true
	config.Report.OutputFormat = "json"
	out := &bytes.Buffer{}
	config.Output = out

	require.NoError(t, Run(context.Background(), config))
	assert.Contains(t, out.String(), "moved")
	assert.Equal(t, middle.ID, server.Page(doc.ID).ParentID, "a dry run must not move the page")
}
