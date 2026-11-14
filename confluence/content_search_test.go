package confluence_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refuseContentListing answers the /content listing 403, as an instance does
// that has disabled it, and leaves /content/search and single pages alone.
func refuseContentListing(server *confluencetest.Server) {
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet && strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/rest/api/content") {
			return http.StatusForbidden, `{"message":"refused"}`, true
		}
		return 0, "", false
	})
}

func contentGets(server *confluencetest.Server, suffix string) int {
	n := 0
	for _, req := range server.Requests() {
		if req.Method == http.MethodGet && strings.HasSuffix(strings.TrimRight(req.Path, "/"), suffix) {
			n++
		}
	}
	return n
}

func TestFindPageSearchesWhenContentListingIsRefused(t *testing.T) {
	api, server := newAPI(t)
	want := server.AddPage("DOCS", `A "quoted" title`, "page", "")
	server.AddPage("DOCS", "Other", "page", "")
	refuseContentListing(server)

	page, err := api.FindPage(context.Background(), "DOCS", `A "quoted" title`, "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, want.ID, page.ID)
	assert.NotZero(t, page.Version.Number)

	// The refusal is remembered: the next title goes straight to the search.
	missing, err := api.FindPage(context.Background(), "DOCS", "Nope", "page")
	require.NoError(t, err)
	assert.Nil(t, missing)
	assert.Equal(t, 1, contentGets(server, "/rest/api/content"))
	assert.Equal(t, 2, contentGets(server, "/rest/api/content/search"))
}

func TestFindPageReportsTheRefusalWhenSearchFailsToo(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Page", "page", "")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		path := strings.TrimRight(r.URL.Path, "/")
		if strings.HasSuffix(path, "/rest/api/content") || strings.HasSuffix(path, "/content/search") {
			return http.StatusForbidden, `{"message":"refused"}`, true
		}
		return 0, "", false
	})

	_, err := api.FindPage(context.Background(), "DOCS", "Page", "page")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
