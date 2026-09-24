package confluence_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestNonJSONSuccessIsExplainedEverywhere walks the calls that used to wrap the
// HTTP library's error with a bare fmt.Errorf, and so still surfaced an SSO
// login page as `invalid character '<'` with nothing to say where it came from.
//
// Each is tried against a proxy that answers the API path itself and one that
// redirects it to a login page, which the HTTP client follows to a 200.
func TestNonJSONSuccessIsExplainedEverywhere(t *testing.T) {
	calls := map[string]struct {
		call func(*confluence.API) error
		// what the message must name: the operation and its subject
		want []string
		// the API path, which only the non-redirecting proxy leaves in the URL
		path string
	}{
		"CreateFolder": {
			call: func(api *confluence.API) error {
				_, err := api.CreateFolder("98304", "Guides", nil, "")
				return err
			},
			want: []string{"create folder", "Guides", "98304"},
			path: "/api/v2/folders",
		},
		"FindFolder": {
			call: func(api *confluence.API) error {
				_, err := api.FindFolder("DOCS", "Guides", "")
				return err
			},
			want: []string{"search for folder", "Guides"},
			path: "/rest/api/search",
		},
		"FindChildFolder": {
			call: func(api *confluence.API) error {
				_, err := api.FindChildFolder("1004", "page", "Guides")
				return err
			},
			want: []string{"look for folder", "Guides", "1004"},
			path: "/api/v2/pages/1004/direct-children",
		},
		"FindRootFolder": {
			call: func(api *confluence.API) error {
				_, err := api.FindRootFolder("DOCS", "Guides")
				return err
			},
			want: []string{"search for folder", "Guides"},
			path: "/rest/api/search",
		},
		"GetFolderByID": {
			call: func(api *confluence.API) error {
				_, err := api.GetFolderByID("2001")
				return err
			},
			want: []string{"read folder 2001"},
			path: "/api/v2/folders/2001",
		},
		"GetSpaceID": {
			call: func(api *confluence.API) error {
				_, err := api.GetSpaceID("DOCS")
				return err
			},
			want: []string{"look up the id of space DOCS"},
			path: "/api/v2/spaces",
		},
		"FindHomePage": {
			call: func(api *confluence.API) error {
				_, err := api.FindHomePage("DOCS")
				return err
			},
			want: []string{"read space DOCS", "look up space DOCS"},
			path: "/rest/api/space/DOCS",
		},
		"GetChildPages": {
			call: func(api *confluence.API) error {
				_, err := api.GetChildPages("1004")
				return err
			},
			want: []string{"list child pages of 1004"},
			path: "/rest/api/content/1004/child/page",
		},
		"HasChildFolders": {
			call: func(api *confluence.API) error {
				_, err := api.HasChildFolders("1004")
				return err
			},
			want: []string{"list direct children of 1004"},
			path: "/api/v2/pages/1004/direct-children",
		},
		"DeletePage": {
			call: func(api *confluence.API) error {
				return api.DeletePage("1004")
			},
			want: []string{"delete content 1004"},
			path: "/rest/api/content/1004",
		},
		"ArchivePage": {
			call: func(api *confluence.API) error {
				return api.ArchivePage("1004")
			},
			want: []string{"archive content 1004"},
			path: "/rest/api/content/archive",
		},
		"MoveContentAppend": {
			call: func(api *confluence.API) error {
				return api.MoveContentAppend("1004", "1005")
			},
			want: []string{"move content 1004 append 1005"},
			path: "/rest/api/content/1004/move/append/1005",
		},
	}

	proxies := map[string]func() *httptest.Server{
		"login page": ssoLoginPage,
		"redirect to login page": func() *httptest.Server {
			return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/login" {
					http.Redirect(w, r, "/login", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte("<!doctype html><html><body>Sign in to continue</body></html>"))
			}))
		},
	}

	for proxyName, proxy := range proxies {
		for name, tc := range calls {
			t.Run(proxyName+"/"+name, func(t *testing.T) {
				server := proxy()
				defer server.Close()

				api := confluence.NewAPI(server.URL, "user", "token", false)

				err := tc.call(api)
				require.Error(t, err)

				for _, want := range tc.want {
					assert.Contains(t, err.Error(), want)
				}
				assert.Contains(t, err.Error(), server.URL, "the URL that answered")
				assert.Contains(t, err.Error(), "200 OK", "the status it answered with")
				assert.Contains(t, err.Error(), "SSO", "the cause worth checking first")
				if proxyName == "login page" {
					assert.Contains(t, err.Error(), tc.path)
				}
			})
		}
	}
}

// TestStatusErrorBodyIsBounded: the body of a failed response is quoted in the
// error, and an HTML error page from a proxy can be arbitrarily large.
func TestStatusErrorBodyIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("x", 1<<20)))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	err := api.DeletePage("1004")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502 Bad Gateway")
	assert.Contains(t, err.Error(), "(truncated)")
	assert.Less(t, len(err.Error()), 8192)
}
