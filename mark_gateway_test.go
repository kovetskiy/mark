package mark

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublishThroughTheGateway is issue #917 end to end: a scoped API token
// goes through the api.atlassian.com gateway, which refuses every v1 endpoint
// to it. A publish and a republish have to get by on v2 alone.
func TestPublishThroughTheGateway(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/rest/api/") {
			return http.StatusUnauthorized,
				`{"code":401,"message":"Unauthorized; scope does not match"}`, true
		}
		return 0, "", false
	})

	baseURL := server.URL + "/ex/confluence/cloud-id"

	dir := t.TempDir()
	writeFile(t, dir, "guide.md",
		"<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	config := Config{
		BaseURL: baseURL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}
	require.NoError(t, Run(config))

	writeFile(t, dir, "guide.md",
		"<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA revised guide.\n")
	require.NoError(t, Run(config))

	api := confluence.NewAPI(baseURL, "user", "token", false)
	page, err := api.FindPage("DOCS", "Guide", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	stored := server.Page(page.ID)
	require.NotNil(t, stored)
	assert.Contains(t, stored.Body, "A revised guide.")
	// A first publish creates the page and then fills it in, so the second
	// run's update is the third version.
	assert.Equal(t, int64(3), stored.Version, "the second run updates the page it made")
	assert.Equal(t, home.ID, stored.ParentID, "a republish must not move the page")
}
