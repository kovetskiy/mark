package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"testing/fstest"

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
