package metadata

import (
	"os"
	"path"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractDocumentLeadingH1(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	filename = "testdata/header.md"

	markdown, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	actual := ExtractDocumentLeadingH1(markdown)

	assert.Equal(t, "a", actual)
}

func TestExtractMeta_FolderHeaders(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		expected *Meta
	}{
		{
			name: "single folder header",
			markdown: `<!-- Space: DOCS -->
<!-- Folder: API Documentation -->
<!-- Title: Authentication -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Folders:           []string{"API Documentation"},
				Title:             "Authentication",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
		{
			name: "multiple folder headers",
			markdown: `<!-- Space: DOCS -->
<!-- Folder: Backend -->
<!-- Folder: Services -->
<!-- Folder: Authentication -->
<!-- Title: Password Reset -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Folders:           []string{"Backend", "Services", "Authentication"},
				Title:             "Password Reset",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
		{
			name: "folder and parent headers mixed",
			markdown: `<!-- Space: DOCS -->
<!-- Folder: Backend -->
<!-- Folder: Services -->
<!-- Parent: User Management -->
<!-- Title: Password Reset -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Folders:           []string{"Backend", "Services"},
				Parents:           []string{"User Management"},
				Title:             "Password Reset",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
		{
			name: "no folder headers",
			markdown: `<!-- Space: DOCS -->
<!-- Parent: User Management -->
<!-- Title: Password Reset -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Parents:           []string{"User Management"},
				Title:             "Password Reset",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
		{
			name: "folder headers with spaces and special characters",
			markdown: `<!-- Space: DOCS -->
<!-- Folder: API Documentation & Examples -->
<!-- Folder: User's Guide -->
<!-- Title: Getting Started -->

# Content`,
			expected: &Meta{
				Space:             "DOCS",
				Folders:           []string{"API Documentation & Examples", "User's Guide"},
				Title:             "Getting Started",
				Type:              "page",
				ContentAppearance: "full-width",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, _, err := ExtractMeta([]byte(tt.markdown), "", false, nil, false)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, meta)
		})
	}
}

func TestExtractMeta_FolderHeadersOrder(t *testing.T) {
	markdown := `<!-- Space: DOCS -->
<!-- Folder: First -->
<!-- Folder: Second -->
<!-- Folder: Third -->
<!-- Title: Test Page -->

# Content`

	meta, _, err := ExtractMeta([]byte(markdown), "", false, nil, false)
	assert.NoError(t, err)
	assert.Equal(t, []string{"First", "Second", "Third"}, meta.Folders)
}

func TestExtractMeta_FolderHeadersWithCliParents(t *testing.T) {
	markdown := `<!-- Space: DOCS -->
<!-- Folder: API -->
<!-- Title: Test Page -->

# Content`

	cliParents := []string{"CLI Parent 1", "CLI Parent 2"}
	meta, _, err := ExtractMeta([]byte(markdown), "", false, cliParents, false)
	assert.NoError(t, err)
	
	// CLI parents should be prepended to any parents in the markdown, not folders
	assert.Equal(t, cliParents, meta.Parents)
	assert.Equal(t, []string{"API"}, meta.Folders)
}
