package confluence_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordV2Creates keeps the body of every v2 content create, and lets the
// request through to the fake unchanged.
func recordV2Creates(server *confluencetest.Server) *[]map[string]any {
	var bodies []map[string]any
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/v2/") {
			return 0, "", false
		}
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		body["_path"] = r.URL.Path
		bodies = append(bodies, body)
		return 0, "", false
	})
	return &bodies
}

// TestCreatePageWithFolderParentIsTheV2Create pins that a page created in a
// folder goes through the same v2 create as every other: the body v2
// documents, and a result read the way every other v2 result is.
//
// It had its own copy, which drifted: it sent v1's "type", overwrote the web
// UI link v2 returned with a Server-style viewpage.action URL, left Type empty,
// and never learned the site URL from _links.base.
func TestCreatePageWithFolderParentIsTheV2Create(t *testing.T) {
	api, server := newAPI(t)
	server.SiteBase = "https://tenant.example.net/wiki"
	space := server.AddSpace("DOCS")
	folder := server.AddFolder("DOCS", "Guides", "", "page")
	bodies := recordV2Creates(server)

	page, err := api.CreatePageWithFolderParent("DOCS", "page", folder.ID, "In A Folder", "<p/>")
	require.NoError(t, err)

	require.Len(t, *bodies, 1)
	body := (*bodies)[0]
	assert.Equal(t, "/api/v2/pages", body["_path"])
	assert.Equal(t, space.ID, body["spaceId"])
	assert.Equal(t, folder.ID, body["parentId"])
	assert.Equal(t, "folder", body["parentType"])
	assert.NotContains(t, body, "type", "v2 has no content type field; the collection is the type")

	assert.Equal(t, folder.ID, server.Page(page.ID).ParentID)
	assert.Equal(t, "page", page.Type)
	assert.Equal(t, "/display/DOCS/"+page.ID, page.Links.Full, "the web UI link v2 returned")
	assert.Equal(t, "https://tenant.example.net/wiki", page.Links.Base)

	// Remembered like any other created page: finding it costs no request.
	server.ResetRequests()
	found, err := api.FindPage("DOCS", "In A Folder", "page")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, page.ID, found.ID)
	assert.Empty(t, server.Requests())
}

// TestCreateBlogpostWithFolderParentGoesToBlogposts: v2 keeps blogposts in a
// collection of their own and a blogpost has no parent. The old copy posted
// to /pages whatever the type, which made a page.
func TestCreateBlogpostWithFolderParentGoesToBlogposts(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	folder := server.AddFolder("DOCS", "Guides", "", "page")
	bodies := recordV2Creates(server)

	post, err := api.CreatePageWithFolderParent("DOCS", "blogpost", folder.ID, "News", "<p/>")
	require.NoError(t, err)

	require.Len(t, *bodies, 1)
	body := (*bodies)[0]
	assert.Equal(t, "/api/v2/blogposts", body["_path"])
	assert.NotContains(t, body, "parentId")
	assert.NotContains(t, body, "parentType")

	assert.Equal(t, "blogpost", post.Type)
	assert.Equal(t, "blogpost", server.Page(post.ID).Type)
}
