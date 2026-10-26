package mark

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parentDoc(title string) string {
	return "<!-- Space: DOCS -->\n<!-- Title: " + title + " -->\n\n# " + title + "\n\nParent body.\n"
}

func childDoc(parent string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: " + parent + " -->\n<!-- Title: Child -->\n\n# Child\n\nChild body.\n"
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

// TestRetitledParentAndChildWithGeneratedHash: under
// --title-append-generated-hash a page is published as "Title - <hash>", and the
// child names the parent by that title. The run's index has to hold the hashed
// title too, with and without a path hierarchy, or the child sorting first
// creates an empty page under the new name.
func TestRetitledParentAndChildWithGeneratedHash(t *testing.T) {
	hashed := func(title string) string {
		return metadata.GeneratedHashTitle(nil, "DOCS", title)
	}

	for name, tweak := range map[string]func(*Config, string){
		"flat":      func(*Config, string) {},
		"hierarchy": func(c *Config, dir string) { c.ParentsFromPath = true; c.ParentsFromPathRoot = dir },
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			writeFile(t, dir, "parent.md", parentDoc("Parent"))
			writeFile(t, dir, "a-child.md", childDoc(hashed("Parent")))
			config := trackingConfig(server, dir+"/*.md")
			config.TitleAppendGeneratedHash = true
			tweak(&config, dir)
			require.NoError(t, Run(config))

			parentID := pageIDTitled(t, server, hashed("Parent"))
			childID := pageIDTitled(t, server, metadata.GeneratedHashTitle([]string{hashed("Parent")}, "DOCS", "Child"))
			require.NotEmpty(t, parentID)
			require.NotEmpty(t, childID)

			writeFile(t, dir, "parent.md", parentDoc("Parent Renamed"))
			writeFile(t, dir, "a-child.md", childDoc(hashed("Parent Renamed")))
			require.NoError(t, Run(config))

			assert.Equal(t, parentID, pageIDTitled(t, server, hashed("Parent Renamed")), "renamed in place")
			assert.Empty(t, pageIDTitled(t, server, hashed("Parent")))
			assert.Equal(t, parentID, server.Page(childID).ParentID, "child stays under the original page")
		})
	}
}

// TestRetitledParentByRenamingItsFile: the parent takes its title from its
// filename, so renaming the file is what retitles it. Its path is not recorded
// yet, and the page it published is found by its content, as the parent's own
// turn will find it.
func TestRetitledParentByRenamingItsFile(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	parent := "<!-- Space: DOCS -->\n\nParent body.\n"
	writeFile(t, dir, "parent-doc.md", parent)
	writeFile(t, dir, "a-child.md", childDoc("Parent Doc"))
	config := trackingConfig(server, dir+"/*.md")
	config.TitleFromFilename = true
	require.NoError(t, Run(config))

	parentID := pageIDTitled(t, server, "Parent Doc")
	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, parentID)
	require.NotEmpty(t, childID)

	require.NoError(t, os.Rename(filepath.Join(dir, "parent-doc.md"), filepath.Join(dir, "renamed-doc.md")))
	writeFile(t, dir, "a-child.md", childDoc("Renamed Doc"))
	require.NoError(t, Run(config))

	assert.Equal(t, parentID, pageIDTitled(t, server, "Renamed Doc"), "renamed in place")
	assert.Empty(t, pageIDTitled(t, server, "Parent Doc"))
	assert.Equal(t, parentID, server.Page(childID).ParentID, "child stays under the original page")
}

// TestRetitledBlogPostIsNotAParent: a blog post retitled to the title a child
// now names is not that child's parent. The parent is a page that does not
// exist yet, and is created.
func TestRetitledBlogPostIsNotAParent(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	post := func(title string) string {
		return "<!-- Space: DOCS -->\n<!-- Type: blogpost -->\n<!-- Title: " + title + " -->\n\nNews.\n"
	}
	writeFile(t, dir, "news.md", post("News"))
	writeFile(t, dir, "a-child.md", childDoc("Parent"))
	config := trackingConfig(server, dir+"/*.md")
	require.NoError(t, Run(config))

	childID := pageIDTitled(t, server, "Child")
	require.NotEmpty(t, childID)

	writeFile(t, dir, "news.md", post("Section"))
	writeFile(t, dir, "a-child.md", childDoc("Section"))
	require.NoError(t, Run(config))

	sectionID := pageIDTitled(t, server, "Section")
	require.NotEmpty(t, sectionID, "the parent the child names is created")
	assert.Equal(t, sectionID, server.Page(childID).ParentID)
	assert.Empty(t, pageIDTitled(t, server, "News"), "no page under the blog post's old title")
}

// TestRetitledDirectoryDocument: with a path hierarchy, a directory's README
// titles the directory, and the documents beside it name that title as their
// parent. API.md sorts ahead of README.md, so it looks for the new title
// before the README has retitled its page.
func TestRetitledDirectoryDocument(t *testing.T) {
	readme := func(title string) string {
		return "<!-- Space: DOCS -->\n<!-- Title: " + title + " -->\n\nGuide.\n"
	}
	api := "<!-- Space: DOCS -->\n<!-- Title: API -->\n\nAPI.\n"

	for name, hash := range map[string]bool{"plain": false, "generated hash": true} {
		t.Run(name, func(t *testing.T) {
			guideTitle := func(title string) string {
				if hash {
					return metadata.GeneratedHashTitle(nil, "DOCS", title)
				}
				return title
			}
			apiTitle := "API"
			if hash {
				apiTitle = metadata.GeneratedHashTitle([]string{guideTitle("Guide")}, "DOCS", "API")
			}

			server, _ := docsSpace(t)
			dir := t.TempDir()
			writeAt(t, dir, "guide/README.md", readme("Guide"))
			writeAt(t, dir, "guide/API.md", api)
			config := trackingConfig(server, filepath.Join(dir, "**", "*.md"))
			config.ParentsFromPath = true
			config.ParentsFromPathRoot = dir
			config.TitleAppendGeneratedHash = hash
			require.NoError(t, Run(config))

			guideID := pageIDTitled(t, server, guideTitle("Guide"))
			apiID := pageIDTitled(t, server, apiTitle)
			require.NotEmpty(t, guideID)
			require.NotEmpty(t, apiID)
			require.Equal(t, guideID, server.Page(apiID).ParentID)

			writeAt(t, dir, "guide/README.md", readme("Guide Renamed"))
			require.NoError(t, Run(config))

			assert.Equal(t, guideID, pageIDTitled(t, server, guideTitle("Guide Renamed")), "renamed in place")
			assert.Empty(t, pageIDTitled(t, server, guideTitle("Guide")))
			assert.Equal(t, guideID, server.Page(apiID).ParentID, "API stays under the original page")
		})
	}
}
