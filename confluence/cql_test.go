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
