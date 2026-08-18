package mark

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoPassServer(t *testing.T) *confluencetest.Server {
	t.Helper()
	s := confluencetest.New(t)
	home := s.AddPage("DOCS", "Home", "page", "")
	s.SetHomepage("DOCS", home.ID)
	s.AddPage("DOCS", "Parent", "page", home.ID)
	return s
}

// TestLinksResolveOnTheFirstRun is the point of the change. A document linking
// to another the same run creates had nothing to find, so the link stayed dead
// until somebody ran mark a second time.
func TestLinksResolveOnTheFirstRun(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	require.NotNil(t, a)

	body := server.Page(a.ID).Body
	assert.Contains(t, body, "/x/", "the link must resolve within one run")
	assert.NotContains(t, body, "./b-doc.md")
}

// TestSecondPassOnlyRepublishesWhatWaited: a document with nothing waiting must
// not be written twice.
func TestSecondPassOnlyRepublishesWhatWaited(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB, linking nowhere.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	b, err := api.FindPage("DOCS", "B", "page")
	require.NoError(t, err)

	assert.Greater(t, server.Page(a.ID).Version, server.Page(b.ID).Version,
		"only the document that was waiting should have been published again")
}

// TestSecondRunDoesNothingExtra: once the pages exist there is nothing to wait
// for, so a later run publishes each document once.
func TestSecondRunDoesNothingExtra(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	before := server.Page(a.ID).Version

	require.NoError(t, Run(config))

	assert.Equal(t, before+1, server.Page(a.ID).Version,
		"a run with nothing waiting should publish each document once")
}

// TestCheckLinksSeesResolvedLinksForPagesThisRunCreates covers the interaction
// with --check-links, whose "not in the space yet" warning existed because of
// the limitation this removes.
//
// Asserting only that the run succeeds would prove nothing -- that case was a
// warning before as well -- so the link is checked to have actually resolved.
func TestCheckLinksSeesResolvedLinksForPagesThisRunCreates(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		CheckLinks: []string{"internal"}, Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Contains(t, server.Page(a.ID).Body, "/x/",
		"nothing should be left waiting for a page this run created")
}

// TestDryRunDoesNotWaitForPages: nothing is published on a dry run, so nothing
// a link waits for will come to exist and the wait is worth reporting.
func TestDryRunDoesNotWaitForPages(t *testing.T) {
	server := twoPassServer(t)
	dir := t.TempDir()
	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

	writeFile(t, dir, "a-doc.md", header+"<!-- Title: A -->\n\nSee [B](./b-doc.md).\n")
	writeFile(t, dir, "b-doc.md", header+"<!-- Title: B -->\n\nB.\n")

	var out strings.Builder
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		DryRun: true, Output: &out,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	a, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	assert.Nil(t, a, "a dry run publishes nothing")
}
