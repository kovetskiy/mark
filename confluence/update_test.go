package confluence_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateOmitsAncestorsWhenThereAreNone pins the request shape: the
// ancestors key is what Confluence documents for *moving* a page, so sending an
// empty list is a request, not a formality.
//
// A page created under a folder comes back from v2 with no ancestors -- folders
// are not ancestors -- and the update a moment later used to send
// "ancestors": [], which reads as "move this to the space root".
func TestUpdateOmitsAncestorsWhenThereAreNone(t *testing.T) {
	var body []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	page := &confluence.PageInfo{ID: "1001", Title: "Orphan", Type: "page"}
	require.NoError(t, api.UpdatePage(page, "<p/>", false, "", "full-width", ""))

	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &payload))

	_, present := payload["ancestors"]
	assert.False(t, present,
		"an update with nothing to reparent to must not mention ancestors at all")
}

// TestUpdateKeepsTheAncestorItHas is the other half: when the page does have an
// ancestry, the last ancestor still has to be named, or the update itself would
// be the thing that moves it.
func TestUpdateKeepsTheAncestorItHas(t *testing.T) {
	var body []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	page := &confluence.PageInfo{ID: "1001", Title: "Child", Type: "page"}
	page.Ancestors = append(page.Ancestors, struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}{ID: "900", Title: "Root"}, struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}{ID: "950", Title: "Parent"})

	require.NoError(t, api.UpdatePage(page, "<p/>", false, "", "full-width", ""))

	var payload struct {
		Ancestors []struct {
			ID string `json:"id"`
		} `json:"ancestors"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Len(t, payload.Ancestors, 1, "Confluence wants only the immediate parent")
	assert.Equal(t, "950", payload.Ancestors[0].ID)
}

// TestUpdateDoesNotEvictAFolderParentedPage is the same bug seen from the
// server's side, through the fake: publish a page into a folder, update it, and
// it must still be in the folder afterwards.
func TestUpdateDoesNotEvictAFolderParentedPage(t *testing.T) {
	api, server := newAPI(t)
	space := server.AddSpace("DOCS")
	folder := server.AddFolder("DOCS", "Guides", "", "page")

	page, err := api.CreatePageWithFolderParent("DOCS", "page", folder.ID, "In A Folder", "<p/>")
	require.NoError(t, err)
	require.Equal(t, folder.ID, server.Page(page.ID).ParentID)

	require.NoError(t, api.UpdatePage(page, "<p>new</p>", false, "", "full-width", ""))

	assert.Equal(t, folder.ID, server.Page(page.ID).ParentID,
		"an update must not move the page out of its folder")
	assert.NotEmpty(t, space.ID)
}
