package mark

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTitleLengthLimit: Confluence takes a title of at most 255 characters,
// counted in UTF-16 code units, and answers a longer one with a 400 once the
// page's parents have been created. mark refuses it before anything is asked
// of Confluence, dry run included.
func TestTitleLengthLimit(t *testing.T) {
	for _, test := range []struct {
		name, title string
		want        string
	}{
		{"256 letters", strings.Repeat("a", 256), "is 256 characters long"},
		// 128 characters, each two UTF-16 code units.
		{"128 emoji", strings.Repeat("🚀", 128), "is 256 characters long"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, dryRun := range []bool{true, false} {
				server, api := docsSpace(t)
				file := writeFile(t, t.TempDir(), "doc.md", markdownWithTitle(test.title))

				config := trackingConfig(server, file)
				config.DryRun = dryRun
				err := Run(context.Background(), config)
				require.Error(t, err, "dry run: %v", dryRun)
				assert.Contains(t, err.Error(), "page title")
				assert.Contains(t, err.Error(), test.want)

				published, err := api.FindPage(context.Background(), "DOCS", test.title, "page")
				require.NoError(t, err)
				assert.Nil(t, published)
			}
		})
	}

	t.Run("255 letters", func(t *testing.T) {
		server, api := docsSpace(t)
		title := strings.Repeat("a", 255)
		file := writeFile(t, t.TempDir(), "doc.md", markdownWithTitle(title))

		require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

		published, err := api.FindPage(context.Background(), "DOCS", title, "page")
		require.NoError(t, err)
		assert.NotNil(t, published)
	})

	t.Run("parent", func(t *testing.T) {
		server, api := docsSpace(t)
		parent := strings.Repeat("p", 256)
		file := writeFile(t, t.TempDir(), "doc.md",
			strings.Replace(markdownWithTitle("Child"), "<!-- Parent: Parent -->", "<!-- Parent: "+parent+" -->", 1))

		err := Run(context.Background(), trackingConfig(server, file))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parent title")

		created, err := api.FindPage(context.Background(), "DOCS", parent, "page")
		require.NoError(t, err)
		assert.Nil(t, created, "the parent is not created")
	})
}

// TestHeadersDifferingOnlyInCaseKeepThePage: headers that differ from the
// stored titles only in case still name the same page and ancestor, so the
// page is updated where it is rather than moved up under "Home".
func TestHeadersDifferingOnlyInCaseKeepThePage(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	before, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, before)
	parentID := server.Page(before.ID).ParentID

	recased := strings.NewReplacer(
		"<!-- Parent: Parent -->", "<!-- Parent: home -->",
		"<!-- Title: Doc -->", "<!-- Title: doc -->",
	).Replace(markdownWithTitle("Doc"))
	writeFile(t, dir, "doc.md", recased)
	require.NoError(t, Run(context.Background(), config))

	after, err := api.FindPage(context.Background(), "DOCS", "doc", "page")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, before.ID, after.ID, "the page the document published is the one it names")
	assert.Equal(t, parentID, server.Page(after.ID).ParentID, "a page under the declared ancestor stays where it is")
}
