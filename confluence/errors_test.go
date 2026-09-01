package confluence_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ssoLoginPage stands in for a Confluence behind single sign-on: the proxy
// answers 200 with an HTML login form instead of passing the API call through.
// A 200 means the HTTP library decodes the body and hands the decode error
// straight back, so the one place that builds a readable message is skipped.
func ssoLoginPage() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!doctype html><html><body>Sign in to continue</body></html>"))
	}))
}

// TestNonJSONSuccessSaysWhatFailed pins the class of bug where a failure
// surfaces as a bare JSON parse error.
//
// What the user used to get was `invalid character '<' looking for beginning of
// value` -- no URL, no status, no page name, and nothing to suggest an SSO
// proxy was the cause.
func TestNonJSONSuccessSaysWhatFailed(t *testing.T) {
	server := ssoLoginPage()
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	_, err := api.FindPage("DOCS", "Getting Started", "page")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "find page", "the operation has to be named")
	assert.Contains(t, err.Error(), "Getting Started", "and its subject")
	assert.Contains(t, err.Error(), "DOCS")
	assert.Contains(t, err.Error(), server.URL, "and the URL that answered")
	assert.Contains(t, err.Error(), "200 OK", "and the status it answered with")
	assert.Contains(t, err.Error(), "SSO", "and the cause worth checking first")
}

// TestNonJSONSuccessIsExplainedOnEveryVerb: the same proxy sits in front of
// every call, so a create and an update must be as legible as a lookup.
func TestNonJSONSuccessIsExplainedOnEveryVerb(t *testing.T) {
	server := ssoLoginPage()
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	_, err := api.CreatePage("DOCS", "page", nil, "New Page", "<p/>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create page")
	assert.Contains(t, err.Error(), "New Page")

	page := &confluence.PageInfo{ID: "1004", Title: "Existing", Type: "page"}
	err = api.UpdatePage(page, "<p/>", false, "", "full-width", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update page")
	assert.Contains(t, err.Error(), "Existing")
	assert.Contains(t, err.Error(), "1004")
}

// TestStatusErrorsCarryTheURL covers the other half: a status that is not OK
// says which of the several calls mark makes per page produced it.
func TestStatusErrorsCarryTheURL(t *testing.T) {
	for name, status := range map[string]int{
		"unauthorized": http.StatusUnauthorized,
		"not found":    http.StatusNotFound,
		"server error": http.StatusBadRequest,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"no"}`))
			}))
			defer server.Close()

			api := confluence.NewAPI(server.URL, "user", "token", false)

			_, err := api.GetPageByID("1004")
			require.Error(t, err)
			assert.Contains(t, err.Error(), server.URL)
			assert.Contains(t, err.Error(), "/rest/api/content/1004")
		})
	}
}

// TestNotFoundStaysASentinel: callers ask "is this gone?" with errors.Is, and
// adding the URL to the message must not break that.
func TestNotFoundStaysASentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"gone"}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	_, err := api.GetPageByID("1004")
	require.Error(t, err)
	assert.ErrorIs(t, err, confluence.ErrNotFound)
}
