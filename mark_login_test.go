package mark

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
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

// TestExportConfigCookiesReachConfluence is the same hop for mark export,
// which builds its own client. The page carries an attachment so that the
// download, which is a request made outside gopencils, is covered as well.
func TestExportConfigCookiesReachConfluence(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	page := server.AddPage("DOCS", "Doc", "page", "")
	server.EditPage(page.ID, `<p><ac:image><ri:attachment ri:filename="chart.png"/></ac:image></p>`)
	server.AddAttachmentData(page.ID, "chart.png", []byte("chart"))

	dir := t.TempDir()
	_, err := Export(context.Background(), ExportConfig{
		BaseURL: server.URL,
		Cookies: []*http.Cookie{{Name: "JSESSIONID", Value: "abc"}},
		PageID:  page.ID,
		Output:  filepath.Join(dir, "doc.md"),
	})
	require.NoError(t, err)

	requests := server.Requests()
	require.NotEmpty(t, requests)

	downloaded := false
	for _, request := range requests {
		assert.Contains(t, request.Header.Get("Cookie"), "JSESSIONID=abc",
			"%s %s", request.Method, request.Path)
		if strings.Contains(request.Path, "/download/") {
			downloaded = true
		}
	}
	assert.True(t, downloaded, "the attachment download should have been among the requests")
}
