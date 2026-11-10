package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachmentURL(t *testing.T) {
	attachment := func(context, download string) AttachmentInfo {
		info := AttachmentInfo{Filename: "a.png"}
		info.Links.Context = context
		info.Links.Download = download
		return info
	}

	for _, test := range []struct {
		name, base string
		info       AttachmentInfo
		want       string
	}{
		{"a base URL that ends in the context", "https://example.atlassian.net/wiki",
			attachment("/wiki", "/download/attachments/1/a.png"), "https://example.atlassian.net/wiki/download/attachments/1/a.png"},
		{"a base URL without it", "https://example.com",
			attachment("/confluence", "/download/attachments/1/a.png"), "https://example.com/confluence/download/attachments/1/a.png"},
		{"no context", "https://example.com/",
			attachment("", "/download/attachments/1/a.png?version=2"), "https://example.com/download/attachments/1/a.png?version=2"},
		{"an absolute link on the same host", "https://example.com",
			attachment("", "https://example.com/download/attachments/1/a.png"), "https://example.com/download/attachments/1/a.png"},
		{"the default port written out", "https://example.com",
			attachment("", "https://example.com:443/download/attachments/1/a.png"), "https://example.com:443/download/attachments/1/a.png"},
		{"a base URL with a user name", "https://svc@example.com",
			attachment("", "https://example.com/download/attachments/1/a.png"), "https://example.com/download/attachments/1/a.png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewAPI(test.base, Options{Username: "u", Password: "p"}).AttachmentURL("1", test.info)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}

	for name, info := range map[string]AttachmentInfo{
		"a context that extends the host": attachment(".elsewhere.example", "/download/attachments/1/a.png"),
		"a context that is a user":        attachment("@elsewhere.example", "/download/attachments/1/a.png"),
		"a context that is a port":        attachment(":8443", "/download/attachments/1/a.png"),
	} {
		t.Run("a relative link under "+name+" is refused", func(t *testing.T) {
			_, err := NewAPI("https://example.com", Options{Username: "u", Password: "p"}).AttachmentURL("1", info)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to send credentials")
		})
	}

	for name, link := range map[string]string{
		"another host": "https://elsewhere.example/a.png",
		"plain http":   "http://example.com/download/attachments/1/a.png",
		"another port": "https://example.com:8443/download/attachments/1/a.png",
		"a user name":  "https://example.com@elsewhere.example/download/attachments/1/a.png",
		"a lookalike":  "https://example.com.elsewhere.example/download/attachments/1/a.png",
	} {
		t.Run("an absolute link to "+name+" is refused", func(t *testing.T) {
			_, err := NewAPI("https://example.com", Options{Username: "u", Password: "p"}).
				AttachmentURL("1", attachment("", link))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to send credentials")
		})
	}

	t.Run("through the gateway, the REST download endpoint", func(t *testing.T) {
		info := attachment("/wiki", "/download/attachments/1/a.png")
		info.ID = "att2"
		got, err := NewAPI("https://api.atlassian.com/ex/confluence/cloud", Options{Password: "token"}).AttachmentURL("1", info)
		require.NoError(t, err)
		assert.Equal(t, "https://api.atlassian.com/ex/confluence/cloud/rest/api/content/1/child/attachment/att2/download", got)
	})
}

func TestDownloadAttachment(t *testing.T) {
	server := confluencetest.New(t)
	page := server.AddPage("DOCS", "Page", "page", "")
	server.AddAttachmentData(page.ID, "a b#1.png", []byte("content"))

	api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
	attachments, err := api.GetAttachments(context.Background(), page.ID)
	require.NoError(t, err)
	require.Len(t, attachments, 1)

	var got bytes.Buffer
	require.NoError(t, api.DownloadAttachment(context.Background(), page.ID, attachments[0], &got))
	assert.Equal(t, "content", got.String())

	missing := attachments[0]
	missing.Links.Download = "/download/attachments/" + page.ID + "/gone.png"
	err = api.DownloadAttachment(context.Background(), page.ID, missing, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// TestDownloadAttachmentStaysOnServer serves an attachment listing whose
// _links.context, joined to the base URL, names another server, and checks the
// download, and with it the credentials, does not go there.
func TestDownloadAttachmentStaysOnServer(t *testing.T) {
	var elsewhere atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		assert.Empty(t, r.Header.Get("Authorization"), "credentials sent to another server")
		_, _ = w.Write([]byte("stolen"))
	}))
	t.Cleanup(evil.Close)
	evilHost := strings.TrimPrefix(evil.URL, "http://")

	// serve lists one attachment under contextPath and serves its content
	// from there.
	serve := func(t *testing.T, contextPath string) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/rest/api/content/1/child/attachment"):
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"results": []any{map[string]any{
						"id": "att1", "title": "a.png",
						"_links": map[string]any{"download": "/download/attachments/1/a.png"},
					}},
					"size":   1,
					"_links": map[string]any{"context": contextPath},
				})
			case r.URL.Path == contextPath+"/download/attachments/1/a.png":
				_, _ = w.Write([]byte("content"))
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)
		return server
	}

	for _, test := range []struct {
		name, context, base string
	}{
		{"Cloud's /wiki, in the base URL", "/wiki", "/wiki"},
		{"a context path not in the base URL", "/confluence", ""},
		{"no context", "", ""},
	} {
		t.Run(test.name+" downloads", func(t *testing.T) {
			server := serve(t, test.context)
			api := NewAPI(server.URL+test.base, Options{Username: "user", Password: "token"})

			attachments, err := api.GetAttachments(context.Background(), "1")
			require.NoError(t, err)
			require.Len(t, attachments, 1)

			var got bytes.Buffer
			require.NoError(t, api.DownloadAttachment(context.Background(), "1", attachments[0], &got))
			assert.Equal(t, "content", got.String())
		})
	}

	t.Run("a context that names another server is refused", func(t *testing.T) {
		server := serve(t, "@"+evilHost)
		api := NewAPI(server.URL, Options{Username: "user", Password: "token"})

		attachments, err := api.GetAttachments(context.Background(), "1")
		require.NoError(t, err)
		require.Len(t, attachments, 1)

		err = api.DownloadAttachment(context.Background(), "1", attachments[0], &bytes.Buffer{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "refusing to send credentials")
		assert.Zero(t, elsewhere.Load(), "a request went to another server")
	})

}
