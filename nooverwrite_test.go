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
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unrestrictedDoc is restrictedDoc's document with its read restriction
// lifted, which is the half of a restriction change that follows the content.
func unrestrictedDoc(body string) string {
	return "---\nrestrictions:\n  read: {}\n---\n" +
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Restricted -->\n\n" + body + "\n"
}

// restrictedNoOverwriteFixture publishes restrictedDoc under --no-overwrite
// and returns the server, the page's id, the document's directory and the
// config.
func restrictedNoOverwriteFixture(t *testing.T) (*confluencetest.Server, string, string, Config) {
	t.Helper()

	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", restrictedDoc("writers", "First."))
	config := trackingConfig(server, file)
	config.Tracking.NoOverwrite = true
	config.Rendering.Features = append(config.Rendering.Features, "frontmatter")
	require.NoError(t, Run(context.Background(), config))

	return server, mustFind(t, server, "Restricted"), dir, config
}

// TestNoOverwriteKeepsAWriteWhoseRestrictionsWereRefused: the body is on the
// page once the update is answered, whatever becomes of the restrictions that
// follow it. The version that update made is mark's own, so the next run
// publishes to the page rather than leaving it alone as edited in Confluence.
func TestNoOverwriteKeepsAWriteWhoseRestrictionsWereRefused(t *testing.T) {
	server, id, dir, config := restrictedNoOverwriteFixture(t)

	writeFile(t, dir, "doc.md", unrestrictedDoc("Second."))
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/restriction") {
			return http.StatusForbidden, `{"message":"not permitted"}`, true
		}
		return 0, "", false
	})

	err := Run(context.Background(), config)
	require.ErrorIs(t, err, confluence.ErrForbidden)
	require.Contains(t, server.Page(id).Body, "Second.", "the body was written before the restrictions were refused")

	server.SetFail(nil)
	writeFile(t, dir, "doc.md", unrestrictedDoc("Third."))
	after := runReported(t, config)

	assert.Equal(t, report.StatusPublished, after.Status, after.Reason)
	assert.Contains(t, server.Page(id).Body, "Third.")
	assert.Empty(t, server.Page(id).RestrictedGroups["read"])
}

// TestNoOverwriteKeepsAWriteTheRunWasStoppedAfter: a run stopped while its
// update is on the way still lands it, and the restrictions that would follow
// are not sent. The next run publishes to the page.
func TestNoOverwriteKeepsAWriteTheRunWasStoppedAfter(t *testing.T) {
	server, id, dir, config := restrictedNoOverwriteFixture(t)

	writeFile(t, dir, "doc.md", unrestrictedDoc("Second."))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/content/"+id) {
			cancel()
		}
		return 0, "", false
	})

	err := Run(ctx, config)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, server.Page(id).Body, "Second.", "the update landed")

	server.SetFail(nil)
	writeFile(t, dir, "doc.md", unrestrictedDoc("Third."))
	after := runReported(t, config)

	assert.Equal(t, report.StatusPublished, after.Status, after.Reason)
	assert.Contains(t, server.Page(id).Body, "Third.")
}

// TestNoOverwriteKeepsAWriteOfAStoppedRunWithoutTheFlag: a run without
// --no-overwrite has not read the manifest by the time it writes a page found
// by its title, and once stopped it cannot. The version it wrote is recorded
// all the same, and the next run with the flag publishes to the page.
func TestNoOverwriteKeepsAWriteOfAStoppedRunWithoutTheFlag(t *testing.T) {
	server, _ := docsSpace(t)
	doc := func(body string) string {
		return "<!-- Space: DOCS -->\n<!-- Title: Plain -->\n\n" + body + "\n"
	}
	dir := t.TempDir()
	config := trackingConfig(server, writeFile(t, dir, "doc.md", doc("First.")))
	config.Tracking.NoOverwrite = true
	require.NoError(t, Run(context.Background(), config))
	id := mustFind(t, server, "Plain")

	writeFile(t, dir, "doc.md", doc("Second."))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/content/"+id) {
			cancel()
		}
		return 0, "", false
	})

	config.Tracking.NoOverwrite = false
	require.ErrorIs(t, Run(ctx, config), context.Canceled)
	require.Contains(t, server.Page(id).Body, "Second.", "the update landed")

	server.SetFail(nil)
	config.Tracking.NoOverwrite = true
	writeFile(t, dir, "doc.md", doc("Third."))
	after := runReported(t, config)

	assert.Equal(t, report.StatusPublished, after.Status, after.Reason)
	assert.Contains(t, server.Page(id).Body, "Third.")
}

// TestNoOverwriteKeepsAWriteWhosePropertiesWereRefused: through the gateway a
// page's properties are written after its body, on an endpoint of their own.
// An update that fails there has still made a version, and it is mark's.
func TestNoOverwriteKeepsAWriteWhosePropertiesWereRefused(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	doc := func(body string) string {
		return "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n<!-- Content-Appearance: full-width -->\n\n" + body + "\n"
	}
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", doc("First."))
	config := Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{Features: []string{"mention"}},
			Output:    io.Discard,
		},
		Files: filepath.Join(dir, "*.md"),
		Connection: ConnectionConfig{
			BaseURL: server.URL + "/ex/confluence/cloud-id", Username: "user", Password: "token",
		},
		Tracking: TrackingConfig{TrackPages: true, NoOverwrite: true},
	}
	require.NoError(t, Run(context.Background(), config))
	id := mustFind(t, server, "Doc")

	writeFile(t, dir, "doc.md", doc("Second."))
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodGet && strings.Contains(r.URL.Path, "/pages/"+id+"/properties") {
			return http.StatusForbidden, `{"errors":[{"status":403,"title":"Forbidden"}]}`, true
		}
		return 0, "", false
	})

	err := Run(context.Background(), config)
	require.ErrorIs(t, err, confluence.ErrForbidden)
	require.Contains(t, server.Page(id).Body, "Second.", "the body was written before the properties were refused")

	server.SetFail(nil)
	writeFile(t, dir, "doc.md", doc("Third."))
	after := runReported(t, config)

	assert.Equal(t, report.StatusPublished, after.Status, after.Reason)
	assert.Contains(t, server.Page(id).Body, "Third.")
}

// TestNoOverwriteStillLeavesAnEditAfterAFailedRun: recording the version of a
// write that was followed by a failure must not record anyone else's. An edit
// made in Confluence after such a run is kept.
func TestNoOverwriteStillLeavesAnEditAfterAFailedRun(t *testing.T) {
	server, id, dir, config := restrictedNoOverwriteFixture(t)

	writeFile(t, dir, "doc.md", unrestrictedDoc("Second."))
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/restriction") {
			return http.StatusForbidden, `{"message":"not permitted"}`, true
		}
		return 0, "", false
	})
	require.Error(t, Run(context.Background(), config))

	server.SetFail(nil)
	server.EditPage(id, "<p>Written by a person.</p>")
	writeFile(t, dir, "doc.md", unrestrictedDoc("Third."))
	after := runReported(t, config)

	assert.Equal(t, report.StatusSkipped, after.Status)
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body)
}

// TestNoOverwriteLeavesAnEditedPageWhereItIs: a page left alone as edited in
// Confluence is not moved either, and the parent its document now names is
// not created for it.
func TestNoOverwriteLeavesAnEditedPageWhereItIs(t *testing.T) {
	server, id, config := noOverwriteFixture(t)
	dir := filepath.Dir(config.Files)
	parent := server.Page(id).ParentID

	server.EditPage(id, "<p>Written by a person.</p>")
	writeMovingDoc(t, dir, "Elsewhere", "From mark again.")
	server.ResetRequests()
	skipped := runReported(t, config)

	assert.Equal(t, report.StatusSkipped, skipped.Status)
	assert.Equal(t, id, skipped.PageID)
	assert.Equal(t, parent, server.Page(id).ParentID, "the page stays under the parent it had")
	assert.Zero(t, countPagesTitled(t, server, "Elsewhere"), "no parent is created for a page that is left alone")
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body)

	// Published without the flag, the page is reclaimed and moved.
	config.Tracking.NoOverwrite = false
	require.NoError(t, Run(context.Background(), config))
	assert.Equal(t, mustFind(t, server, "Elsewhere"), server.Page(id).ParentID)
	assert.Contains(t, server.Page(id).Body, "From mark again.")
}

// TestNoOverwriteLeavesAnEditedRetitledPageWhereItIs: the same for a page
// found through the manifest because its document's title changed as well.
func TestNoOverwriteLeavesAnEditedRetitledPageWhereItIs(t *testing.T) {
	server, id, config := noOverwriteFixture(t)
	dir := filepath.Dir(config.Files)
	parent := server.Page(id).ParentID

	server.EditPage(id, "<p>Written by a person.</p>")
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Elsewhere -->\n<!-- Title: Doc Renamed -->\n\nFrom mark again.\n")
	skipped := runReported(t, config)

	assert.Equal(t, report.StatusSkipped, skipped.Status)
	assert.Equal(t, id, skipped.PageID)
	assert.Equal(t, parent, server.Page(id).ParentID, "the page stays under the parent it had")
	assert.Equal(t, "Doc", server.Page(id).Title)
	assert.Zero(t, countPagesTitled(t, server, "Elsewhere"), "no parent is created for a page that is left alone")

	// The skip repeats until the difference is resolved.
	assert.Equal(t, report.StatusSkipped, runReported(t, config).Status)
}

// TestNoOverwriteLeavesAnEditedPageOfAMovedFileWhereItIs: the same for a
// document whose file moved, whose baseline is still under the path it had.
func TestNoOverwriteLeavesAnEditedPageOfAMovedFileWhereItIs(t *testing.T) {
	server, id, config := noOverwriteFixture(t)
	dir := filepath.Dir(config.Files)
	parent := server.Page(id).ParentID

	server.EditPage(id, "<p>Written by a person.</p>")
	require.NoError(t, os.Remove(filepath.Join(dir, "doc.md")))
	writeMovingDoc(t, dir, "Elsewhere", "From mark again.")
	require.NoError(t, os.Rename(filepath.Join(dir, "doc.md"), filepath.Join(dir, "moved.md")))
	skipped := runReported(t, config)

	assert.Equal(t, report.StatusSkipped, skipped.Status, skipped.Reason)
	assert.Equal(t, parent, server.Page(id).ParentID, "the page stays under the parent it had")
	assert.Zero(t, countPagesTitled(t, server, "Elsewhere"), "no parent is created for a page that is left alone")
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body)

	// The baseline follows the file, so the skip repeats.
	assert.Equal(t, report.StatusSkipped, runReported(t, config).Status)
}

// TestNoOverwriteMovesAPageNobodyEdited: a page still at the version mark
// wrote follows its document to a new parent as before.
func TestNoOverwriteMovesAPageNobodyEdited(t *testing.T) {
	server, id, config := noOverwriteFixture(t)
	dir := filepath.Dir(config.Files)

	writeMovingDoc(t, dir, "Elsewhere", "Moved.")
	moved := runReported(t, config)

	assert.Equal(t, report.StatusPublished, moved.Status, moved.Reason)
	assert.Equal(t, mustFind(t, server, "Elsewhere"), server.Page(id).ParentID)
	assert.Contains(t, server.Page(id).Body, "Moved.")
}
