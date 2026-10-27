package confluence

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFolderCQLEscapesTheTitle: FindFolder and searchFolders send the same
// search, and a title is a quoted CQL string in it.
func TestFolderCQLEscapesTheTitle(t *testing.T) {
	assert.Equal(t,
		`type=folder AND title="Guides" AND space="DOCS"`,
		folderCQL("DOCS", "Guides"))
	assert.Equal(t,
		`type=folder AND title="say \"hi\" \\ bye" AND space="DOCS"`,
		folderCQL("DOCS", `say "hi" \ bye`))
}

// TestWithListingContextFillsOnlyWhatIsMissing: a listing names the context
// path once for all its attachments, and an attachment that names its own
// keeps it.
func TestWithListingContextFillsOnlyWhatIsMissing(t *testing.T) {
	attachments := listing[AttachmentInfo]{Results: make([]AttachmentInfo, 2)}
	attachments.Links.Context = "/wiki"
	attachments.Results[1].Links.Context = "/own"

	withListingContext(&attachments)

	assert.Equal(t, "/wiki", attachments.Results[0].Links.Context)
	assert.Equal(t, "/own", attachments.Results[1].Links.Context)
}
