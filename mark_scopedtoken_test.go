package mark

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scopedTokenGateway refuses every v1 endpoint the way the api.atlassian.com
// gateway refuses one to an Atlassian scoped API token: the token carries the
// granular page scopes, the v1 endpoints check the older content ones, and the
// gateway answers the mismatch before Confluence ever sees the request.
func scopedTokenGateway() confluencetest.FailFunc {
	return func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/rest/api") {
			return http.StatusUnauthorized,
				`{"code":401,"message":"Unauthorized; scope does not match"}`, true
		}
		return 0, "", false
	}
}

// TestPublishWithAScopedToken is issue #917 end to end: a run that resolves the
// space, creates the page and publishes into it without v1 answering anything.
func TestPublishWithAScopedToken(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.SetFail(scopedTokenGateway())

	dir := t.TempDir()
	writeFile(t, dir, "guide.md",
		"<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage("DOCS", "Guide", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	stored := server.Page(page.ID)
	require.NotNil(t, stored)
	assert.Contains(t, stored.Body, "A guide.")
	assert.Equal(t, home.ID, stored.ParentID)
}

// TestRepublishWithAScopedToken: the second run finds the page it made and
// updates it rather than trying to create it again.
func TestRepublishWithAScopedToken(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.SetFail(scopedTokenGateway())

	dir := t.TempDir()
	writeFile(t, dir, "guide.md",
		"<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		Output: io.Discard,
	}
	require.NoError(t, Run(config))

	writeFile(t, dir, "guide.md",
		"<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA revised guide.\n")
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	page, err := api.FindPage("DOCS", "Guide", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	stored := server.Page(page.ID)
	require.NotNil(t, stored)
	assert.Contains(t, stored.Body, "A revised guide.")
	assert.Equal(t, home.ID, stored.ParentID, "a republish must not move the page")
}
