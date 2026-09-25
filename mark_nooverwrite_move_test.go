package mark

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dataCenterMoveFixture publishes one document under "Parent" on the fake
// dressed as Server or Data Center -- no /api/v2 and no content move endpoint
// -- so that moving it later takes the update fallback, which writes a version
// of its own. It returns the server, the page's id, the "Other" parent's id and
// the config, whose Output the caller replaces as it needs.
func dataCenterMoveFixture(
	t *testing.T, configure func(*Config),
) (*confluencetest.Server, string, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2") || strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		return 0, "", false
	})

	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)
	other := server.AddPage("DOCS", "Other", "page", home.ID)

	dir := t.TempDir()
	writeMovingDoc(t, dir, "Parent", "From mark.")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:      filepath.Join(dir, "*.md"),
		Features:   []string{"mention"},
		TrackPages: true,
		Output:     io.Discard,
	}
	configure(&config)

	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc)

	return server, doc.ID, other.ID, config
}

func writeMovingDoc(t *testing.T, dir, parent, body string) {
	t.Helper()

	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: "+parent+" -->\n<!-- Title: Doc -->\n\n"+body+"\n")
}

// runReported runs config and returns what the JSON report says about the
// one page it handled.
func runReported(t *testing.T, config Config) report.Page {
	t.Helper()

	var out bytes.Buffer
	config.OutputFormat = report.FormatJSON
	config.Output = &out

	require.NoError(t, Run(config))

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	require.Len(t, parsed.Pages, 1)

	return parsed.Pages[0]
}

// TestNoOverwriteDoesNotMistakeAMoveForAnEdit: on Server/DC a move is an update,
// and an update is a new version. It happens before --no-overwrite compares the
// page's version with the one mark recorded, so the version mark had just
// written by moving the page looked like somebody else's edit: the page was
// reported as edited in Confluence, left unpublished, and -- since a skipped
// page keeps its old baseline -- reported again on every run after.
func TestNoOverwriteDoesNotMistakeAMoveForAnEdit(t *testing.T) {
	server, id, other, config := dataCenterMoveFixture(t, func(c *Config) {
		c.NoOverwrite = true
	})
	dir := filepath.Dir(config.Files)

	writeMovingDoc(t, dir, "Other", "Moved.")
	moved := runReported(t, config)

	assert.NotEqual(t, report.StatusSkipped, moved.Status, moved.Reason)
	assert.Equal(t, other, server.Page(id).ParentID)
	assert.Contains(t, server.Page(id).Body, "Moved.", "the move run has to publish as well")

	writeMovingDoc(t, dir, "Other", "After the move.")
	after := runReported(t, config)

	assert.NotEqual(t, report.StatusSkipped, after.Status, after.Reason)
	assert.Contains(t, server.Page(id).Body, "After the move.")
}

// TestNoOverwriteAfterAChangesOnlyMove: a document whose only change is its
// parent compiles to the page it already is, so --changes-only moves the page
// and publishes nothing. The version the move wrote still has to become the
// baseline, or the next --no-overwrite run finds a version mark never
// recorded and takes it for an edit.
func TestNoOverwriteAfterAChangesOnlyMove(t *testing.T) {
	for _, noOverwrite := range []bool{false, true} {
		name := map[bool]string{false: "changes-only", true: "changes-only and no-overwrite"}[noOverwrite]
		t.Run(name, func(t *testing.T) {
			server, id, other, config := dataCenterMoveFixture(t, func(c *Config) {
				c.ChangesOnly = true
				c.NoOverwrite = noOverwrite
			})
			dir := filepath.Dir(config.Files)

			writeMovingDoc(t, dir, "Other", "From mark.")
			moved := runReported(t, config)
			assert.NotEqual(t, report.StatusSkipped, moved.Status, moved.Reason)
			require.Equal(t, other, server.Page(id).ParentID)

			config.ChangesOnly = false
			config.NoOverwrite = true
			writeMovingDoc(t, dir, "Other", "After the move.")
			after := runReported(t, config)

			assert.NotEqual(t, report.StatusSkipped, after.Status, after.Reason)
			assert.Contains(t, server.Page(id).Body, "After the move.")
		})
	}
}

// TestNoOverwriteStillSeesAnEditMadeBeforeAMove is the other half: the version
// a move writes is discounted only when the page was at the version mark
// recorded before it. An edit made in Confluence and then carried along by the
// move is still an edit, and is still reported on every run.
func TestNoOverwriteStillSeesAnEditMadeBeforeAMove(t *testing.T) {
	server, id, other, config := dataCenterMoveFixture(t, func(c *Config) {
		c.NoOverwrite = true
	})
	dir := filepath.Dir(config.Files)

	server.EditPage(id, "<p>Written by a person.</p>")

	writeMovingDoc(t, dir, "Other", "Moved.")
	moved := runReported(t, config)

	assert.Equal(t, report.StatusSkipped, moved.Status)
	assert.Contains(t, moved.Reason, "edited in Confluence")
	assert.Equal(t, other, server.Page(id).ParentID,
		"moving the page overwrites nothing, so it is still moved")
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body)

	writeMovingDoc(t, dir, "Other", "After the move.")
	after := runReported(t, config)

	assert.Equal(t, report.StatusSkipped, after.Status, "and it is said again until resolved")
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body)
}
