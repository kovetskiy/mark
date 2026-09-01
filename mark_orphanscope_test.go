package mark

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getsFor counts the GETs the fake received for exactly one content path,
// which is what a scope check costs: one GetPageByIDExpanded per orphan.
func getsFor(server *confluencetest.Server, pageID string) int {
	var n int
	for _, request := range server.Requests() {
		if request.Method == http.MethodGet && request.Path == "/rest/api/content/"+pageID {
			n++
		}
	}

	return n
}

// TestOrphanScopeIsCheckedOnce pins the class of bug where the same remote
// question is asked twice. handleOrphans filtered its candidates with InScope
// and HandleOrphans then asked again about every survivor, so the one operation
// in mark that trashes pages cost twice the API calls it needed -- for an
// answer that had just been computed.
func TestOrphanScopeIsCheckedOnce(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Inside", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "gone.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Inside -->\n<!-- Title: Gone -->\n\nGone.\n")
	writeFile(t, dir, "keep.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Inside -->\n<!-- Title: Keep -->\n\nKeep.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", OrphanUnder: "Inside",
		Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	gone, err := api.FindPage("DOCS", "Gone", "page")
	require.NoError(t, err)
	require.NotNil(t, gone)

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))
	server.ResetRequests()
	require.NoError(t, Run(config))

	assert.True(t, server.Page(gone.ID).Trashed, "the orphan is still acted on")
	assert.Equal(t, 1, getsFor(server, gone.ID),
		"an orphan's ancestry should be read once, not once per caller that wants the scope")
}
