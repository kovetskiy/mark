package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parentDoc(title string) string {
	return "<!-- Space: DOCS -->\n<!-- Title: " + title + " -->\n\n# " + title + "\n\nParent body.\n"
}

func childDoc(parent string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: " + parent + " -->\n<!-- Title: Child -->\n\n# Child\n\nChild body.\n"
}

// pageIDTitled looks a title up through a client of its own, because a shared
// one memoises FindPage and would answer from before the run.
func pageIDTitled(t *testing.T, server *confluencetest.Server, title string) string {
	t.Helper()
	pg, err := confluence.NewAPI(server.URL, "user", "token", false).FindPage("DOCS", title, "page")
	require.NoError(t, err)
	if pg == nil {
		return ""
	}
	return pg.ID
}

// TestRetitledParentAndChildInOneRun: a document and the child naming it as
// parent are retitled together, and the child is processed first. The child's
// parent title exists nowhere yet, so a page was created under it, and the
// parent document then found that page by title and published there instead of
// renaming the page it tracks -- leaving the original empty and every child
// moved.
func TestRetitledParentAndChildInOneRun(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "parent.md", parentDoc("Parent"))
	writeFile(t, dir, "a-child.md", childDoc("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent")
	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, parentID)
	require.NotEmpty(t, childID)

	writeFile(t, dir, "parent.md", parentDoc("Parent Renamed"))
	writeFile(t, dir, "a-child.md", childDoc("Parent Renamed"))
	require.NoError(t, Run(config))

	assert.Equal(t, parentID, pageIDTitled(t, server, "Parent Renamed"), "the tracked page should be renamed in place")
	assert.Empty(t, pageIDTitled(t, server, "Parent"), "the old title should be gone")
	assert.Equal(t, childID, pageIDTitled(t, server, "Child"))
	assert.Equal(t, parentID, server.Page(childID).ParentID, "the child should stay under the original page")
}

// TestRetitledParentNamedWithoutRegardToCase: the child spells the new parent
// title in another case, which Confluence reads as the same title.
func TestRetitledParentNamedWithoutRegardToCase(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "parent.md", parentDoc("Parent"))
	writeFile(t, dir, "a-child.md", childDoc("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent")
	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, parentID)

	writeFile(t, dir, "parent.md", parentDoc("Parent Renamed"))
	writeFile(t, dir, "a-child.md", childDoc("parent renamed"))
	require.NoError(t, Run(config))

	assert.Equal(t, "Parent Renamed", server.Page(parentID).Title)
	assert.Equal(t, parentID, server.Page(childID).ParentID)
}

// TestChildNamingOldParentTitleStillFollowsRename keeps refreshStaleParents
// working: only the parent is retitled, the child still names the old title.
func TestChildNamingOldParentTitleStillFollowsRename(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "parent.md", parentDoc("Parent"))
	writeFile(t, dir, "a-child.md", childDoc("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent")
	require.NotEmpty(t, parentID)

	writeFile(t, dir, "parent.md", parentDoc("Parent Renamed"))
	require.NoError(t, Run(config))

	assert.Equal(t, "Parent Renamed", server.Page(parentID).Title)
	assert.Empty(t, pageIDTitled(t, server, "Parent"))
}
