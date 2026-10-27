package confluence_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponseHeadersDoNotLeakIntoLaterRequests pins the class of bug where one
// response's headers are echoed back on every request that follows it: a
// single odd Content-Type on any sub-400 response would then be pinned on
// every later PUT and POST body, and Confluence rejects a storage-format
// update announced as anything but application/json.
func TestResponseHeadersDoNotLeakIntoLaterRequests(t *testing.T) {
	var (
		putContentType string
		putOddHeader   string
		putAuth        string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// An odd Content-Type, still JSON, and a header of no
			// interest: neither may come back on the update.
			w.Header().Set("Content-Type", "application/json; charset=iso-8859-1")
			w.Header().Set("X-Odd-Header", "from-a-response")
			_, _ = w.Write([]byte(
				`{"results":[{"id":"1001","title":"Leaky","type":"page",` +
					`"version":{"number":1}}],"_links":{"base":"/wiki"}}`,
			))
		case http.MethodPut:
			putContentType = r.Header.Get("Content-Type")
			putOddHeader = r.Header.Get("X-Odd-Header")
			putAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	// A Personal Access Token: no username, which is the setup that used to
	// install a long-lived header map on the root resource.
	api := confluence.NewAPI(server.URL, "", "pat-token", false)

	page, err := api.FindPage(context.Background(), "DOCS", "Leaky", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	require.NoError(t, api.UpdatePage(context.Background(), page, "<p>body</p>", false, "", "full-width", ""))

	assert.Equal(t, "application/json", putContentType,
		"the GET response's Content-Type must not survive into the update body")
	assert.Empty(t, putOddHeader,
		"no response header may be echoed back on a later request")
	assert.Equal(t, "Bearer pat-token", putAuth,
		"the token still has to be sent on every request")
}

// TestAuthorizationIsSentOnEveryRequest guards the other half of the fix:
// moving the Personal Access Token off the shared root resource must not stop
// it from reaching the wire, on either API version.
func TestAuthorizationIsSentOnEveryRequest(t *testing.T) {
	var seen []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		// One body for both: a listing to the lookup, a folder to the read.
		_, _ = w.Write([]byte(`{"id":"2001","results":[],"_links":{"base":"/wiki"}}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "", "pat-token", false)

	_, err := api.FindPage(context.Background(), "DOCS", "Anything", "page")
	require.NoError(t, err)
	_, err = api.GetFolderByID(context.Background(), "2001")
	require.NoError(t, err)

	require.Len(t, seen, 2)
	for i, got := range seen {
		assert.Equal(t, "Bearer pat-token", got, "request %d carried no token", i)
	}
}
