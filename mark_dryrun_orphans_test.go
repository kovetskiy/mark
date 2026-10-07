package mark

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writesTo lists the requests that could have changed something on the server:
// anything that is not a read. A dry run must send none of them.
func writesTo(server *confluencetest.Server) []string {
	var writes []string
	for _, r := range server.Requests() {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writes = append(writes, r.Method+" "+r.Path)
		}
	}
	return writes
}

// publishTracked publishes every *.md in dir with tracking on and
// --on-orphan delete, returning the config so a dry run can repeat it.
func publishTracked(t *testing.T, server *confluencetest.Server, dir string) Config {
	t.Helper()
	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.OnOrphan = "delete"
	require.NoError(t, Run(config))
	return config
}

// TestDryRunDoesNotReportPublishedPagesAsOrphans: a dry run over documents that
// are all still present, after a tracked publish, must not read them as gone.
// The dry run found each page by title and returned before recording it, so
// every tracked path looked unseen - and with --on-orphan delete it then set
// about removing the whole set.
func TestDryRunDoesNotReportPublishedPagesAsOrphans(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	a := writeFile(t, dir, "a.md", markdownWithTitle("A"))
	b := writeFile(t, dir, "b.md", markdownWithTitle("B"))
	config := publishTracked(t, server, dir)

	pageA, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	require.NotNil(t, pageA)
	pageB, err := api.FindPage("DOCS", "B", "page")
	require.NoError(t, err)
	require.NotNil(t, pageB)

	server.ResetRequests()

	dry := config
	dry.DryRun = true
	logged := captureLog(t, func() { require.NoError(t, Run(dry)) })

	// The capture works, at the level the orphan report is written at: without
	// this, a quieter logger would make the assertions below pass vacuously.
	assert.Contains(t, logged, "processing "+a)
	assert.Contains(t, logged, "processing "+b)

	assert.NotContains(t, logged, "had no matching source file",
		"every document is still there")
	assert.NotContains(t, logged, "would be deleted")

	assert.Empty(t, writesTo(server), "a dry run must not change anything")
	assert.False(t, server.Page(pageA.ID).Trashed)
	assert.False(t, server.Page(pageB.ID).Trashed)
}

// TestDryRunStillReportsRealOrphans is the other half: recording the pages a dry
// run finds must not hide the one that is really gone. It is reported, and
// would be deleted, but the dry run leaves it where it is.
func TestDryRunStillReportsRealOrphans(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	a := writeFile(t, dir, "a.md", markdownWithTitle("A"))
	b := writeFile(t, dir, "b.md", markdownWithTitle("B"))
	config := publishTracked(t, server, dir)

	pageA, err := api.FindPage("DOCS", "A", "page")
	require.NoError(t, err)
	require.NotNil(t, pageA)
	pageB, err := api.FindPage("DOCS", "B", "page")
	require.NoError(t, err)
	require.NotNil(t, pageB)

	require.NoError(t, os.Remove(b))
	server.ResetRequests()

	dry := config
	dry.DryRun = true
	logged := captureLog(t, func() { require.NoError(t, Run(dry)) })

	assert.Contains(t, logged, "processing "+a)
	assert.Contains(t, logged, "1 tracked page(s) had no matching source file in this run: "+b,
		"b.md is gone, and only b.md")
	assert.Contains(t, logged, `page \"B\" would be deleted`)
	assert.NotContains(t, logged, `page \"A\" would be deleted`)

	assert.Empty(t, writesTo(server), "a dry run must not change anything")
	assert.False(t, server.Page(pageA.ID).Trashed)
	assert.False(t, server.Page(pageB.ID).Trashed, "a dry run only says what it would delete")
}
