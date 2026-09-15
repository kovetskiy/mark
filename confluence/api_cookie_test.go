package confluence_test

import (
	"net/http"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCookieAuthSendsCookieHeader covers the whole point of --login: the
// captured session has to reach Confluence on every call.
func TestCookieAuthSendsCookieHeader(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "", "", false,
		confluence.WithCookies([]*http.Cookie{{Name: "JSESSIONID", Value: "abc"}}))

	server.AddPage("DOCS", "Getting Started", "page", "")

	_, err := api.FindPage("DOCS", "Getting Started", "page")
	require.NoError(t, err)

	requests := server.Requests()
	require.NotEmpty(t, requests)
	assert.Contains(t, requests[0].Header.Get("Cookie"), "JSESSIONID=abc")
}

// TestCookieAuthSetsXSRFHeader holds the header without which Confluence
// rejects every cookie-authenticated write. Nothing else in mark sends it, so
// nothing else would catch its removal.
func TestCookieAuthSetsXSRFHeader(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "", "", false,
		confluence.WithCookies([]*http.Cookie{{Name: "JSESSIONID", Value: "abc"}}))

	server.AddPage("DOCS", "Getting Started", "page", "")

	// Fetched rather than constructed: PageInfo.Version is an anonymous
	// struct, which cannot be written as a literal without repeating its
	// tags, and FindPage is the way every other test gets one.
	page, err := api.FindPage("DOCS", "Getting Started", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	server.ResetRequests()

	require.NoError(t, api.UpdatePage(page, "<p>body</p>", false, "", "", ""))

	var sawWrite bool
	for _, request := range server.Requests() {
		if request.Method == http.MethodPut || request.Method == http.MethodPost {
			sawWrite = true
			assert.Equal(t, "no-check", request.Header.Get("X-Atlassian-Token"),
				"%s %s", request.Method, request.Path)
		}
	}
	assert.True(t, sawWrite, "expected the update to issue a write request")
}

// TestBasicAuthSendsNoXSRFHeader holds the other half: the existing auth paths
// are untouched by this feature.
func TestBasicAuthSendsNoXSRFHeader(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	server.AddPage("DOCS", "Getting Started", "page", "")

	_, err := api.FindPage("DOCS", "Getting Started", "page")
	require.NoError(t, err)

	requests := server.Requests()
	require.NotEmpty(t, requests)
	assert.Empty(t, requests[0].Header.Get("X-Atlassian-Token"))
	assert.Empty(t, requests[0].Header.Get("Cookie"))
}
