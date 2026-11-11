package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"strings"
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

// hierarchyDocument is a document in an empty FS at path holding body under a
// Space header.
func hierarchyDocument(path, body string) Document {
	return Document{Path: path, Content: []byte(space + body), FS: fstest.MapFS{}}
}

// documentHierarchyConfig is a tracked run of documents alone, laid out by
// their Paths under root.
func documentHierarchyConfig(server *confluencetest.Server, root string, documents ...Document) Config {
	config := documentsConfig(server, documents...)
	config.ParentsFromPath = true
	config.ParentsFromPathRoot = root

	return config
}

func hierarchyAPI(server *confluencetest.Server) *confluence.API {
	return confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
}

// TestDocumentsAreLaidOutByTheirPaths: documents in virtual directories go
// under pages standing for them, a README document titles its directory from
// its Content, and a .pages file on disk that happens to sit at a virtual
// directory's path is not read for it.
func TestDocumentsAreLaidOutByTheirPaths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAt(t, dir, "reference/api/.pages", "title: From Disk\n")

	server := hierarchyServer(t)
	config := documentHierarchyConfig(server, "reference",
		hierarchyDocument("reference/api/users.md", "<!-- Title: Users -->\n\nUsers.\n"),
		hierarchyDocument("reference/api/v2/groups.md", "<!-- Title: Groups -->\n\nGroups.\n"),
		hierarchyDocument("reference/guides/README.md", "<!-- Title: Handbook -->\n\nThe guides.\n"),
		hierarchyDocument("reference/guides/login.md",
			"<!-- Title: Login -->\n\nSee [users](../api/users.md).\n"),
	)
	require.NoError(t, Run(context.Background(), config))

	api := hierarchyAPI(server)
	assert.Equal(t, []string{"Home", "Api"}, ancestryOf(t, api, "Users"))
	assert.Equal(t, []string{"Home", "Api", "V2"}, ancestryOf(t, api, "Groups"))
	assert.Equal(t, []string{"Home"}, ancestryOf(t, api, "Handbook"), "the README is its directory's page")
	assert.Equal(t, []string{"Home", "Handbook"}, ancestryOf(t, api, "Login"))
	assert.Equal(t, 0, countPagesTitled(t, server, "From Disk"))

	assert.NotContains(t, bodyOfPageTitled(t, server, "Login"), "users.md",
		"a link to another document names its page")
}

// TestDocumentIndexWithoutATitleIsNamedByItsDirectory: a README document
// that names no title of its own takes its directory's, as a file does.
func TestDocumentIndexWithoutATitleIsNamedByItsDirectory(t *testing.T) {
	t.Chdir(t.TempDir())

	server := hierarchyServer(t)
	config := documentHierarchyConfig(server, "docs",
		hierarchyDocument("docs/getting-started/index.md", "\nWhere to begin.\n"),
		hierarchyDocument("docs/getting-started/install.md", "<!-- Title: Install -->\n\nInstall.\n"),
	)
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, []string{"Home", "Getting Started"}, ancestryOf(t, hierarchyAPI(server), "Install"))
	assert.Contains(t, bodyOfPageTitled(t, server, "Getting Started"), "Where to begin.")
}

// TestFilesAndDocumentsShareOneHierarchy: a directory files and documents
// both lie in is one page, recorded by its path as a directory of files is,
// and one only documents lie in is recorded under a key of the scope's own.
func TestFilesAndDocumentsShareOneHierarchy(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAt(t, dir, "docs/guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	server := hierarchyServer(t)
	config := trackingConfig(server, "docs/**/*.md")
	config.ParentsFromPath = true
	config.DocumentScope = "generated"
	config.Documents = []Document{
		hierarchyDocument("docs/guides/generated.md", "<!-- Title: Generated -->\n\nGenerated.\n"),
		hierarchyDocument("docs/reference/api.md", "<!-- Title: API -->\n\nAPI.\n"),
	}
	require.NoError(t, Run(context.Background(), config))

	api := hierarchyAPI(server)
	assert.Equal(t, []string{"Home", "Guides"}, ancestryOf(t, api, "Setup"))
	assert.Equal(t, []string{"Home", "Guides"}, ancestryOf(t, api, "Generated"))
	assert.Equal(t, 1, countPagesTitled(t, server, "Guides"))
	assert.Equal(t, []string{"Home", "Reference"}, ancestryOf(t, api, "API"))

	store := manifest.NewStore(api)
	guides, ok, err := store.Lookup(t.Context(), "DOCS", "docs/guides")
	require.NoError(t, err)
	require.True(t, ok, "a directory files lie in is recorded by its path")
	assert.Equal(t, pageIDTitled(t, server, "Guides"), guides.PageID)
	assert.Equal(t, "docs/**/*.md", guides.Glob)

	_, ok, err = store.Lookup(t.Context(), "DOCS", "docs/reference")
	require.NoError(t, err)
	assert.False(t, ok, "a directory of documents alone is not keyed by its path")

	reference, ok, err := store.Lookup(t.Context(), "DOCS", manifest.DocumentDirectoryKey("generated", "docs/reference"))
	require.NoError(t, err)
	require.True(t, ok, "but by a key of the scope's own")
	assert.Equal(t, pageIDTitled(t, server, "Reference"), reference.PageID)
	assert.True(t, manifest.IsDocumentScope(reference.Glob))
}

// TestDocumentsAloneNeedARootToBeLaidOut: with no --files pattern there is
// nothing to take the root from, and the run is refused before anything is
// published.
func TestDocumentsAloneNeedARootToBeLaidOut(t *testing.T) {
	server := hierarchyServer(t)
	config := documentHierarchyConfig(server, "",
		hierarchyDocument("reference/api/users.md", "<!-- Title: Users -->\n\nUsers.\n"))

	err := Run(context.Background(), config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ParentsFromPathRoot")
	assert.Contains(t, err.Error(), "--parents-from-path-root")
	assert.Equal(t, 0, countPagesTitled(t, server, "Users"))
}

// TestEmptiedDocumentDirectoryBecomesAnOrphanOfItsScope: the page standing
// for a directory of documents is the scope's, so once no document of the
// scope lies in it, it is an orphan like the documents were. A partial run
// given the scope's keys does not take it for gone.
func TestEmptiedDocumentDirectoryBecomesAnOrphanOfItsScope(t *testing.T) {
	t.Chdir(t.TempDir())

	server := hierarchyServer(t)
	users := hierarchyDocument("reference/api/users.md", "<!-- Title: Users -->\n\nUsers.\n")
	keep := hierarchyDocument("reference/keep.md", "<!-- Title: Keep -->\n\nKeep.\n")

	config := documentHierarchyConfig(server, "reference", users, keep)
	config.DocumentScope = "reference"
	config.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), config))

	apiID, usersID := pageIDTitled(t, server, "Api"), pageIDTitled(t, server, "Users")
	require.NotEmpty(t, apiID)

	// A partial run publishing only keep.md, naming users.md as still there.
	partial := config
	partial.Documents = []Document{keep}
	partial.Tracking.Partial = true
	partial.DocumentKeys = []string{users.Path}
	var out bytes.Buffer
	partial.Report.OutputFormat = report.FormatJSON
	partial.Output = &out
	require.NoError(t, Run(context.Background(), partial))

	var reported report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &reported))
	assert.Empty(t, reported.Orphans, "a listed key's directory is not gone")
	assert.False(t, server.Page(usersID).Trashed)
	assert.False(t, server.Page(apiID).Trashed)

	// users.md is gone from the scope: its page first, then the directory's.
	config.Documents = []Document{keep}
	require.NoError(t, Run(context.Background(), config))
	assert.True(t, server.Page(usersID).Trashed)
	require.NoError(t, Run(context.Background(), config))
	assert.True(t, server.Page(apiID).Trashed, "the page standing for the directory is an orphan too")
}

// TestDocumentDirectoryDoesNotSurviveForItsKeys: a partial run whose list
// names none of the scope's tracked keys removes nothing, though it
// published a new document into a directory the scope already had: the
// directory's page being there again says nothing of the keys.
func TestDocumentDirectoryDoesNotSurviveForItsKeys(t *testing.T) {
	t.Chdir(t.TempDir())

	server := hierarchyServer(t)
	config := documentHierarchyConfig(server, "reference",
		hierarchyDocument("reference/api/a.md", "<!-- Title: A -->\n\nFirst document.\n"),
		hierarchyDocument("reference/api/b.md", "<!-- Title: B -->\n\nSecond document.\n"),
	)
	config.DocumentScope = "reference"
	config.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), config))

	config.Documents = []Document{hierarchyDocument("reference/api/c.md", "<!-- Title: C -->\n\nThird document.\n")}
	config.Tracking.Partial = true
	config.DocumentKeys = []string{"reference/unrelated.md"}
	require.NoError(t, Run(context.Background(), config))

	for _, title := range []string{"A", "B", "Api"} {
		assert.False(t, server.Page(pageIDTitled(t, server, title)).Trashed, "%s is kept", title)
	}
}

// TestPartialRunLeavesADocumentDirectoryItLaysOut: a partial run lays out a
// directory for a document it publishes, but a document that declares its
// own parents never records it. The directory's page still holds a listed
// key's page, so it is neither reported nor warned about as gone.
func TestPartialRunLeavesADocumentDirectoryItLaysOut(t *testing.T) {
	t.Chdir(t.TempDir())

	server := hierarchyServer(t)
	config := documentHierarchyConfig(server, "reference",
		hierarchyDocument("reference/api/a.md", "<!-- Title: A -->\n\nFirst document.\n"),
		hierarchyDocument("reference/api/b.md", "<!-- Title: B -->\n\nSecond document.\n"),
	)
	config.DocumentScope = "reference"
	config.Tracking.OnOrphan = report.OnOrphanDelete
	require.NoError(t, Run(context.Background(), config))

	apiID := pageIDTitled(t, server, "Api")
	require.NotEmpty(t, apiID)

	config.Documents = []Document{
		hierarchyDocument("reference/api/a.md", "<!-- Parent: Home -->\n<!-- Title: A -->\n\nFirst document.\n"),
	}
	config.Tracking.Partial = true
	config.DocumentKeys = []string{"reference/api/b.md"}
	var out bytes.Buffer
	config.Report.OutputFormat = report.FormatJSON
	config.Output = &out
	require.NoError(t, Run(context.Background(), config))

	var reported report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &reported))
	assert.Empty(t, reported.Orphans)
	assert.False(t, server.Page(apiID).Trashed, "the directory B still lies in is kept")
	assert.False(t, server.Page(pageIDTitled(t, server, "B")).Trashed)
}

// TestIncludePathFragmentLinksToALaidOutFile: a fragment a document in an FS
// includes from --include-path links to a README the run lays out, found on
// disk under its absolute name; the link names the page the README is
// published as, titled by its directory.
func TestIncludePathFragmentLinksToALaidOutFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAt(t, dir, "docs/guides/README.md", space+"\nThe guides.\n")
	writeAt(t, dir, "docs/guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")
	writeAt(t, dir, "shared/fragment.md", "See [the guides](../docs/guides/README.md).\n")

	server := hierarchyServer(t)
	config := trackingConfig(server, "docs/**/*.md")
	config.ParentsFromPath = true
	config.Rendering.IncludePath = "shared"
	config.Documents = []Document{
		hierarchyDocument("docs/generated/doc.md", "<!-- Title: Overview -->\n\n<!-- Include: fragment.md -->\n"),
	}
	require.NoError(t, Run(context.Background(), config))

	guides := pageIDTitled(t, server, "Guides")
	require.NotEmpty(t, guides)
	link, err := confluence.GenerateTinyLink(server.URL, guides)
	require.NoError(t, err)
	assert.Contains(t, bodyOfPageTitled(t, server, "Overview"), link,
		"the fragment's link names the README's page")
}

// pagesLayout is a tree laid out twice by the tests below: as files on disk
// and as documents in an fstest.MapFS. guides has a README with a title and a
// .pages file, howto a README without one and a .pages file, and reference a
// .pages file alone.
var pagesLayout = map[string]string{
	"docs/guides/.pages":      "title: Guides From Pages\n",
	"docs/guides/README.md":   space + "<!-- Title: Handbook -->\n\nThe guides.\n",
	"docs/guides/setup.md":    space + "<!-- Title: Setup -->\n\nSetup.\n",
	"docs/howto/.pages":       "title: How To From Pages\n",
	"docs/howto/README.md":    space + "\nHow to do things.\n",
	"docs/howto/deploy.md":    space + "<!-- Title: Deploy -->\n\nDeploy.\n",
	"docs/reference/.pages":   "title: API Reference\n",
	"docs/reference/users.md": space + "<!-- Title: Users -->\n\nUsers.\n",
}

// pagesLayoutAncestries is where the layout's leaf pages end up.
func pagesLayoutAncestries(t *testing.T, server *confluencetest.Server) map[string][]string {
	t.Helper()

	api := hierarchyAPI(server)
	ancestries := map[string][]string{}
	for _, title := range []string{"Setup", "Deploy", "Users"} {
		ancestries[title] = ancestryOf(t, api, title)
	}

	return ancestries
}

// TestPagesFileInADocumentsFSTitlesItsDirectory: a .pages file is read from
// the FS of the documents a directory holds, with the precedence it has on
// disk: a README's own title first, then the .pages file, then the
// directory's name. Laid out from files on disk and from documents in an FS,
// the same tree gets the same parents.
func TestPagesFileInADocumentsFSTitlesItsDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	fsys := fstest.MapFS{}
	var documents []Document
	for name, content := range pagesLayout {
		writeAt(t, dir, "disk/"+name, content)
		if strings.HasSuffix(name, ".md") {
			documents = append(documents, Document{Path: name, Content: []byte(content), FS: fsys})
		} else {
			fsys[name] = &fstest.MapFile{Data: []byte(content)}
		}
	}

	diskServer := hierarchyServer(t)
	diskConfig := trackingConfig(diskServer, "disk/docs/**/*.md")
	diskConfig.ParentsFromPath = true
	require.NoError(t, Run(context.Background(), diskConfig))

	documentServer := hierarchyServer(t)
	require.NoError(t, Run(context.Background(), documentHierarchyConfig(documentServer, "docs", documents...)))

	want := map[string][]string{
		"Setup":  {"Home", "Handbook"},
		"Deploy": {"Home", "How To From Pages"},
		"Users":  {"Home", "API Reference"},
	}
	assert.Equal(t, want, pagesLayoutAncestries(t, diskServer), "on disk")
	assert.Equal(t, want, pagesLayoutAncestries(t, documentServer), "in an FS")
	assert.Contains(t, bodyOfPageTitled(t, documentServer, "How To From Pages"), "How to do things.",
		"a README without a title is its directory's page under the .pages title")
}

// TestPagesFileOnDiskBeatsADocumentsFS: a directory a file lies in is on
// disk, and its .pages file is read there, not from the FS of a document
// beside the file.
func TestPagesFileOnDiskBeatsADocumentsFS(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAt(t, dir, "docs/guides/.pages", "title: From Disk\n")
	writeAt(t, dir, "docs/guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	server := hierarchyServer(t)
	config := trackingConfig(server, "docs/**/*.md")
	config.ParentsFromPath = true
	config.DocumentScope = "generated"
	config.Documents = []Document{{
		Path:    "docs/guides/generated.md",
		Content: []byte(space + "<!-- Title: Generated -->\n\nGenerated.\n"),
		FS:      fstest.MapFS{"docs/guides/.pages": {Data: []byte("title: From The FS\n")}},
	}}
	require.NoError(t, Run(context.Background(), config))

	api := hierarchyAPI(server)
	assert.Equal(t, []string{"Home", "From Disk"}, ancestryOf(t, api, "Generated"))
	assert.Equal(t, 0, countPagesTitled(t, server, "From The FS"))
}

// TestPagesFileLinkingOutOfADocumentsFSIsRefused: a .pages file is read as
// any file of the FS is, so a symbolic link out of it, the file's own or a
// directory's above it, is refused rather than followed or taken for absent.
func TestPagesFileLinkingOutOfADocumentsFSIsRefused(t *testing.T) {
	link := func(target string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
	}

	for name, fsys := range map[string]fstest.MapFS{
		"a .pages above the root": {"docs/guides/.pages": link("../../../.pages")},
		"an absolute .pages":      {"docs/guides/.pages": link("/etc/mark/.pages")},
		"a directory linked out":  {"docs/guides": link("../../elsewhere")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			server := hierarchyServer(t)
			err := Run(context.Background(), documentHierarchyConfig(server, "docs", Document{
				Path:    "docs/guides/setup.md",
				Content: []byte(space + "<!-- Title: Setup -->\n\nSetup.\n"),
				FS:      fsys,
			}))
			require.Error(t, err)
			require.ErrorIs(t, err, attachment.ErrOutsideProject)
			assert.Contains(t, err.Error(), ".pages")
			assert.Equal(t, 0, countPagesTitled(t, server, "Setup"))
		})
	}
}

// TestPagesFileUnderAFileInADocumentsFSIsAbsent: a Document's Path need not
// exist in its FS, so a directory of it that is a file there has no .pages
// file, as a directory that is not there has none, rather than failing the
// run.
func TestPagesFileUnderAFileInADocumentsFSIsAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir())
	writeAt(t, dir, "generated", "a file, not a directory\n")

	server := hierarchyServer(t)
	require.NoError(t, Run(context.Background(), documentHierarchyConfig(server, "generated", Document{
		Path:    "generated/api/users.md",
		Content: []byte(space + "<!-- Title: Users -->\n\nUsers.\n"),
		FS:      os.DirFS(dir),
	})))

	assert.Equal(t, []string{"Home", "Api"}, ancestryOf(t, hierarchyAPI(server), "Users"))
}
