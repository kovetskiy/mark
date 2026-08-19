package mark

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hierarchyServer(t *testing.T) *confluencetest.Server {
	t.Helper()
	s := confluencetest.New(t)
	home := s.AddPage("DOCS", "Home", "page", "")
	s.SetHomepage("DOCS", home.ID)
	return s
}

// writeAt writes a file, creating the directories leading to it.
func writeAt(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// ancestryOf reports the titles a page sits under, outermost first.
func ancestryOf(t *testing.T, api *confluence.API, title string) []string {
	t.Helper()

	page, err := api.FindPage("DOCS", title, "page")
	require.NoError(t, err)
	require.NotNil(t, page, "page %q should exist", title)

	found, err := api.GetPageByIDExpanded(page.ID, "ancestors")
	require.NoError(t, err)

	var titles []string
	for _, ancestor := range found.Ancestors {
		titles = append(titles, ancestor.Title)
	}

	return titles
}

func runHierarchy(t *testing.T, dir string, extra func(*Config)) *confluence.API {
	t.Helper()

	server := hierarchyServer(t)
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "**", "*.md"), Features: []string{"mention"},
		ParentsFromPath: true, ParentsFromPathRoot: dir, Output: io.Discard,
	}
	if extra != nil {
		extra(&config)
	}
	require.NoError(t, Run(config))

	return confluence.NewAPI(server.URL, "user", "token", false)
}

const space = "<!-- Space: DOCS -->\n"

// TestPathBecomesTheParentChain is the feature: where a file sits is where its
// page goes.
func TestPathBecomesTheParentChain(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/deep/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, nil)

	assert.Equal(t, []string{"Home", "Guides", "Deep"}, ancestryOf(t, api, "Setup"))
}

// TestIndexIsItsDirectorysPage: a directory's own document is its landing page,
// not a child of an empty one.
func TestIndexIsItsDirectorysPage(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"\nWhat the guides are.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, nil)

	// The README took its title from the directory and sits at the top.
	assert.Equal(t, []string{"Home"}, ancestryOf(t, api, "Guides"))
	assert.Equal(t, []string{"Home", "Guides"}, ancestryOf(t, api, "Setup"))

	page, err := api.FindPage("DOCS", "Guides", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
}

// TestDeclaredParentWins: an author who wrote a Parent header meant it.
func TestDeclaredParentWins(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md",
		space+"<!-- Title: Setup -->\n<!-- Parent: Elsewhere -->\n\nSetup.\n")

	api := runHierarchy(t, dir, nil)

	assert.Equal(t, []string{"Home", "Elsewhere"}, ancestryOf(t, api, "Setup"),
		"the document's own Parent must not be joined by the path's")
}

// TestDeclaredTitleWinsForAnIndex: only a title it does not have is supplied.
func TestDeclaredTitleWinsForAnIndex(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"<!-- Title: Handbook -->\n\nx.\n")

	api := runHierarchy(t, dir, nil)

	page, err := api.FindPage("DOCS", "Handbook", "page")
	require.NoError(t, err)
	assert.NotNil(t, page, "the title the document gave itself is kept")
}

// TestParentsFlagStillPrefixes: --parents remains the root of everything.
func TestParentsFlagStillPrefixes(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.Parents = []string{"Docs Root"} })

	assert.Equal(t, []string{"Home", "Docs Root", "Guides"}, ancestryOf(t, api, "Setup"))
}

// TestWithoutTheFlagNothingChanges guards the default.
func TestWithoutTheFlagNothingChanges(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.ParentsFromPath = false })

	assert.Equal(t, []string{"Home"}, ancestryOf(t, api, "Setup"),
		"without the flag the path says nothing")
}

// TestRootIsTakenFromThePattern: the root need not be given.
func TestRootIsTakenFromThePattern(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.ParentsFromPathRoot = "" })

	assert.Equal(t, []string{"Home", "Guides"}, ancestryOf(t, api, "Setup"))
}

// TestTitleCollisionIsRefused is what makes this safe to turn on. A space holds
// one page of a given title, so two documents wanting the same one want the
// same page: without this the second overwrites the first and drags it under
// its own parents, leaving one page where two were meant and nothing to say so.
//
// Deriving parents from the path makes that likely rather than unlucky, since
// every directory tends to hold a README and several will want an "Overview".
func TestTitleCollisionIsRefused(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "api/overview.md", space+"<!-- Title: Overview -->\n\nAPI overview.\n")
	writeAt(t, dir, "sdk/overview.md", space+"<!-- Title: Overview -->\n\nSDK overview.\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "**", "*.md"), Features: []string{"mention"},
		ParentsFromPath: true, ParentsFromPathRoot: dir, Output: io.Discard,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already publishes")
	assert.Contains(t, err.Error(), "--title-append-generated-hash")

	// The document that got there first keeps the page it published, with its
	// own content and under its own parents.
	api := confluence.NewAPI(server.URL, "user", "token", false)
	assert.Equal(t, []string{"Home", "Api"}, ancestryOf(t, api, "Overview"))

	page, err := api.FindPage("DOCS", "Overview", "page")
	require.NoError(t, err)
	found, err := api.GetPageByIDExpanded(page.ID, "ancestors,body.storage")
	require.NoError(t, err)
	assert.Contains(t, found.Body.Storage.Value, "API overview",
		"the first document's page must not have been taken over")
}

// TestSameTitleInDifferentSpacesIsFine: the constraint is per space.
func TestSameTitleInDifferentSpacesIsFine(t *testing.T) {
	server := hierarchyServer(t)
	server.AddSpace("OTHER")
	other := server.AddPage("OTHER", "Other Home", "page", "")
	server.SetHomepage("OTHER", other.ID)

	dir := t.TempDir()
	writeAt(t, dir, "api/overview.md", space+"<!-- Title: Overview -->\n\nOne.\n")
	writeAt(t, dir, "sdk/overview.md",
		"<!-- Space: OTHER -->\n<!-- Title: Overview -->\n\nTwo.\n")

	assert.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "**", "*.md"), Features: []string{"mention"},
		ParentsFromPath: true, ParentsFromPathRoot: dir, Output: io.Discard,
	}))
}

// TestEmptiedDirectoryBecomesAnOrphan is what phase two is for. mark creates
// the page standing for a directory, and nothing used to remember that, so when
// the last document under it went away the page stayed behind with nobody aware
// it had ever been mark's doing.
func TestEmptiedDirectoryBecomesAnOrphan(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")
	writeAt(t, dir, "keep.md", space+"<!-- Title: Keep -->\n\nKeep.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "**", "*.md"), Features: []string{"mention"},
		ParentsFromPath: true, ParentsFromPathRoot: dir,
		TrackPages: true, OnOrphan: "delete", Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	guides, err := api.FindPage("DOCS", "Guides", "page")
	require.NoError(t, err)
	require.NotNil(t, guides, "mark created a page for the directory")

	setup, err := api.FindPage("DOCS", "Setup", "page")
	require.NoError(t, err)
	require.NotNil(t, setup)

	// The whole directory goes.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "guides")))
	require.NoError(t, Run(config))

	// Setup goes first, then Guides, which is only removable once empty.
	assert.True(t, server.Page(setup.ID).Trashed, "the document's page is an orphan")
	require.NoError(t, Run(config))
	assert.True(t, server.Page(guides.ID).Trashed,
		"the page standing for the directory is an orphan too")
}

// TestDirectoryWithAnIndexIsNotRecordedTwice: a directory holding its own
// document has that document's entry for its page already, and two entries
// claiming one page is what the manifest complains about.
func TestDirectoryWithAnIndexIsNotRecordedTwice(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"\nWhat the guides are.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "**", "*.md"), Features: []string{"mention"},
		ParentsFromPath: true, ParentsFromPathRoot: dir,
		TrackPages: true, Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	guides, err := api.FindPage("DOCS", "Guides", "page")
	require.NoError(t, err)
	require.NotNil(t, guides)

	store := manifest.NewStore(api)

	// The README owns the page, under its own path.
	entry, ok, err := store.Lookup("DOCS", filepath.Join(dir, "guides", "README.md"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, guides.ID, entry.PageID)

	// The directory itself is not a second claim on it.
	_, ok, err = store.Lookup("DOCS", filepath.Join(dir, "guides"))
	require.NoError(t, err)
	assert.False(t, ok, "the directory must not claim a page its own document owns")
}

// TestDirectoryPagesAreNotRecordedWithoutTheFlag guards the default.
func TestDirectoryPagesAreNotRecordedWithoutTheFlag(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md",
		space+"<!-- Title: Setup -->\n<!-- Parent: Guides -->\n\nSetup.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "**", "*.md"), Features: []string{"mention"},
		TrackPages: true, Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	_, ok, err := manifest.NewStore(api).Lookup("DOCS", filepath.Join(dir, "guides"))
	require.NoError(t, err)
	assert.False(t, ok, "a parent mark did not derive from the path is not its to track")
}
