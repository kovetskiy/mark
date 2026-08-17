package mark

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func followupServer(t *testing.T) *confluencetest.Server {
	t.Helper()
	s := confluencetest.New(t)
	home := s.AddPage("DOCS", "Home", "page", "")
	s.SetHomepage("DOCS", home.ID)
	s.AddPage("DOCS", "Parent", "page", home.ID)
	return s
}

// TestConfluenceLinkToAPagePublishedLaterInTheRun: an ac: link is checked by
// looking the page up, and a page named here may be published by a file that
// sorts after this one. Failing would make the check depend on the order the
// files happen to be in.
func TestConfluenceLinkToAPagePublishedLaterInTheRun(t *testing.T) {
	server := followupServer(t)
	dir := t.TempDir()

	writeFile(t, dir, "a-doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: A -->\n\nSee [B](ac:B).\n")
	writeFile(t, dir, "b-doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: B -->\n\nB.\n")

	assert.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		CheckLinks: []string{"confluence"}, Output: io.Discard,
	}))
}

// TestLinkInsideAnIncludedFragment: a fragment's links are written from where
// the fragment lives, not from where it is included.
func TestLinkInsideAnIncludedFragment(t *testing.T) {
	server := followupServer(t)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "frag"), 0o755))

	// Named so the fragment is not itself published by the glob below.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag", "part.inc"),
		[]byte("See [the target](./target.md).\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag", "target.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Target -->\n\nTarget.\n"), 0o600))

	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+
			"<!-- Include: frag/part.inc -->\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		// Both levels, so the fragment's neighbour is published too.
		Files: filepath.Join(dir, "**", "*.md"), Features: []string{"mention"},
		CheckLinks: []string{"internal"}, Output: io.Discard,
	}
	// Twice, so the target exists by the time the link is resolved.
	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc)

	assert.Contains(t, server.Page(doc.ID).Body, "/x/",
		"the fragment's link should resolve to the target page")
}

// TestOwnDirectoryWinsOverAnIncludedOne: adding places to look must not change
// what an unambiguous link already meant.
func TestOwnDirectoryWinsOverAnIncludedOne(t *testing.T) {
	server := followupServer(t)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "frag"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag", "part.inc"),
		[]byte("Fragment.\n"), 0o600))
	// Both directories hold a target.md; the document's own must win.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "frag", "target.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Wrong -->\n\nx.\n"), 0o600))
	writeFile(t, dir, "target.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Right -->\n\nx.\n")

	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+
			"<!-- Include: frag/part.inc -->\n\nSee [it](./target.md).\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}
	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	right, err := api.FindPage("DOCS", "Right", "page")
	require.NoError(t, err)
	require.NotNil(t, right)

	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	assert.Contains(t, server.Page(doc.ID).Body, right.ID[:0]+"/x/",
		"the link should have resolved")
	// The Wrong page must not exist at all, since nothing links to it and no
	// file publishes it under that title in this run.
	wrong, err := api.FindPage("DOCS", "Wrong", "page")
	require.NoError(t, err)
	assert.Nil(t, wrong)
}

// TestMacroDefinedAboveTheHeaders: a Macro definition spans several lines, and
// the lines after its first are not headers. Reading them used to end the
// metadata scan, so every header below was lost and the run failed with "space
// is not set".
func TestMacroDefinedAboveTheHeaders(t *testing.T) {
	server := followupServer(t)
	dir := t.TempDir()

	writeFile(t, dir, "doc.md",
		"<!-- Macro: MYJIRA-\\d+\n"+
			"     Template: ac:jira:ticket\n"+
			"     Ticket: ${0} -->\n"+
			"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+
			"See MYJIRA-123.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"}, Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc, "the headers below the macro must still be read")

	body := server.Page(doc.ID).Body
	assert.Contains(t, body, `ac:name="jira"`,
		"the macro defined above the headers must still apply")
	assert.NotContains(t, body, "Template: ac:jira:ticket",
		"the definition must not be published as content")
}
