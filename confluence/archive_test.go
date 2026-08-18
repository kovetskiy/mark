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

// TestArchivePageSendsANumericID pins the request Confluence documents:
// POST /rest/api/content/archive with {"pages":[{"id":<number>}]}.
//
// The id is the part worth pinning. Every other endpoint here takes one as a
// string, this one rejects a quoted id with 400, and nothing in the call site
// hints at the difference.
func TestArchivePageSendsANumericID(t *testing.T) {
	var (
		method string
		path   string
		body   []byte
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"task-1","links":{"status":"/rest/api/longtask/task-1"}}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)
	require.NoError(t, api.ArchivePage("1004"))

	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "/rest/api/content/archive", path)

	var payload struct {
		Pages []struct {
			ID json.RawMessage `json:"id"`
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Len(t, payload.Pages, 1)
	assert.JSONEq(t, `1004`, string(payload.Pages[0].ID),
		"the id must be sent as a number, not a quoted string")
}

func TestArchivePageRejectsSomethingThatIsNotAnID(t *testing.T) {
	api := confluence.NewAPI("http://127.0.0.1:1", "user", "token", false)

	err := api.ArchivePage("not-a-page")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a page id")
}

// TestArchivePageReportsRefusal covers Server and Data Center, which have no
// archive at all: a run asking for one must not appear to have got it.
func TestArchivePageReportsRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"no such endpoint"}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	err := api.ArchivePage("1004")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}
