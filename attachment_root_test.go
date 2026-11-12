package mark

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attachmentRootProject lays out a repository with one document per
// directory, the guide declaring a file beside the other document, and runs
// from the repository.
func attachmentRootProject(t *testing.T) (server *confluencetest.Server, guide string) {
	t.Helper()

	server = confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	repo := t.TempDir()
	for _, dir := range []string{"guide", "other-doc"} {
		require.NoError(t, os.Mkdir(filepath.Join(repo, dir), 0o755))
	}

	writeFile(t, filepath.Join(repo, "other-doc"), "file.pdf", "other")
	writeFile(t, filepath.Join(repo, "guide"), "own.pdf", "own")
	guide = writeFile(t, filepath.Join(repo, "guide"), "index.md",
		"<!-- Space: DOCS -->\n<!-- Title: Guide -->\n<!-- Attachment: own.pdf -->\n"+
			"<!-- Attachment: ../other-doc/file.pdf -->\n\nguide\n")
	t.Chdir(repo)

	return server, guide
}

// TestAttachmentRootDocumentRefusesAnotherDocumentsFile: with
// Rendering.AttachmentRoot set to attachment.BoundaryDocument, an Attachment:
// header naming a file beside another document fails the document with
// attachment.ErrOutsideProject, for Run and a Publisher alike, and nothing is
// published.
func TestAttachmentRootDocumentRefusesAnotherDocumentsFile(t *testing.T) {
	server, guide := attachmentRootProject(t)

	config := publishConfig(server.URL, "guide/*.md")
	config.Rendering.AttachmentRoot = attachment.BoundaryDocument

	err := Run(context.Background(), config)
	require.ErrorIs(t, err, attachment.ErrOutsideProject)
	assert.Empty(t, bodyOfPageTitled(t, server, "Guide"))

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	_, err = publishOne(context.Background(), guide, api, config.PublishConfig)
	require.ErrorIs(t, err, attachment.ErrOutsideProject)

	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
	assert.Empty(t, bodyOfPageTitled(t, server, "Guide"))
}

// TestAttachmentRootDefaultAllowsAnotherDocumentsFile: without a root the
// working directory is inside the boundary, as it always has been, and so is
// a directory root holding both documents.
func TestAttachmentRootDefaultAllowsAnotherDocumentsFile(t *testing.T) {
	for _, root := range []attachment.Boundary{"", "."} {
		server, _ := attachmentRootProject(t)

		config := publishConfig(server.URL, "guide/*.md")
		config.Rendering.AttachmentRoot = root
		require.NoError(t, Run(context.Background(), config), "root %q", root)

		found, err := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}).
			FindPage(context.Background(), "DOCS", "Guide", "page")
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Len(t, server.Attachments(found.ID), 2, "root %q", root)
	}
}

// TestAttachmentRootValidation: a directory root that is not there is refused
// before anything is published, naming the field and its flag, by Validate,
// Run and NewPublisher alike.
func TestAttachmentRootValidation(t *testing.T) {
	server, _ := attachmentRootProject(t)

	for _, root := range []attachment.Boundary{"missing", "guide/own.pdf"} {
		config := publishConfig(server.URL, "guide/*.md")
		config.Rendering.AttachmentRoot = root

		err := config.Validate()
		require.ErrorContains(t, err, "Rendering.AttachmentRoot (--attachment-root)", root)

		require.ErrorContains(t, Run(context.Background(), config), "Rendering.AttachmentRoot (--attachment-root)")

		api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
		_, err = NewPublisher(context.Background(), api, PublishConfig{
			Rendering: RenderingConfig{AttachmentRoot: root}, Output: io.Discard,
		})
		require.ErrorContains(t, err, "Rendering.AttachmentRoot (--attachment-root)")
	}
}

// TestAttachmentRootHoldsAMarkdownPageHeader: a Markdown page header is
// compiled from its own path, so its images are held to the root around its
// own directory: beside another document it passes the default boundary and
// fails attachment.BoundaryDocument.
func TestAttachmentRootHoldsAMarkdownPageHeader(t *testing.T) {
	for _, root := range []attachment.Boundary{"", attachment.BoundaryDocument} {
		server, _ := attachmentRootProject(t)
		require.NoError(t, os.WriteFile(filepath.Join("guide", "index.md"),
			[]byte("<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nguide\n"), 0o600))
		writeFile(t, "other-doc", "logo.png", "png")
		require.NoError(t, os.Mkdir("templates", 0o755))

		config := publishConfig(server.URL, "guide/*.md")
		config.Rendering.PageHeader = writeFile(t, "templates", "header.md", "![logo](../other-doc/logo.png)\n")
		config.Rendering.AttachmentRoot = root

		err := Run(context.Background(), config)
		if root == "" {
			require.NoError(t, err)
			continue
		}

		require.ErrorIs(t, err, attachment.ErrOutsideProject)
	}
}

// TestAttachmentRootRefusesAPageHeaderBeforePublishing: a Markdown page
// header naming a file outside the root is refused when it is loaded, as
// anything else wrong with it is, rather than by each document after its page
// is created.
func TestAttachmentRootRefusesAPageHeaderBeforePublishing(t *testing.T) {
	server, _ := attachmentRootProject(t)
	require.NoError(t, os.WriteFile(filepath.Join("guide", "index.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nguide\n"), 0o600))
	writeFile(t, "other-doc", "logo.png", "png")
	require.NoError(t, os.Mkdir("templates", 0o755))

	config := publishConfig(server.URL, "guide/*.md")
	config.Rendering.PageHeader = writeFile(t, "templates", "header.md", "![logo](../other-doc/logo.png)\n")
	config.Rendering.AttachmentRoot = attachment.BoundaryDocument

	err := Run(context.Background(), config)
	require.ErrorIs(t, err, attachment.ErrOutsideProject)
	assert.Zero(t, server.CountRequests("POST", "/content"), "no page is created")
}

// attachmentRootFS holds the guide and the other document of
// attachmentRootProject in an fs.FS.
func attachmentRootFS() fstest.MapFS {
	return fstest.MapFS{
		"guide/own.pdf":      {Data: []byte("own")},
		"other-doc/file.pdf": {Data: []byte("other")},
	}
}

// TestAttachmentRootDocumentHoldsADocumentInAnFS: a Document read from an
// fs.FS may name anything inside it by default, and under
// attachment.BoundaryDocument only what is in its own directory there, for
// Run and Publisher.PublishDocument alike.
func TestAttachmentRootDocumentHoldsADocumentInAnFS(t *testing.T) {
	doc := Document{
		Path: "guide/index.md",
		Content: []byte("<!-- Space: DOCS -->\n<!-- Title: Guide -->\n<!-- Attachment: own.pdf -->\n" +
			"<!-- Attachment: ../other-doc/file.pdf -->\n\nguide\n"),
		FS: attachmentRootFS(),
	}

	server, _ := docsSpace(t)
	t.Chdir(t.TempDir())
	require.NoError(t, Run(context.Background(), documentsConfig(server, doc)))
	assert.NotEmpty(t, bodyOfPageTitled(t, server, "Guide"))

	server, api := docsSpace(t)
	config := documentsConfig(server, doc)
	config.Rendering.AttachmentRoot = attachment.BoundaryDocument
	require.ErrorIs(t, Run(context.Background(), config), attachment.ErrOutsideProject)
	assert.Empty(t, bodyOfPageTitled(t, server, "Guide"))

	publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.PublishDocument(context.Background(), doc)
	require.ErrorIs(t, err, attachment.ErrOutsideProject)
	assert.Empty(t, bodyOfPageTitled(t, server, "Guide"))
}

// TestAttachmentRootDirectoryRefusesADocumentInAnFS: a directory root is a
// directory on disk, which holds nothing of a Document's FS, so such a
// Document is refused before anything is published, by Run and
// Publisher.PublishDocument alike, rather than having every file it names
// refused one at a time.
func TestAttachmentRootDirectoryRefusesADocumentInAnFS(t *testing.T) {
	server, api := docsSpace(t)
	t.Chdir(t.TempDir())

	doc := Document{
		Path:    "guide/index.md",
		Content: []byte("<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nguide\n"),
		FS:      attachmentRootFS(),
	}

	config := documentsConfig(server, doc)
	config.Rendering.AttachmentRoot = "."
	require.ErrorContains(t, Run(context.Background(), config), "fs.Sub")
	assert.Empty(t, bodyOfPageTitled(t, server, "Guide"))

	publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.PublishDocument(context.Background(), doc)
	require.ErrorContains(t, err, "fs.Sub")

	// A Document on disk is held to the directory like a file.
	doc.FS = nil
	config.Documents = []Document{doc}
	require.NoError(t, Run(context.Background(), config))
	assert.NotEmpty(t, bodyOfPageTitled(t, server, "Guide"))
}

// TestAttachmentRootDocumentKeepsLinksBetweenDocuments: a link to another
// document names its page, not a file to publish, so it resolves under
// attachment.BoundaryDocument as it does without, on disk and in an fs.FS.
func TestAttachmentRootDocumentKeepsLinksBetweenDocuments(t *testing.T) {
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	a := header + "<!-- Title: A -->\n\nSee [the reference](../reference/b.md).\n"
	b := header + "<!-- Title: B -->\n\nReference.\n"

	t.Run("in an fs.FS", func(t *testing.T) {
		server, _ := docsSpace(t)
		t.Chdir(t.TempDir())

		config := documentsConfig(server,
			Document{Path: "guides/a.md", Content: []byte(a), FS: fstest.MapFS{}},
			Document{Path: "reference/b.md", Content: []byte(b), FS: fstest.MapFS{}},
		)
		config.Rendering.AttachmentRoot = attachment.BoundaryDocument
		require.NoError(t, Run(context.Background(), config))

		assert.Contains(t, bodyOfPageTitled(t, server, "A"), "/x/")
	})

	t.Run("on disk", func(t *testing.T) {
		server, _ := docsSpace(t)
		t.Chdir(t.TempDir())
		for _, dir := range []string{"guides", "reference"} {
			require.NoError(t, os.Mkdir(dir, 0o755))
		}
		writeFile(t, "guides", "a.md", a)
		writeFile(t, "reference", "b.md", b)

		config := publishConfig(server.URL, "*/*.md")
		config.Rendering.AttachmentRoot = attachment.BoundaryDocument
		require.NoError(t, Run(context.Background(), config))

		assert.Contains(t, bodyOfPageTitled(t, server, "A"), "/x/")
	})
}
