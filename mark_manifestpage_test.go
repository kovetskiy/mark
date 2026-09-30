package mark

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrackPagesThroughTheGatewayWithAManifestPage is issue #1020 end to end: a
// scoped API token cannot create a space property, so the mapping is kept on a
// page instead, through v2 -- and a rename on the second run still finds the
// page the first run made, with every v1 endpoint refused throughout.
func TestTrackPagesThroughTheGatewayWithAManifestPage(t *testing.T) {
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
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	config := Config{
		BaseURL: baseURL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, ManifestPage: "Home", Output: io.Discard,
	}
	require.NoError(t, Run(config))

	api := confluence.NewAPI(baseURL, "user", "token", false)
	guide, err := api.FindPage("DOCS", "Guide", "page")
	require.NoError(t, err)
	require.NotNil(t, guide)

	assert.Equal(t, 0, server.CountRequests("POST", "/api/v2/spaces/"),
		"no space property was created")
	shards := 0
	for i := range manifest.ShardCount {
		if server.SpaceProperty(home.ID, manifest.PropertyKey(i)) != nil {
			shards++
		}
	}
	assert.Equal(t, 1, shards, "the mapping is on the page named")

	// A retitle: without the manifest this would be a second page.
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide Renamed -->\n\nA guide.\n")
	require.NoError(t, Run(config))

	renamed, err := api.FindPage("DOCS", "Guide Renamed", "page")
	require.NoError(t, err)
	require.NotNil(t, renamed)
	assert.Equal(t, guide.ID, renamed.ID, "the manifest found the page the first run made")
}

// TestManifestPageRequiresTrackPages: the flag says where a manifest is kept,
// and nothing else keeps one.
func TestManifestPageRequiresTrackPages(t *testing.T) {
	server := confluencetest.New(t)
	dir := t.TempDir()
	writeFile(t, dir, "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), ManifestPage: "Home", Output: io.Discard,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--manifest-page requires --track-pages")
}
