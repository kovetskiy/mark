package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/report"
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
	require.ErrorIs(t, err, attachment.ErrOutsideProject)

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
	require.ErrorIs(t, err, attachment.ErrOutsideProject)
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
	require.ErrorIs(t, err, attachment.ErrOutsideProject)

	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
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

// TestPublisherPublishesLinkInsideCheckoutNamedThroughOtherPath: a checkout
// reached through a symlink (macOS's /tmp, a CI workspace) may be named by
// either spelling; a link staying inside it publishes whichever is used.
func TestPublisherPublishesLinkInsideCheckoutNamedThroughOtherPath(t *testing.T) {
	server, project, _ := documentProject(t)
	writeAt(t, project, filepath.Join("shared", "alias.md"), "<!-- Space: DOCS -->\n<!-- Title: Alias -->\n\nshared\n")
	require.NoError(t, os.Symlink(filepath.Join("..", "shared", "alias.md"), filepath.Join(project, "docs", "alias.md")))

	checkout := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, os.Symlink(project, checkout))
	t.Chdir(checkout)

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	_, err := publishOne(context.Background(), filepath.Join(project, "docs", "alias.md"), api, PublishConfig{Output: io.Discard})
	require.NoError(t, err)
	assert.Contains(t, bodyOfPageTitled(t, server, "Alias"), "shared")
}

// TestRunRefusesDocumentUnderDirectoryLinkedOutsideNamedFromSubdirectory: a
// run from a directory beside the documents holds them to the directory both
// sit in, so a directory link out of it is refused there too.
func TestRunRefusesDocumentUnderDirectoryLinkedOutsideNamedFromSubdirectory(t *testing.T) {
	server, project, outside := documentProject(t)
	require.NoError(t, os.Symlink(filepath.Dir(outside), filepath.Join(project, "docs", "x")))
	require.NoError(t, os.Mkdir(filepath.Join(project, "tools"), 0o755))
	t.Chdir(filepath.Join(project, "tools"))

	err := Run(context.Background(), publishConfig(server.URL, "../docs/**/*.md"))
	require.ErrorIs(t, err, attachment.ErrOutsideProject)
	assert.Empty(t, bodyOfPageTitled(t, server, "Outside"))
}

// TestRunPublishesLinkInsideProjectNamedFromSubdirectory: from a directory
// beside the documents, a link to another directory of the project publishes.
func TestRunPublishesLinkInsideProjectNamedFromSubdirectory(t *testing.T) {
	server, project, _ := documentProject(t)
	writeAt(t, project, filepath.Join("shared", "alias.md"), "<!-- Space: DOCS -->\n<!-- Title: Alias -->\n\nshared\n")
	require.NoError(t, os.Symlink(filepath.Join("..", "shared", "alias.md"), filepath.Join(project, "docs", "alias.md")))
	require.NoError(t, os.Mkdir(filepath.Join(project, "site"), 0o755))
	t.Chdir(filepath.Join(project, "site"))

	require.NoError(t, Run(context.Background(), publishConfig(server.URL, "../docs/*.md")))
	assert.Contains(t, bodyOfPageTitled(t, server, "Alias"), "shared")
}

// TestPublisherReadsDocumentAsChecked: a ".." after a directory link is read
// the way it was checked, from the directory the path names.
func TestPublisherReadsDocumentAsChecked(t *testing.T) {
	server, project, outside := documentProject(t)
	sub := filepath.Join(filepath.Dir(outside), "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.Symlink(sub, filepath.Join(project, "docs", "link")))

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	_, err := publishOne(context.Background(), "docs/link/../outside.md", api, PublishConfig{Output: io.Discard})
	require.Error(t, err)
	assert.Empty(t, bodyOfPageTitled(t, server, "Outside"))
}

// TestIncludePathOutsideTheProjectStillIncludes: the check on the documents a
// run publishes is not the one on what they include. A fragment found through
// an --include-path outside the project, named directly or through a link in
// the project, is still included.
func TestIncludePathOutsideTheProjectStillIncludes(t *testing.T) {
	for name, linked := range map[string]bool{
		"include path named directly":          false,
		"include path linked from the project": true,
	} {
		t.Run(name, func(t *testing.T) {
			server, project, _ := documentProject(t)
			shared := t.TempDir()
			writeFile(t, shared, "fragment.md", "shared fragment\n")
			writeFile(t, filepath.Join(project, "docs"), "with-include.md",
				"<!-- Space: DOCS -->\n<!-- Title: With Include -->\n\n<!-- Include: fragment.md -->\n")

			includePath := shared
			if linked {
				require.NoError(t, os.Symlink(shared, filepath.Join(project, "shared")))
				includePath = "shared"
			}

			config := publishConfig(server.URL, "docs/*.md")
			config.Rendering.IncludePath = includePath
			require.NoError(t, Run(context.Background(), config))

			assert.Contains(t, bodyOfPageTitled(t, server, "With Include"), "shared fragment")
			assert.Contains(t, bodyOfPageTitled(t, server, "Page"), "inside")
		})
	}
}

// documentsConfig is a tracked run of documents alone.
func documentsConfig(server *confluencetest.Server, documents ...Document) Config {
	config := trackingConfig(server, "")
	config.Documents = documents

	return config
}

// lookupKey reports the page the manifest records for key, read afresh.
func lookupKey(t *testing.T, server *confluencetest.Server, key string) (manifest.Entry, bool) {
	t.Helper()

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	entry, ok, err := manifest.NewStore(api).Lookup(context.Background(), "DOCS", key)
	require.NoError(t, err)

	return entry, ok
}

func TestDocumentFromMemoryPublishes(t *testing.T) {
	server, _ := docsSpace(t)

	config := documentsConfig(server, Document{
		Path:    "generated/doc.md",
		Content: []byte(markdownWithTitle("Generated")),
		FS:      fstest.MapFS{},
	})
	require.NoError(t, Run(context.Background(), config))

	assert.Contains(t, bodyOfPageTitled(t, server, "Generated"), "Some content.")
}

// TestDocumentIsKeyedByKey: the manifest remembers the Key, not the Path,
// which is what lets a caller render into a fresh temporary directory each
// run and still have the page found again.
func TestDocumentIsKeyedByKey(t *testing.T) {
	server, _ := docsSpace(t)

	first := Document{
		Path:    filepath.ToSlash(filepath.Join("tmp-run-1", "doc.md")),
		Key:     "guides/doc.md",
		Content: []byte(markdownWithTitle("First Title")),
		FS:      fstest.MapFS{},
	}
	require.NoError(t, Run(context.Background(), documentsConfig(server, first)))

	entry, ok := lookupKey(t, server, "guides/doc.md")
	require.True(t, ok, "the page is recorded under the Key")
	_, ok = lookupKey(t, server, first.Path)
	assert.False(t, ok, "and not under the Path")

	// Another run, another Path, a new title: the Key alone finds the page.
	second := first
	second.Path = "tmp-run-2/doc.md"
	second.Content = []byte(markdownWithTitle("Second Title"))
	require.NoError(t, Run(context.Background(), documentsConfig(server, second)))

	assert.Equal(t, "Second Title", server.Page(entry.PageID).Title, "retitled in place")
	assert.Equal(t, 0, countPagesTitled(t, server, "First Title"))
}

// TestDocumentsLinkToEachOther: a relative link between two documents that
// exist nowhere on disk becomes a link to the other's page, laid out by their
// Paths alone.
func TestDocumentsLinkToEachOther(t *testing.T) {
	server, _ := docsSpace(t)

	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	config := documentsConfig(server,
		Document{
			Path:    "guides/a.md",
			Content: []byte(header + "<!-- Title: A -->\n\nSee [the reference](../reference/b.md).\n"),
			FS:      fstest.MapFS{},
		},
		Document{
			Path:    "reference/b.md",
			Content: []byte(header + "<!-- Title: B -->\n\nReference.\n"),
			FS:      fstest.MapFS{},
		},
	)
	require.NoError(t, Run(context.Background(), config))

	body := bodyOfPageTitled(t, server, "A")
	assert.Contains(t, body, "/x/", "the link names B's page")
	assert.NotContains(t, body, "reference/b.md")
}

// TestFileLinksToDocument: a file on disk links to a document by its Path.
func TestFileLinksToDocument(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	file := writeFile(t, dir, "a.md", header+"<!-- Title: From File -->\n\nSee [generated](gen.md).\n")

	config := trackingConfig(server, file)
	config.Documents = []Document{{
		Path:    filepath.Join(dir, "gen.md"),
		Key:     "gen.md",
		Content: []byte(header + "<!-- Title: Generated -->\n\nGenerated.\n"),
	}}
	require.NoError(t, Run(context.Background(), config))

	body := bodyOfPageTitled(t, server, "From File")
	assert.Contains(t, body, "/x/")
	assert.NotContains(t, body, "gen.md")

	_, ok := lookupKey(t, server, "gen.md")
	assert.True(t, ok)
	_, ok = lookupKey(t, server, file)
	assert.True(t, ok, "the file keeps its own key")
}

// TestDocumentInFSLinksToARunFile: a document in an fs.FS links to a file
// the run publishes from disk, which its FS does not hold.
func TestDocumentInFSLinksToARunFile(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	t.Chdir(dir)

	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	writeAt(t, dir, "guide.md", header+"<!-- Title: Guide -->\n\nOn disk.\n")

	config := trackingConfig(server, "guide.md")
	config.Documents = []Document{{
		Path:    "generated/doc.md",
		Content: []byte(header + "<!-- Title: Generated -->\n\nSee [the guide](../guide.md).\n"),
		FS:      fstest.MapFS{},
	}}
	require.NoError(t, Run(context.Background(), config))

	body := bodyOfPageTitled(t, server, "Generated")
	assert.Contains(t, body, "/x/")
	assert.NotContains(t, body, "guide.md")
}

// TestDocumentReadsFilesFromItsFS: images, declared attachments and includes
// are read from the document's FS, around its Path, subdirectories and
// parents inside the FS included.
func TestDocumentReadsFilesFromItsFS(t *testing.T) {
	server, _ := docsSpace(t)

	files := fstest.MapFS{
		"guides/img/logo.png":  {Data: onePixelPNG()},
		"shared/data.txt":      {Data: []byte("data")},
		"guides/frag.md":       {Data: []byte("Included from the FS.\n")},
		"shared/macros/box.md": {Data: []byte("<b>{{ .Text }}</b>")},
	}

	config := documentsConfig(server, Document{
		Path: "guides/doc.md",
		Content: []byte("<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Assets -->\n" +
			"<!-- Attachment: ../shared/data.txt -->\n\n" +
			"![logo](img/logo.png)\n\n" +
			"<!-- Include: frag.md -->\n\n" +
			"[data](../shared/data.txt)\n\n" +
			"<!-- Macro: BOX-(\\w+)\n     Template: ../shared/macros/box.md\n     Text: ${1} -->\n\nBOX-hello\n"),
		FS: files,
	})
	require.NoError(t, Run(context.Background(), config))

	id := mustFind(t, server, "Assets")
	var names []string
	for _, stored := range server.Attachments(id) {
		names = append(names, stored.Filename)
	}
	assert.ElementsMatch(t, []string{"img_logo.png", ".._shared_data.txt"}, names)

	body := server.Page(id).Body
	assert.Contains(t, body, "Included from the FS.")
	assert.Contains(t, body, `ri:filename="img_logo.png"`)
	assert.Contains(t, body, "<b>hello</b>", "a macro template is read from the FS too")
}

// TestDocumentCannotReadOutsideItsFS: "../" above the FS's root is refused,
// as a path outside the project is for a file, and nothing on disk is read
// in its place.
func TestDocumentCannotReadOutsideItsFS(t *testing.T) {
	cases := map[string]string{
		"an image":      "![x](../../outside.png)\n",
		"an attachment": "<!-- Attachment: ../../outside.txt -->\n\nBody.\n",
		"an include":    "<!-- Include: ../../outside.md -->\n",
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)

			config := documentsConfig(server, Document{
				Path: "guides/doc.md",
				Content: []byte("<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Escapes -->\n" +
					body),
				FS: fstest.MapFS{"outside.png": {Data: onePixelPNG()}},
			})

			err := Run(context.Background(), config)
			require.Error(t, err)
			assert.ErrorIs(t, err, attachment.ErrOutsideProject)

			var fileErr *FileError
			require.ErrorAs(t, err, &fileErr)
			assert.Equal(t, "guides/doc.md", fileErr.File)
		})
	}
}

// TestDocumentIsReportedByPathAndSource: the report and a failure name the
// Path, and the Source the caller gave, with no SourcePath mapping.
func TestDocumentIsReportedByPathAndSource(t *testing.T) {
	server, _ := docsSpace(t)

	var out bytes.Buffer
	config := documentsConfig(server,
		Document{
			Path:    "guides/ok.md",
			Source:  "templates/ok.tmpl",
			Content: []byte(markdownWithTitle("Fine")),
			FS:      fstest.MapFS{},
		},
		Document{
			Path:    "guides/broken.md",
			Source:  "templates/broken.tmpl",
			Content: []byte("No headers at all.\n"),
			FS:      fstest.MapFS{},
		},
	)
	config.ContinueOnError = true
	config.Report.OutputFormat = report.FormatJSON
	config.Output = &out

	err := Run(context.Background(), config)

	var failed *FilesFailedError
	require.ErrorAs(t, err, &failed)
	require.Len(t, failed.Errors, 1)
	assert.Equal(t, "guides/broken.md", failed.Errors[0].File)
	assert.Equal(t, "templates/broken.tmpl", failed.Errors[0].Source)

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	sources := map[string]string{}
	for _, page := range parsed.Pages {
		sources[page.File] = page.Source
	}
	assert.Equal(t, map[string]string{
		"guides/ok.md":     "templates/ok.tmpl",
		"guides/broken.md": "templates/broken.tmpl",
	}, sources)
}

// TestDocumentsWithFiles: both publish in one run, each under its own key.
func TestDocumentsWithFiles(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "file.md", markdownWithTitle("From Disk"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Documents = []Document{{
		Path:    "generated/doc.md",
		Content: []byte(markdownWithTitle("From Memory")),
		FS:      fstest.MapFS{},
	}}
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, 1, countPagesTitled(t, server, "From Disk"))
	assert.Equal(t, 1, countPagesTitled(t, server, "From Memory"))

	_, ok := lookupKey(t, server, filepath.Join(dir, "file.md"))
	assert.True(t, ok)
	_, ok = lookupKey(t, server, "generated/doc.md")
	assert.True(t, ok)
}

// TestDocumentsRefuseDuplicates: two documents with one Path or one Key, or
// a document with a file's, would be one page published twice.
func TestDocumentsRefuseDuplicates(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "file.md", markdownWithTitle("File"))

	doc := func(path, key string) Document {
		return Document{Path: path, Key: key, Content: []byte(markdownWithTitle(path))}
	}

	for name, config := range map[string]Config{
		"one Path": documentsConfig(server, doc("a.md", "a"), doc("a.md", "b")),
		"one Key":  documentsConfig(server, doc("a.md", "k"), doc("b.md", "k")),
		"a file's Path": func() Config {
			c := trackingConfig(server, file)
			c.Documents = []Document{doc(file, "other")}
			return c
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, Run(context.Background(), config))
		})
	}
}

// documentOrphanRun publishes keep.md and gone.md as documents, then only
// keep.md, both times with configure applied, and returns the server and
// gone.md's page.
func documentOrphanRun(t *testing.T, configure func(*Config)) (*confluencetest.Server, string, error) {
	t.Helper()

	server, _ := docsSpace(t)
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	keep := Document{Path: "keep.md", Content: []byte(header + "<!-- Title: Keep -->\n\nKeep.\n"), FS: fstest.MapFS{}}
	gone := Document{Path: "gone.md", Content: []byte(header + "<!-- Title: Gone -->\n\nGone.\n"), FS: fstest.MapFS{}}

	config := documentsConfig(server, keep, gone)
	configure(&config)
	require.NoError(t, Run(context.Background(), config))

	goneID := mustFind(t, server, "Gone")

	config.Documents = []Document{keep}

	return server, goneID, Run(context.Background(), config)
}

func TestDocumentScopeDeletesItsOrphans(t *testing.T) {
	server, goneID, err := documentOrphanRun(t, func(c *Config) {
		c.DocumentScope = "generated"
		c.Tracking.OnOrphan = report.OnOrphanDelete
	})
	require.NoError(t, err)

	assert.True(t, server.Page(goneID).Trashed, "a document its scope no longer publishes is an orphan")
}

func TestDocumentsWithoutScopeAreNeverOrphans(t *testing.T) {
	server, goneID, err := documentOrphanRun(t, func(c *Config) {
		c.Files = ""
	})
	require.NoError(t, err)

	assert.False(t, server.Page(goneID).Trashed)

	_, known := lookupKey(t, server, "gone.md")
	assert.True(t, known, "and stays remembered")
}

// TestDocumentsWithoutScopeRefuseOnOrphan: a run of documents alone with no
// scope has nothing --on-orphan could act on, and says so rather than look.
func TestDocumentsWithoutScopeRefuseOnOrphan(t *testing.T) {
	server, _ := docsSpace(t)
	config := documentsConfig(server, Document{Path: "a.md", Content: []byte(markdownWithTitle("A"))})
	config.Tracking.OnOrphan = report.OnOrphanDelete

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DocumentScope")
	assert.Equal(t, 0, countPagesTitled(t, server, "A"), "refused before anything is published")
}

// TestUnscopedDocumentBesideFilesIsNotTheGlobsOrphan: alongside files,
// a document without a scope is not taken into the files' pattern, so a
// later run of the same pattern without it deletes nothing of it.
func TestUnscopedDocumentBesideFilesIsNotTheGlobsOrphan(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "file.md", markdownWithTitle("From Disk"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Tracking.OnOrphan = report.OnOrphanDelete
	config.Documents = []Document{{
		Path:    "generated/doc.md",
		Content: []byte(markdownWithTitle("From Memory")),
		FS:      fstest.MapFS{},
	}}
	require.NoError(t, Run(context.Background(), config))
	docID := mustFind(t, server, "From Memory")

	config.Documents = nil
	require.NoError(t, Run(context.Background(), config))

	assert.False(t, server.Page(docID).Trashed)
}

// TestEmptyPatternBesideDocumentsOrphansNothing: under CI a pattern that
// matches nothing is let through, and the documents published beside it must
// not make every page the pattern published look orphaned.
func TestEmptyPatternBesideDocumentsOrphansNothing(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "file.md", markdownWithTitle("From Disk"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Tracking.OnOrphan = report.OnOrphanDelete
	config.CI = true
	require.NoError(t, Run(context.Background(), config))
	fileID := mustFind(t, server, "From Disk")

	// A checkout that lost its files, with generated documents still there.
	require.NoError(t, os.Remove(filepath.Join(dir, "file.md")))
	config.Documents = []Document{{
		Path: "generated/doc.md", Content: []byte(markdownWithTitle("From Memory")), FS: fstest.MapFS{},
	}}
	require.NoError(t, Run(context.Background(), config))

	assert.False(t, server.Page(fileID).Trashed)
}

// TestDocumentsDoNotVouchForTheFilesOfTheirSpace: a pattern that still
// matches files elsewhere but published nothing in a space has its pages
// there kept, as a run of files alone keeps them, though documents of a
// scope published to that space.
func TestDocumentsDoNotVouchForTheFilesOfTheirSpace(t *testing.T) {
	server, _ := docsSpace(t)
	server.AddSpace("OTHER")
	other := server.AddPage("OTHER", "Other Home", "page", "")
	server.SetHomepage("OTHER", other.ID)

	dir := t.TempDir()
	writeFile(t, dir, "elsewhere.md", "<!-- Space: OTHER -->\n<!-- Title: Elsewhere -->\n\nElsewhere.\n")
	writeFile(t, dir, "file.md", markdownWithTitle("From Disk"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Tracking.OnOrphan = report.OnOrphanDelete
	config.DocumentScope = "generated"
	require.NoError(t, Run(context.Background(), config))
	fileID := mustFind(t, server, "From Disk")

	// A checkout that lost the files of one space, with generated documents
	// still published there.
	require.NoError(t, os.Remove(filepath.Join(dir, "file.md")))
	config.Documents = []Document{{
		Path: "generated/doc.md", Content: []byte(markdownWithTitle("From Memory")), FS: fstest.MapFS{},
	}}
	require.NoError(t, Run(context.Background(), config))

	assert.False(t, server.Page(fileID).Trashed)
}

// TestDocumentScopeIsNotAFilesPattern: a --files run whose pattern is
// spelled like a document scope still does not take the scope's pages for
// its own.
func TestDocumentScopeIsNotAFilesPattern(t *testing.T) {
	assert.NotEqual(t, manifest.Key("generated"), documentScopePattern("generated"))
	assert.NotEqual(t, manifest.Key(documentScopePattern("generated")), documentScopePattern("generated"),
		"no glob normalises to a scope")
}

func TestDocumentScopeRequiresTrackPages(t *testing.T) {
	server, _ := docsSpace(t)
	config := documentsConfig(server, Document{Path: "a.md", Content: []byte(markdownWithTitle("A"))})
	config.Tracking.TrackPages = false
	config.DocumentScope = "generated"

	require.Error(t, Run(context.Background(), config))
}

func TestDocumentsRefuseParentsFromPath(t *testing.T) {
	server, _ := docsSpace(t)
	config := documentsConfig(server, Document{Path: "a.md", Content: []byte(markdownWithTitle("A"))})
	config.ParentsFromPath = true

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ParentsFromPath")
}

func TestDocumentPathMustBeValidInsideItsFS(t *testing.T) {
	server, _ := docsSpace(t)
	config := documentsConfig(server, Document{
		Path: "../doc.md", Content: []byte(markdownWithTitle("A")), FS: fstest.MapFS{},
	})

	require.Error(t, Run(context.Background(), config))
}

func TestPublisherPublishesADocument(t *testing.T) {
	server, api := docsSpace(t)

	publisher, err := NewPublisher(context.Background(), api, publishConfig(server.URL, "").PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	files := fstest.MapFS{
		"guides/frag.md":  {Data: []byte("From the fragment.\n")},
		"guides/other.md": {Data: []byte(markdownWithTitle("Other"))},
	}
	_, err = publisher.PublishDocument(context.Background(), Document{
		Path: "guides/other.md", Content: files["guides/other.md"].Data, FS: files,
	})
	require.NoError(t, err)

	// A link is looked for in the document's FS: a Publisher has no run's
	// documents to look among first.
	target, err := publisher.PublishDocument(context.Background(), Document{
		Path:    "guides/doc.md",
		Content: []byte(markdownWithTitle("Published") + "\n<!-- Include: frag.md -->\n\nSee [other](./other.md).\n"),
		FS:      files,
	})
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Contains(t, server.Page(target.ID).Body, "From the fragment.")
	assert.NotContains(t, server.Page(target.ID).Body, "other.md", "the link became one to the page")

	_, err = publisher.PublishDocument(context.Background(), Document{
		Path: "guides/broken.md", Source: "templates/broken.tmpl", Content: []byte("No headers.\n"),
		FS: fstest.MapFS{},
	})
	var fileErr *FileError
	require.True(t, errors.As(err, &fileErr))
	assert.Equal(t, "guides/broken.md", fileErr.File)
	assert.Equal(t, "templates/broken.tmpl", fileErr.Source)
}

// renamedDocumentRun publishes one document under the Key old.md, then the
// same content under new.md, both under scope and titled from the filename,
// so that only the content ties the two together. It returns the page each
// key is recorded with.
func renamedDocumentRun(t *testing.T, scope string) (manifest.Entry, manifest.Entry, bool) {
	t.Helper()

	server, _ := docsSpace(t)
	content := []byte("<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n\nThe same body.\n")

	config := documentsConfig(server, Document{Path: "old.md", Content: content, FS: fstest.MapFS{}})
	config.Page.TitleFromFilename = true
	config.DocumentScope = scope
	require.NoError(t, Run(context.Background(), config))

	before, ok := lookupKey(t, server, "old.md")
	require.True(t, ok)

	config.Documents = []Document{{Path: "new.md", Content: content, FS: fstest.MapFS{}}}
	require.NoError(t, Run(context.Background(), config))

	after, ok := lookupKey(t, server, "new.md")
	require.True(t, ok)

	_, oldKept := lookupKey(t, server, "old.md")

	return before, after, oldKept
}

// TestScopedDocumentRenameKeepsThePage: within a scope, a new Key holding a
// document recorded under an old one is that document, as a renamed file is.
func TestScopedDocumentRenameKeepsThePage(t *testing.T) {
	before, after, oldKept := renamedDocumentRun(t, "generated")

	assert.Equal(t, before.PageID, after.PageID, "the page follows the document")
	assert.False(t, oldKept, "and the old key is handed over")
}

// TestUnscopedDocumentIsNeverTakenForARename: without a scope nothing says
// which other keys a document could have had, so a new key is a new page.
func TestUnscopedDocumentIsNeverTakenForARename(t *testing.T) {
	before, after, oldKept := renamedDocumentRun(t, "")

	assert.NotEqual(t, before.PageID, after.PageID)
	assert.True(t, oldKept)
}
