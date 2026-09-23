package mark

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigCookiesReachConfluence covers the last hop of --login: cookies
// resolved by the CLI have to end up on the wire. Everything before this is
// tested in the browser and util packages; this holds the wiring in Run.
func TestConfigCookiesReachConfluence(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n")

	config := Config{
		BaseURL: server.URL,
		Cookies: []*http.Cookie{{Name: "JSESSIONID", Value: "abc"}},
		Files:   filepath.Join(dir, "doc.md"),
		Output:  io.Discard,
	}
	require.NoError(t, Run(config))

	requests := server.Requests()
	require.NotEmpty(t, requests)

	for _, request := range requests {
		assert.Contains(t, request.Header.Get("Cookie"), "JSESSIONID=abc",
			"%s %s", request.Method, request.Path)
	}
}
