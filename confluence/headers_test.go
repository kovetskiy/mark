package confluence_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponseHeadersDoNotLeakIntoLaterRequests pins the class of bug where one
// response's headers are echoed back on every request that follows it.
//
// gopencils hands the same http.Header map to a resource and to everything
// Res() derives from it, and after each response copies that response's headers
// into the map with Add. With the Authorization header installed on the root
// resource -- which is what a Personal Access Token used to do -- that map
// lived for the whole run. Because gopencils sends the *first* value ever
// recorded for a key, a single odd Content-Type on any sub-400 response pinned
// that Content-Type on every later PUT and POST body, and Confluence rejects a
// storage-format update announced as text/html.
func TestResponseHeadersDoNotLeakIntoLaterRequests(t *testing.T) {
	var (
		putContentType string
		putOddHeader   string
		putAuth        string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// A proxy interstitial or SSO shim in front of Confluence,
			// announcing HTML while still passing the JSON body through.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
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

	page, err := api.FindPage("DOCS", "Leaky", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	require.NoError(t, api.UpdatePage(page, "<p>body</p>", false, "", "full-width", ""))

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
		_, _ = w.Write([]byte(`{"results":[],"_links":{"base":"/wiki"}}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "", "pat-token", false)

	_, err := api.FindPage("DOCS", "Anything", "page")
	require.NoError(t, err)
	_, err = api.GetFolderByID("2001")
	require.NoError(t, err)

	require.Len(t, seen, 2)
	for i, got := range seen {
		assert.Equal(t, "Bearer pat-token", got, "request %d carried no token", i)
	}
}
