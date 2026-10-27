package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

// writeFile writes content into dir and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// markdownWithTitle is the document under test, parameterised on its title.
func markdownWithTitle(title string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: ` + title + ` -->

# ` + title + `

Some content.
`
}

// trackingConfig is a publish configured to remember page identity.
func trackingConfig(server *confluencetest.Server, files string) Config {
	return Config{
		BaseURL:    server.URL,
		Username:   "user",
		Password:   "token",
		Files:      files,
		Features:   []string{"mention"},
		TrackPages: true,
		Output:     io.Discard,
	}
}

// docsSpace builds the minimal realistic tree the publish path expects.
func docsSpace(t *testing.T) (*confluencetest.Server, *confluence.API) {
	t.Helper()
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)
	return server, api
}

// pageIDTitled returns the id of the page in the space carrying a title, or ""
// when none does.
//
// It builds its own client on purpose. confluence.API memoises FindPage by
// (space, title, type), so asking a client that has already looked this title up
// would be answered from that cache and report a page that has since been
// renamed out from under it.
func pageIDTitled(t *testing.T, server *confluencetest.Server, title string) string {
	t.Helper()
	fresh := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := fresh.FindPage(context.Background(), "DOCS", title, "page")
	require.NoError(t, err)
	if page == nil {
		return ""
	}
	return page.ID
}

// countPagesTitled reports whether any page in the space carries a title, which
// is what tells a retitle apart from a duplicate.
func countPagesTitled(t *testing.T, server *confluencetest.Server, title string) int {
	t.Helper()
	if pageIDTitled(t, server, title) == "" {
		return 0
	}
	return 1
}

// markdownUnder is a document declaring the full ancestry Parent > parent.
// The whole chain has to be declared, not just the immediate parent, or mark
// rejects the page as sitting at a different depth than its headers claim.
func markdownUnder(parent, title string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Parent: ` + parent + ` -->
<!-- Title: ` + title + ` -->

Body.
`
}

// handMadeParent puts a page under DOCS > Parent the way somebody would in the
// Confluence UI, and returns it. No document in the repository publishes it,
// which is what a parent usually is and what the title index cannot know about.
func handMadeParent(t *testing.T, server *confluencetest.Server, title string) *confluencetest.Page {
	t.Helper()
	under, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage(context.Background(), "DOCS", "Parent", "page")
	require.NoError(t, err)
	require.NotNil(t, under)
	return server.AddPage("DOCS", title, "page", under.ID)
}

// markdownInFolder declares a folder hierarchy under the MARK_PARENTS anchor.
func markdownInFolder(folder, title string) string {
	return `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Folder: ` + folder + ` -->
<!-- Title: ` + title + ` -->

Body.
`
}

// captureLog runs fn with the global logger redirected, returning what it wrote.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&buf)
	defer func() { log.Logger = restore }()
	fn()
	return buf.String()
}

func parentDoc(title string) string {
	return "<!-- Space: DOCS -->\n<!-- Title: " + title + " -->\n\n# " + title + "\n\nParent body.\n"
}

// writeAt writes a file, creating the directories leading to it.
func writeAt(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// previewOf runs config as a dry run with the given output format and returns
// what it wrote.
func previewOf(t *testing.T, config Config, format string) string {
	t.Helper()
	var out bytes.Buffer
	config.DryRun = true
	config.OutputFormat = format
	config.Output = &out
	require.NoError(t, Run(context.Background(), config))
	return out.String()
}

// mustFind returns the id of the page titled title in DOCS.
func mustFind(t *testing.T, server *confluencetest.Server, title string) string {
	t.Helper()
	api := confluence.NewAPI(server.URL, "user", "token", false)
	pg, err := api.FindPage(context.Background(), "DOCS", title, "page")
	require.NoError(t, err)
	require.NotNil(t, pg)
	return pg.ID
}

// noOverwriteFixture publishes one page and returns the server, its id and the
// config that produced it.
func noOverwriteFixture(t *testing.T) (*confluencetest.Server, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nFrom mark.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:       filepath.Join(dir, "*.md"),
		Features:    []string{"mention"},
		TrackPages:  true,
		NoOverwrite: true,
		Output:      io.Discard,
	}

	require.NoError(t, Run(context.Background(), config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc)

	return server, doc.ID, config
}

// runReported runs config and returns what the JSON report says about the
// one page it handled.
func runReported(t *testing.T, config Config) report.Page {
	t.Helper()

	var out bytes.Buffer
	config.OutputFormat = report.FormatJSON
	config.Output = &out

	require.NoError(t, Run(context.Background(), config))

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	require.Len(t, parsed.Pages, 1)

	return parsed.Pages[0]
}

// publishConfig is a plain publish with no tracking, for tests about what
// reaches Confluence rather than about what mark remembers.
func publishConfig(url, files string) Config {
	return Config{
		BaseURL:  url,
		Username: "user",
		Password: "token",
		Files:    files,
		Features: []string{"mention"},
		Output:   io.Discard,
	}
}

// bodyOfPageTitled returns what is actually stored on the page carrying a
// title, or "" if no page carries it.
func bodyOfPageTitled(t *testing.T, server *confluencetest.Server, title string) string {
	t.Helper()
	fresh := confluence.NewAPI(server.URL, "user", "token", false)
	found, err := fresh.FindPage(context.Background(), "DOCS", title, "page")
	require.NoError(t, err)
	if found == nil {
		return ""
	}
	return server.Page(found.ID).Body
}
