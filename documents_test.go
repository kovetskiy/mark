package mark

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// documentProject lays out a project with docs/page.md in it and a document
// outside it, and runs the test from the project.
func documentProject(t *testing.T) (server *confluencetest.Server, project, outside string) {
	t.Helper()

	server = confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	outsideDir := t.TempDir()
	outside = writeFile(t, outsideDir, "outside.md",
		"<!-- Space: DOCS -->\n<!-- Title: Outside -->\n\noutside\n")

	project = t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(project, "docs"), 0o755))
	writeFile(t, filepath.Join(project, "docs"), "page.md",
		"<!-- Space: DOCS -->\n<!-- Title: Page -->\n\ninside\n")
	t.Chdir(project)

	return server, project, outside
}

// TestRunRefusesDocumentLinkedOutsideProject: a document matched by --files is
// held to the boundary every file it names is held to, and fails on its own
// under --continue-on-error.
func TestRunRefusesDocumentLinkedOutsideProject(t *testing.T) {
	server, project, outside := documentProject(t)
	require.NoError(t, os.Symlink(outside, filepath.Join(project, "docs", "notes.md")))

	config := publishConfig(server.URL, "docs/*.md")
	config.ContinueOnError = true

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.ErrorIs(t, err, attachment.ErrOutsideProject)

	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
	assert.Equal(t, "docs/notes.md", filepath.ToSlash(fileErr.File))

	assert.Empty(t, bodyOfPageTitled(t, server, "Outside"))
	assert.Contains(t, bodyOfPageTitled(t, server, "Page"), "inside")
}

// TestRunRefusesDocumentUnderDirectoryLinkedOutsideProject: a glob follows a
// directory symlink, so the document's own directory is outside with it and
// the boundary is the working directory.
func TestRunRefusesDocumentUnderDirectoryLinkedOutsideProject(t *testing.T) {
	server, project, outside := documentProject(t)
	require.NoError(t, os.Symlink(filepath.Dir(outside), filepath.Join(project, "docs", "x")))

	err := Run(context.Background(), publishConfig(server.URL, "docs/**/*.md"))
	require.Error(t, err)
	assert.ErrorIs(t, err, attachment.ErrOutsideProject)
	assert.Empty(t, bodyOfPageTitled(t, server, "Outside"))
}

// TestRunPublishesDocumentLinkedInsideProject: a symlink that stays in the
// project is an ordinary way to publish a file under a second name.
func TestRunPublishesDocumentLinkedInsideProject(t *testing.T) {
	server, project, _ := documentProject(t)
	shared := filepath.Join(project, "shared")
	require.NoError(t, os.Mkdir(shared, 0o755))
	writeFile(t, shared, "alias.md", "<!-- Space: DOCS -->\n<!-- Title: Alias -->\n\nshared\n")
	require.NoError(t, os.Symlink(filepath.Join("..", "shared", "alias.md"), filepath.Join(project, "docs", "alias.md")))

	require.NoError(t, Run(context.Background(), publishConfig(server.URL, "docs/*.md")))
	assert.Contains(t, bodyOfPageTitled(t, server, "Alias"), "shared")
	assert.Contains(t, bodyOfPageTitled(t, server, "Page"), "inside")
}

// TestPublisherRefusesDocumentLinkedOutsideProject: a library caller's path is
// read the way Run reads a matched one.
func TestPublisherRefusesDocumentLinkedOutsideProject(t *testing.T) {
	server, project, outside := documentProject(t)
	link := filepath.Join(project, "docs", "notes.md")
	require.NoError(t, os.Symlink(outside, link))

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	_, err := publishOne(context.Background(), link, api, PublishConfig{Output: io.Discard})
	require.Error(t, err)
	assert.ErrorIs(t, err, attachment.ErrOutsideProject)

	var fileErr *FileError
	assert.True(t, errors.As(err, &fileErr))
	assert.Empty(t, bodyOfPageTitled(t, server, "Outside"))
}

// TestPublisherPublishesDocumentsNamedDirectly: an absolute path inside the
// working directory, and a document named outside it that is not a link out of
// its own directory, are both read as they always were.
func TestPublisherPublishesDocumentsNamedDirectly(t *testing.T) {
	server, project, outside := documentProject(t)
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	_, err := publishOne(context.Background(), filepath.Join(project, "docs", "page.md"), api, PublishConfig{Output: io.Discard})
	require.NoError(t, err)
	assert.Contains(t, bodyOfPageTitled(t, server, "Page"), "inside")

	_, err = publishOne(context.Background(), outside, api, PublishConfig{Output: io.Discard})
	require.NoError(t, err)
	assert.Contains(t, bodyOfPageTitled(t, server, "Outside"), "outside")
}
