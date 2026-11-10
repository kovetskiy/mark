package mark

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/metadata"
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

// ancestryOf reports the titles a page sits under, outermost first.
func ancestryOf(t *testing.T, api *confluence.API, title string) []string {
	t.Helper()

	page, err := api.FindPage(context.Background(), "DOCS", title, "page")
	require.NoError(t, err)
	require.NotNil(t, page, "page %q should exist", title)

	found, err := api.GetPageByIDExpanded(context.Background(), page.ID, "ancestors")
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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		ParentsFromPath:     true,
		ParentsFromPathRoot: dir,
	}
	if extra != nil {
		extra(&config)
	}
	require.NoError(t, Run(context.Background(), config))

	return confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
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

	page, err := api.FindPage(context.Background(), "DOCS", "Guides", "page")
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

	page, err := api.FindPage(context.Background(), "DOCS", "Handbook", "page")
	require.NoError(t, err)
	assert.NotNil(t, page, "the title the document gave itself is kept")
}

// TestParentsFlagStillPrefixes: --parents remains the root of everything.
func TestParentsFlagStillPrefixes(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.Page.Parents = []string{"Docs Root"} })

	assert.Equal(t, []string{"Home", "Docs Root", "Guides"}, ancestryOf(t, api, "Setup"))
}

// TestParentsFlagIsNotWrittenThrough: the --parents prefix was prepended by
// appending onto the flag's own slice, so every document's meta.Parents shared
// its backing array -- the very array, for a document declaring no Parent of
// its own. Following a renamed parent writes the new title into meta.Parents,
// which wrote it into Config.Page.Parents: the caller's value changed under it, and
// every later document in the run started from a chain it had not declared.
func TestParentsFlagIsNotWrittenThrough(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	for _, name := range []string{"One", "Two", "Three"} {
		writeAt(t, dir, strings.ToLower(name)+".md", space+"<!-- Title: "+name+" -->\n\nx.\n")
	}

	// The capacity splitParents leaves three parents with, and room to spare.
	parents := append(make([]string, 0, 4), "Root", "Section", "Chapter")

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.Page.Parents = parents
	require.NoError(t, Run(context.Background(), config))

	chapter, err := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}).
		FindPage(context.Background(), "DOCS", "Chapter", "page")
	require.NoError(t, err)
	require.NotNil(t, chapter)

	server.RenamePage(chapter.ID, "Chapter Renamed")
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, []string{"Root", "Section", "Chapter"}, parents,
		"the caller's --parents must come back as it was given")

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	for _, name := range []string{"One", "Two", "Three"} {
		assert.Equal(t, []string{"Home", "Root", "Section", "Chapter Renamed"}, ancestryOf(t, api, name),
			"%s still sits under the renamed parent", name)
	}
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

	err := Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		ParentsFromPath:     true,
		ParentsFromPathRoot: dir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already publishes")
	assert.Contains(t, err.Error(), "--title-append-generated-hash")

	// The document that got there first keeps the page it published, with its
	// own content and under its own parents.
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	assert.Equal(t, []string{"Home", "Api"}, ancestryOf(t, api, "Overview"))

	page, err := api.FindPage(context.Background(), "DOCS", "Overview", "page")
	require.NoError(t, err)
	found, err := api.GetPageByIDExpanded(context.Background(), page.ID, "ancestors,body.storage")
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

	assert.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		ParentsFromPath:     true,
		ParentsFromPathRoot: dir,
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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		ParentsFromPath:     true,
		ParentsFromPathRoot: dir,
		Tracking: TrackingConfig{
			TrackPages: true,
			OnOrphan:   "delete",
		},
	}
	require.NoError(t, Run(context.Background(), config))

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	guides, err := api.FindPage(context.Background(), "DOCS", "Guides", "page")
	require.NoError(t, err)
	require.NotNil(t, guides, "mark created a page for the directory")

	setup, err := api.FindPage(context.Background(), "DOCS", "Setup", "page")
	require.NoError(t, err)
	require.NotNil(t, setup)

	// The whole directory goes.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "guides")))
	require.NoError(t, Run(context.Background(), config))

	// Setup goes first, then Guides, which is only removable once empty.
	assert.True(t, server.Page(setup.ID).Trashed, "the document's page is an orphan")
	require.NoError(t, Run(context.Background(), config))
	assert.True(t, server.Page(guides.ID).Trashed,
		"the page standing for the directory is an orphan too")
}

// TestDirectoryWithAnIndexIsNotRecordedTwice: a directory holding its own
// document has that document's entry for its page already, and two entries
// claiming one page is what the manifest complains about.
func TestDirectoryWithAnIndexIsNotRecordedTwice(t *testing.T) {
	ctx := t.Context()
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"\nWhat the guides are.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	config := Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		ParentsFromPath:     true,
		ParentsFromPathRoot: dir,
		Tracking: TrackingConfig{
			TrackPages: true,
		},
	}
	require.NoError(t, Run(context.Background(), config))

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	guides, err := api.FindPage(context.Background(), "DOCS", "Guides", "page")
	require.NoError(t, err)
	require.NotNil(t, guides)

	store := manifest.NewStore(api)

	// The README owns the page, under its own path.
	entry, ok, err := store.Lookup(ctx, "DOCS", filepath.Join(dir, "guides", "README.md"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, guides.ID, entry.PageID)

	// The directory itself is not a second claim on it.
	_, ok, err = store.Lookup(ctx, "DOCS", filepath.Join(dir, "guides"))
	require.NoError(t, err)
	assert.False(t, ok, "the directory must not claim a page its own document owns")
}

// TestDirectoryPagesAreNotRecordedWithoutTheFlag guards the default.
func TestDirectoryPagesAreNotRecordedWithoutTheFlag(t *testing.T) {
	ctx := t.Context()
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md",
		space+"<!-- Title: Setup -->\n<!-- Parent: Guides -->\n\nSetup.\n")

	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		Tracking: TrackingConfig{
			TrackPages: true,
		},
	}))

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	_, ok, err := manifest.NewStore(api).Lookup(ctx, "DOCS", filepath.Join(dir, "guides"))
	require.NoError(t, err)
	assert.False(t, ok, "a parent mark did not derive from the path is not its to track")
}

// TestIndexTitleIsTheDirectoryTitle is a bug phase one left behind. A README
// that titled itself published under that title, while the documents beside it
// went on looking for a page named after the directory -- so mark made a second,
// empty one and put them under that, leaving the README's page childless.
func TestIndexTitleIsTheDirectoryTitle(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"<!-- Title: Developer Handbook -->\n\nIntro.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, nil)

	assert.Equal(t, []string{"Home", "Developer Handbook"}, ancestryOf(t, api, "Setup"),
		"the children belong to the page their directory's document published")

	empty, err := api.FindPage(context.Background(), "DOCS", "Guides", "page")
	require.NoError(t, err)
	assert.Nil(t, empty, "no second page named after the directory")
}

// TestIndexH1BecomesTheDirectoryTitle: with --title-from-h1 the heading is what
// the document calls itself, and phase one threw it away.
func TestIndexH1BecomesTheDirectoryTitle(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"\n# Developer Handbook\n\nIntro.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.Page.TitleFromH1 = true })

	assert.Equal(t, []string{"Home", "Developer Handbook"}, ancestryOf(t, api, "Setup"))

	empty, err := api.FindPage(context.Background(), "DOCS", "Guides", "page")
	require.NoError(t, err)
	assert.Nil(t, empty)
}

// TestPagesFileNamesADirectory covers a directory with no document of its own.
func TestPagesFileNamesADirectory(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/.pages", "title: Developer Handbook\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, nil)

	assert.Equal(t, []string{"Home", "Developer Handbook"}, ancestryOf(t, api, "Setup"))
}

// TestDocumentBeatsPagesFile: a directory's own document is the more particular
// statement about what its page is called.
func TestDocumentBeatsPagesFile(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/.pages", "title: From The Pages File\n")
	writeAt(t, dir, "guides/README.md", space+"<!-- Title: From The Document -->\n\nx.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, nil)

	assert.Equal(t, []string{"Home", "From The Document"}, ancestryOf(t, api, "Setup"))
}

// TestIndexWithByteOrderMarkNamesItsDirectory: the directory's title is read
// from the same bytes publishing reads. A byte-order mark in front of the Title
// header used to hide it from the directory's reader, which named the page
// after the directory -- and publishing then gave the document that name in
// place of the one it gave itself.
func TestIndexWithByteOrderMarkNamesItsDirectory(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", "\uFEFF"+space+"<!-- Title: Custom -->\n\nIntro.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, nil)

	assert.Equal(t, []string{"Home", "Custom"}, ancestryOf(t, api, "Setup"))

	named, err := api.FindPage(context.Background(), "DOCS", "Guides", "page")
	require.NoError(t, err)
	assert.Nil(t, named, "the document's own title is kept, not the directory's")
}

// TestIgnoredTitleDoesNotNameItsDirectory: a header inside an ignored region is
// not published, so it cannot name the directory either.
func TestIgnoredTitleDoesNotNameItsDirectory(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md",
		"<!-- ac:ignore -->\n<!-- Title: Hidden -->\n<!-- ac:ignore end -->\n"+space+"\nIntro.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, nil)

	assert.Equal(t, []string{"Home", "Guides"}, ancestryOf(t, api, "Setup"))

	hidden, err := api.FindPage(context.Background(), "DOCS", "Hidden", "page")
	require.NoError(t, err)
	assert.Nil(t, hidden, "an ignored header names nothing")
}

// TestUnreadablePagesFileIsAnError: only a missing .pages file is the ordinary
// case. One that is there and cannot be read used to be taken for absent, and
// the directory quietly named after itself.
func TestUnreadablePagesFileIsAnError(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "guides", ".pages"), 0o755))
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	err := Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		ParentsFromPath:     true,
		ParentsFromPathRoot: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".pages")
}

// TestFilenameNeverNamesADirectory: --title-from-filename would call every
// directory's page "Readme", which is a name for a file rather than a page.
func TestFilenameNeverNamesADirectory(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"\nIntro.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.Page.TitleFromFilename = true })

	assert.Equal(t, []string{"Home", "Guides"}, ancestryOf(t, api, "Setup"))

	readme, err := api.FindPage(context.Background(), "DOCS", "Readme", "page")
	require.NoError(t, err)
	assert.Nil(t, readme, "no page should be called Readme")
}

// TestHashTellsSameTitlesApart: --title-append-generated-hash is the way to
// keep two documents of one title in different directories, and it only works
// if the hash is taken once the derived parents are in. Taken before, the two
// hashed the same and were refused all the same.
func TestHashTellsSameTitlesApart(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "api/overview.md", space+"<!-- Title: Overview -->\n\nAPI.\n")
	writeAt(t, dir, "sdk/overview.md", space+"<!-- Title: Overview -->\n\nSDK.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.Page.TitleAppendGeneratedHash = true })

	var titles []string
	for _, p := range serverPages(t, api) {
		if strings.HasPrefix(p.Title, "Overview - ") {
			titles = append(titles, p.Title)
		}
	}
	require.Len(t, titles, 2, "both documents publish, under titles the hash tells apart")
	assert.NotEqual(t, titles[0], titles[1])
}

// serverPages lists every page in DOCS by title, through the API the run used.
func serverPages(t *testing.T, api *confluence.API) []*confluence.PageInfo {
	t.Helper()

	home, err := api.FindHomePage(context.Background(), "DOCS")
	require.NoError(t, err)

	var all []*confluence.PageInfo
	var walk func(parentID string)
	walk = func(parentID string) {
		children, err := api.GetChildPages(context.Background(), parentID)
		require.NoError(t, err)
		for i := range children {
			all = append(all, &children[i])
			walk(children[i].ID)
		}
	}
	walk(home.ID)

	return all
}

// TestTitleCollisionIgnoresCase: Confluence compares titles without regard to
// case, so "Overview" and "overview" want the same page too.
func TestTitleCollisionIgnoresCase(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "api/overview.md", space+"<!-- Title: Overview -->\n\nOne.\n")
	writeAt(t, dir, "sdk/overview.md", space+"<!-- Title: overview -->\n\nTwo.\n")

	err := Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		ParentsFromPath:     true,
		ParentsFromPathRoot: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already publishes")
}

// TestRootCoveringNothingIsRefused: a --parents-from-path-root the files do not
// lie under used to derive no parents at all, silently.
func TestRootCoveringNothingIsRefused(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	err := Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "**", "*.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
		ParentsFromPath:     true,
		ParentsFromPathRoot: filepath.Join(dir, "elsewhere"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "covers none of the files")
}

// TestReadmeOutsideTheRunDoesNotNameItsDirectory: only a document the run
// publishes stands for its directory. One the pattern leaves out would
// otherwise name a page that never gets published.
func TestReadmeOutsideTheRunDoesNotNameItsDirectory(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"<!-- Title: Not Published -->\n\nx.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, func(c *Config) {
		c.Files = filepath.Join(dir, "**", "setup.md")
	})

	assert.Equal(t, []string{"Home", "Guides"}, ancestryOf(t, api, "Setup"))
}

// TestRootReadmeIsTitledByTheRoot: the document standing for the root has no
// directory above it to be named after, and "Readme" is a name for a file.
func TestRootReadmeIsTitledByTheRoot(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "README.md", space+"\nThe docs.\n")

	api := runHierarchy(t, dir, nil)

	page, err := api.FindPage(context.Background(), "DOCS", metadata.TitleFromName(filepath.Base(dir)), "page")
	require.NoError(t, err)
	assert.NotNil(t, page, "the root's document is titled after the root directory")
}

// titlesWithPrefix lists the titles of the pages in DOCS that begin with prefix.
func titlesWithPrefix(t *testing.T, api *confluence.API, prefix string) []string {
	t.Helper()

	var titles []string
	for _, p := range serverPages(t, api) {
		if strings.HasPrefix(p.Title, prefix) {
			titles = append(titles, p.Title)
		}
	}

	return titles
}

// TestHashedIndexIsItsDirectorysPage: with --title-append-generated-hash the
// README's title was hashed while the documents beside it still looked for the
// unhashed directory title, so the README published childless and an empty
// page named after the directory took its children.
func TestHashedIndexIsItsDirectorysPage(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "guides/README.md", space+"\nWhat the guides are.\n")
	writeAt(t, dir, "guides/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.Page.TitleAppendGeneratedHash = true })

	guides := titlesWithPrefix(t, api, "Guides")
	require.Len(t, guides, 1, "one page for the directory, not the README and an empty one")
	assert.True(t, strings.HasPrefix(guides[0], "Guides - "), "the directory's page is hashed: %q", guides[0])

	setup := titlesWithPrefix(t, api, "Setup - ")
	require.Len(t, setup, 1)
	assert.Equal(t, []string{"Home", guides[0]}, ancestryOf(t, api, setup[0]),
		"the children belong to the page their directory's document published")
}

// TestHashTellsSameDirectoriesApart: a directory without a document of its own
// is a page too, and two of one name in different places are two pages. The
// hash on the documents beneath them is no use if both still look for one
// parent of the same unhashed title: the second directory's documents end up
// under the first's page.
func TestHashTellsSameDirectoriesApart(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "api/guides/setup.md", space+"<!-- Title: Setup -->\n\nAPI.\n")
	writeAt(t, dir, "sdk/guides/setup.md", space+"<!-- Title: Setup -->\n\nSDK.\n")

	api := runHierarchy(t, dir, func(c *Config) { c.Page.TitleAppendGeneratedHash = true })

	guides := titlesWithPrefix(t, api, "Guides - ")
	require.Len(t, guides, 2, "each directory has a page of its own, hashed apart")
	assert.NotEqual(t, guides[0], guides[1])

	for _, title := range titlesWithPrefix(t, api, "Setup - ") {
		ancestry := ancestryOf(t, api, title)
		require.Len(t, ancestry, 3, "%s sits under its own directories", title)
		assert.True(t, strings.HasPrefix(ancestry[1], "Api - ") || strings.HasPrefix(ancestry[1], "Sdk - "),
			"%s: the top directory's page is hashed too: %v", title, ancestry)
		assert.True(t, strings.HasPrefix(ancestry[2], "Guides - "), "%s: %v", title, ancestry)
	}

	// Each Guides page holds exactly one Setup.
	for _, title := range guides {
		parent, err := api.FindPage(context.Background(), "DOCS", title, "page")
		require.NoError(t, err)
		require.NotNil(t, parent)
		children, err := api.GetChildPages(context.Background(), parent.ID)
		require.NoError(t, err)
		assert.Len(t, children, 1, "%s holds its own directory's document only", title)
	}
}

// failOnceLookingUp makes the fake refuse the first page lookup for a title and
// answer every later one normally, which is what a network blip looks like from
// here.
func failOnceLookingUp(server *confluencetest.Server, title string) {
	failed := false
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if failed || r.Method != http.MethodGet {
			return 0, "", false
		}
		if r.URL.Query().Get("title") != title {
			return 0, "", false
		}

		failed = true

		return http.StatusForbidden, `{"message":"nope"}`, true
	})
}

// TestStaleParentLookupFailureStopsTheRun pins the class of bug where a failed
// read is treated as an answer. refreshStaleParents asks whether a parent title
// still resolves, and a lookup that errored used to be indistinguishable from a
// parent that is exactly where it says it is -- so a blip left the stale title
// in place, which is the one condition that makes ancestry create an empty
// duplicate parent and re-home the real one's children.
func TestStaleParentLookupFailureStopsTheRun(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownWithTitle("Child"))

	failOnceLookingUp(server, "Parent")

	err := Run(context.Background(), trackingConfig(server, file))
	require.Error(t, err, "a parent that could not be read is not a parent that is fine")
	assert.Contains(t, err.Error(), `unable to look up parent "Parent"`)
}

// TestStaleParentLookupSucceedsNormally guards the other side: the failure path
// must not have made the ordinary run any more fragile.
func TestStaleParentLookupSucceedsNormally(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownWithTitle("Child"))

	require.NoError(t, Run(context.Background(), trackingConfig(server, file)))

	published, err := api.FindPage(context.Background(), "DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, published)
}

// TestLinkFindsThePageTheHierarchyTitles: a link to another document names the
// page that document publishes as, which under --parents-from-path the path
// helps decide -- a README takes its directory's title, and the generated hash
// is taken over the parents the path gives. Read from the linked document's
// headers alone, the link looked for a title nothing publishes under and was
// left pointing at the Markdown file.
func TestLinkFindsThePageTheHierarchyTitles(t *testing.T) {
	for name, tc := range map[string]struct {
		configure func(*Config)
		linked    string // the document linked to
		target    string // a prefix of the title it publishes under
	}{
		"index titled by its directory": {
			configure: func(c *Config) { c.Page.TitleFromFilename = true },
			linked:    "guide/README.md",
			target:    "Guide",
		},
		"hash over the derived parents": {
			configure: func(c *Config) { c.Page.TitleAppendGeneratedHash = true },
			linked:    "guide/setup.md",
			target:    "Setup - ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := hierarchyServer(t)
			dir := t.TempDir()
			writeAt(t, dir, "intro.md", space+"<!-- Title: Intro -->\n\nSee [it]("+tc.linked+").\n")
			writeAt(t, dir, "guide/README.md", space+"\nThe guide.\n")
			writeAt(t, dir, "guide/setup.md", space+"<!-- Title: Setup -->\n\nSetup.\n")

			config := publishConfig(server.URL, filepath.Join(dir, "**", "*.md"))
			config.ParentsFromPath = true
			config.ParentsFromPathRoot = dir
			tc.configure(&config)
			require.NoError(t, Run(context.Background(), config))

			api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

			var target, intro *confluence.PageInfo
			for _, p := range serverPages(t, api) {
				switch {
				case strings.HasPrefix(p.Title, "Intro"):
					intro = p
				case strings.HasPrefix(p.Title, tc.target):
					target = p
				}
			}
			require.NotNil(t, intro)
			require.NotNil(t, target, "the linked document publishes")

			link, err := confluence.GenerateTinyLink(server.URL, target.ID)
			require.NoError(t, err)

			body := server.Page(intro.ID).Body
			assert.NotContains(t, body, `href="`+tc.linked+`"`, "the link is resolved")
			assert.Contains(t, body, `href="`+link+`"`, "to the page the linked document is")
		})
	}
}

// TestLinkFromAnIncludePathFindsTheHierarchy: a link inside a fragment found
// through --include-path is resolved from that directory, which the operator
// may name relative while the root is absolute. The linked document is the
// same file either way, so it has to take the same title.
func TestLinkFromAnIncludePathFindsTheHierarchy(t *testing.T) {
	for name, relativeRoot := range map[string]bool{
		"relative include path, absolute root": false,
		"absolute include path, relative root": true,
	} {
		t.Run(name, func(t *testing.T) {
			server := hierarchyServer(t)
			dir := t.TempDir()
			writeAt(t, dir, "docs/sub/intro.md", space+"<!-- Title: Intro -->\n\n<!-- Include: frag.md -->\n")
			writeAt(t, dir, "docs/guide/README.md", space+"\nThe guide.\n")
			writeAt(t, dir, "shared/frag.md", "See [the guide](../docs/guide/README.md).\n")

			cwd, err := os.Getwd()
			require.NoError(t, err)
			relative, err := filepath.Rel(cwd, dir)
			require.NoError(t, err)

			project, includeDir := dir, relative
			if relativeRoot {
				project, includeDir = relative, dir
			}

			config := publishConfig(server.URL, filepath.Join(project, "docs", "**", "*.md"))
			config.ParentsFromPath = true
			config.ParentsFromPathRoot = filepath.Join(project, "docs")
			config.Page.TitleFromFilename = true
			config.Rendering.IncludePath = filepath.Join(includeDir, "shared")
			require.NoError(t, Run(context.Background(), config))

			api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
			guide, err := api.FindPage(context.Background(), "DOCS", "Guide", "page")
			require.NoError(t, err)
			require.NotNil(t, guide, "the README publishes as its directory")
			link, err := confluence.GenerateTinyLink(server.URL, guide.ID)
			require.NoError(t, err)

			assert.Contains(t, bodyOfPageTitled(t, server, "Intro"), `href="`+link+`"`)
		})
	}
}

// TestLinkToADocumentOutsideTheRunKeepsItsHeaders: the hierarchy only places
// the run's own files. A README another run publishes is titled by that run,
// from its own headers, not after a directory this run knows no index for.
func TestLinkToADocumentOutsideTheRunKeepsItsHeaders(t *testing.T) {
	server := hierarchyServer(t)
	dir := t.TempDir()
	writeAt(t, dir, "docs/a.md", space+"<!-- Title: A -->\n\nSee [it](sub/README.md).\n")
	writeAt(t, dir, "docs/sub/README.md", space+"<!-- Title: Sub Guide -->\n\nThe guide.\n")

	run := func(files string) {
		config := publishConfig(server.URL, filepath.Join(dir, "docs", files))
		config.ParentsFromPath = true
		config.ParentsFromPathRoot = filepath.Join(dir, "docs")
		require.NoError(t, Run(context.Background(), config))
	}
	run(filepath.Join("sub", "*.md"))
	run("*.md")

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	guide, err := api.FindPage(context.Background(), "DOCS", "Sub Guide", "page")
	require.NoError(t, err)
	require.NotNil(t, guide, "the other run publishes the README under its header")
	link, err := confluence.GenerateTinyLink(server.URL, guide.ID)
	require.NoError(t, err)

	// The page has a URL by now, so the link is to its tiny link under it.
	tiny := link[strings.Index(link, "/x/"):]
	assert.Contains(t, bodyOfPageTitled(t, server, "A"), tiny+`"`)
}
