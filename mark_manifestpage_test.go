package mark

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/manifest"
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

// TestManifestPageByIDAcrossTwoSpaces: a page id names the same page for every
// space, so a run publishing to two of them used to read one manifest as both
// and write it back twice. The second write was refused as stale, warned about
// as a concurrent run, and the run exited 0 with that space's mapping gone.
// The run fails instead, and the space it did publish keeps its mapping.
func TestManifestPageByIDAcrossTwoSpaces(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	opsHome := server.AddPage("OPS", "Home", "page", "")
	server.SetHomepage("OPS", opsHome.ID)
	handbook := server.AddPage("DOCS", "Handbook", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "a.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")
	writeFile(t, dir, "b.md", "<!-- Space: OPS -->\n<!-- Title: Runbook -->\n\nA runbook.\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, ManifestPage: handbook.ID, Output: io.Discard,
	})
	require.Error(t, err, "a run spanning two spaces on one manifest page fails")
	assert.Contains(t, err.Error(), "--manifest-page")
	assert.Contains(t, err.Error(), `"OPS"`)

	api := confluence.NewAPI(server.URL, "user", "token", false)
	runbook, err := api.FindPage("OPS", "Runbook", "page")
	require.NoError(t, err)
	assert.Nil(t, runbook, "refused before anything was published to the second space")

	assert.NotNil(t,
		server.SpaceProperty(handbook.ID, manifest.PropertyKey(manifest.ShardFor(filepath.Join(dir, "a.md")))),
		"the first space's mapping is saved")
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
