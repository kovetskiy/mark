package mark

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// labelFixture publishes a page carrying the given Label headers and returns
// the server, the page id and a config that republishes it.
func labelFixture(t *testing.T, appendLabels bool, labels string) (*confluencetest.Server, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n"+labels+"\nBody.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:        filepath.Join(dir, "doc.md"),
		Features:     []string{"mention"},
		AppendLabels: appendLabels,
		Output:       io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc)

	return server, doc.ID, config
}

// TestLabelsRemovedByDefault pins the behaviour that has always been: the
// document decides, and a label it does not name is taken off.
func TestLabelsRemovedByDefault(t *testing.T) {
	server, id, config := labelFixture(t, false, "<!-- Label: from-mark -->\n")

	server.AddLabel(id, "added-by-hand")
	require.NoError(t, Run(config))

	assert.Equal(t, []string{"from-mark"}, server.Page(id).Labels,
		"without --append-labels a page carries exactly what its headers name")
}

// TestAppendLabelsKeepsLabelsAddedInConfluence is the fix. A label applied in
// the web UI drives macros, searches and reports; removing it destroys work no
// document ever mentioned.
func TestAppendLabelsKeepsLabelsAddedInConfluence(t *testing.T) {
	server, id, config := labelFixture(t, true, "<!-- Label: from-mark -->\n")

	server.AddLabel(id, "added-by-hand")
	require.NoError(t, Run(config))

	assert.ElementsMatch(t, []string{"from-mark", "added-by-hand"}, server.Page(id).Labels,
		"a label applied in Confluence must survive a publish")
}

// TestAppendLabelsStillAddsNewOnes: appending must not mean doing nothing.
func TestAppendLabelsStillAddsNewOnes(t *testing.T) {
	server, id, config := labelFixture(t, true, "<!-- Label: first -->\n")

	dir := filepath.Dir(config.Files)
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n"+
			"<!-- Label: first -->\n<!-- Label: second -->\n\nBody.\n")
	require.NoError(t, Run(config))

	assert.ElementsMatch(t, []string{"first", "second"}, server.Page(id).Labels)
}

// TestAppendLabelsKeepsARemovedHeadersLabel is the cost of the flag, written
// down: a label outlives the header that introduced it.
func TestAppendLabelsKeepsARemovedHeadersLabel(t *testing.T) {
	server, id, config := labelFixture(t, true, "<!-- Label: first -->\n<!-- Label: second -->\n")

	dir := filepath.Dir(config.Files)
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n"+
			"<!-- Label: first -->\n\nBody.\n")
	require.NoError(t, Run(config))

	assert.ElementsMatch(t, []string{"first", "second"}, server.Page(id).Labels,
		"appending cannot tell a dropped header from a label somebody added")
}
