package confluence

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
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
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewAPI(test.base, "u", "p", false).AttachmentURL("1", test.info)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}

	for name, link := range map[string]string{
		"another host": "https://elsewhere.example/a.png",
		"plain http":   "http://example.com/download/attachments/1/a.png",
		"another port": "https://example.com:8443/download/attachments/1/a.png",
	} {
		t.Run("an absolute link to "+name+" is refused", func(t *testing.T) {
			_, err := NewAPI("https://example.com", "u", "p", false).
				AttachmentURL("1", attachment("", link))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to send credentials")
		})
	}

	t.Run("through the gateway, the REST download endpoint", func(t *testing.T) {
		info := attachment("/wiki", "/download/attachments/1/a.png")
		info.ID = "att2"
		got, err := NewAPI("https://api.atlassian.com/ex/confluence/cloud", "", "token", false).AttachmentURL("1", info)
		require.NoError(t, err)
		assert.Equal(t, "https://api.atlassian.com/ex/confluence/cloud/rest/api/content/1/child/attachment/att2/download", got)
	})
}

func TestDownloadAttachment(t *testing.T) {
	server := confluencetest.New(t)
	page := server.AddPage("DOCS", "Page", "page", "")
	server.AddAttachmentData(page.ID, "a b#1.png", []byte("content"))

	api := NewAPI(server.URL, "user", "token", false)
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

func TestDownloadAttachmentAuthentication(t *testing.T) {
	for _, test := range []struct {
		name, contentType, reason, username, body, evidence string
	}{
		{"failed HTML", "text/html; charset=UTF-8", "AUTHENTICATED_FAILED", "", "<html>Log in</html>", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"empty failed HTML", "text/html", "AUTHENTICATED_FAILED", "", "", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"failed XHTML", "application/xhtml+xml", "AUTHENTICATION_DENIED", "", "<html/>", "X-Seraph-LoginReason: AUTHENTICATION_DENIED"},
		{"failed binary", "application/octet-stream", "AUTHENTICATED_FAILED", "", "\x00\xff", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"failed without type", "", "AUTHENTICATED_FAILED", "", "", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"anonymous", "text/html", "", "anonymous", "<html>Log in</html>", "X-AUSERNAME: anonymous"},
		{"empty anonymous", "text/plain", "", "anonymous", "", "X-AUSERNAME: anonymous"},
		{"failed reason before anonymous", "text/html", "AUTHENTICATED_FAILED", "anonymous", "login", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"anonymous despite OK", "text/html", "OK", "anonymous", "login", "X-AUSERNAME: anonymous"},
		{"binary", "application/octet-stream", "", "", "\x00\xff\x80content", ""},
		{"HTML", "text/html", "", "", "<html>Attachment</html>", ""},
		{"XHTML", "application/xhtml+xml", "", "", "<html/>", ""},
		{"OK HTML", "text/html", "OK", "user", "<html>Attachment</html>", ""},
		{"authenticated binary", "application/octet-stream", "", "user", "content", ""},
		{"empty attachment", "application/octet-stream", "OK", "", "", ""},
		{"JSON exemption", "application/json; charset=utf-8", "AUTHENTICATED_FAILED", "anonymous", `{"attachment":true}`, ""},
		{"structured JSON exemption", "Application/Vnd.Atlassian+Json; charset=UTF-8", "AUTHENTICATED_FAILED", "anonymous", `{"attachment":true}`, ""},
		{"JSON is not decoded", "application/json", "AUTHENTICATED_FAILED", "", "not JSON", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := confluencetest.New(t)
			server.SetFailHeaders(http.Header{
				"Content-Type": {test.contentType}, "X-Seraph-LoginReason": {test.reason}, "X-Ausername": {test.username},
			})
			server.SetFail(func(*http.Request) (int, string, bool) {
				return http.StatusOK, test.body, true
			})
			api := NewAPI(server.URL, "user", "token", false)
			body := observeDownload(api)
			var got bytes.Buffer
			err := api.DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), &got)
			assert.True(t, body.closed)
			if test.evidence != "" {
				assert.ErrorIs(t, err, ErrLoginPage)
				assert.Empty(t, got.Bytes())
				assert.Zero(t, body.read)
				if err != nil {
					assert.Contains(t, err.Error(), `unable to download attachment "a.png"`)
					assert.Contains(t, err.Error(), test.evidence)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.body, got.String())
		})
	}
}

func TestDownloadAttachmentRedirects(t *testing.T) {
	for _, name := range []string{"same host", "another host", "login.action attachment", "header-free login redirect"} {
		t.Run(name, func(t *testing.T) {
			server := confluencetest.New(t)
			page := server.AddPage("DOCS", "Page", "page", "")
			server.AddAttachmentData(page.ID, "login.action", []byte("attachment"))
			target := server.URL + "/download/attachments/" + page.ID + "/login.action"
			if name == "another host" {
				target = strings.Replace(target, "127.0.0.1", "localhost", 1)
			}
			if name == "header-free login redirect" {
				target = server.URL + "/login.action"
			}
			server.SetFailHeaders(http.Header{"Location": {target}, "Content-Type": {"text/html"}})
			server.SetFail(func(r *http.Request) (int, string, bool) {
				if r.URL.Path == "/redirect" {
					return http.StatusFound, "", true
				}
				return http.StatusOK, "attachment", r.URL.Path == "/login.action"
			})
			info := downloadInfo(page.ID, "login.action")
			info.Links.Download = "/redirect"
			if name == "login.action attachment" {
				info.Links.Download = target
			}
			var got bytes.Buffer
			require.NoError(t, NewAPI(server.URL, "user", "token", false).
				DownloadAttachment(context.Background(), page.ID, info, &got))
			assert.Equal(t, "attachment", got.String())
		})
	}
}

func TestDownloadAttachmentKeepsAPIAuthenticationChecks(t *testing.T) {
	for _, test := range []struct {
		contentType, reason, username string
		login                         bool
	}{
		{"text/html", "AUTHENTICATED_FAILED", "", true},
		{"text/plain", "", "anonymous", true},
		{"application/json", "AUTHENTICATED_FAILED", "anonymous", false},
		{"application/vnd.atlassian+json", "AUTHENTICATED_FAILED", "anonymous", false},
		{"text/plain", "OK", "user", false},
	} {
		t.Run(test.contentType+test.reason+test.username, func(t *testing.T) {
			server := confluencetest.New(t)
			server.SetFailHeaders(http.Header{
				"Content-Type": {test.contentType}, "X-Seraph-LoginReason": {test.reason}, "X-Ausername": {test.username},
			})
			server.SetFail(func(*http.Request) (int, string, bool) {
				return http.StatusOK, `{"id":"1","title":"Page"}`, true
			})
			_, err := NewAPI(server.URL, "user", "token", false).GetPageByID(context.Background(), "1")
			if test.login {
				require.ErrorIs(t, err, ErrLoginPage)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDownloadAttachmentStatusBeforeAuthentication(t *testing.T) {
	server := confluencetest.New(t)
	server.SetFailHeaders(http.Header{"Content-Type": {"text/html"}, "X-Seraph-LoginReason": {"AUTHENTICATED_FAILED"}})
	server.SetFail(func(*http.Request) (int, string, bool) {
		return http.StatusForbidden, "login", true
	})
	api := NewAPI(server.URL, "user", "token", false)
	body := observeDownload(api)
	var got bytes.Buffer
	err := api.DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), &got)
	require.ErrorContains(t, err, "403 Forbidden")
	assert.NotErrorIs(t, err, ErrLoginPage)
	assert.Empty(t, got.Bytes())
	assert.True(t, body.closed)
}

func TestDownloadAttachmentStreamsAndCloses(t *testing.T) {
	server := confluencetest.New(t)
	page := server.AddPage("DOCS", "Page", "page", "")
	data := bytes.Repeat([]byte("x"), 4<<20)
	server.AddAttachmentData(page.ID, "a.png", data)
	api := NewAPI(server.URL, "user", "token", false)
	body := observeDownload(api)
	written := 0
	writer := downloadWriter(func(p []byte) (int, error) {
		if written == 0 {
			assert.Less(t, body.read, int64(len(data)))
		}
		written += len(p)
		return len(p), nil
	})
	require.NoError(t, api.DownloadAttachment(context.Background(), page.ID, downloadInfo(page.ID, "a.png"), writer))
	assert.Equal(t, len(data), written)
	assert.True(t, body.closed)
}

func TestDownloadAttachmentLimitsTheBody(t *testing.T) {
	server := confluencetest.New(t)
	page := server.AddPage("DOCS", "Page", "page", "")
	attachment := server.AddAttachmentData(page.ID, "a.png", nil)
	attachment.Data = make([]byte, maxAttachmentDownload+2)
	api := NewAPI(server.URL, "user", "token", false)
	body := observeDownload(api)
	err := api.DownloadAttachment(context.Background(), page.ID, downloadInfo(page.ID, "a.png"), io.Discard)
	require.ErrorContains(t, err, "larger than 1073741824 bytes")
	assert.Equal(t, int64(maxAttachmentDownload+1), body.read)
	assert.True(t, body.closed)
}

func TestDownloadAttachmentKeepsIOErrors(t *testing.T) {
	t.Run("writer", func(t *testing.T) {
		server := confluencetest.New(t)
		page := server.AddPage("DOCS", "Page", "page", "")
		server.AddAttachmentData(page.ID, "a.png", []byte("content"))
		api := NewAPI(server.URL, "user", "token", false)
		body := observeDownload(api)
		failure := errors.New("destination failed")
		writer := downloadWriter(func([]byte) (int, error) { return 0, failure })
		err := api.DownloadAttachment(context.Background(), page.ID, downloadInfo(page.ID, "a.png"), writer)
		require.ErrorIs(t, err, failure)
		assert.True(t, body.closed)
	})
	t.Run("reader", func(t *testing.T) {
		server := confluencetest.New(t)
		server.SetFailHeaders(http.Header{"Content-Length": {"100"}})
		server.SetFail(func(*http.Request) (int, string, bool) { return http.StatusOK, "short", true })
		api := NewAPI(server.URL, "user", "token", false)
		body := observeDownload(api)
		err := api.DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), io.Discard)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		assert.True(t, body.closed)
	})
}

func TestDownloadAttachmentKeepsEarlyErrors(t *testing.T) {
	info := downloadInfo("1", "a.png")
	t.Run("missing link", func(t *testing.T) {
		err := NewAPI("https://example.com", "user", "token", false).
			DownloadAttachment(context.Background(), "1", AttachmentInfo{Filename: "a.png"}, io.Discard)
		require.ErrorContains(t, err, "has no download link")
	})
	t.Run("invalid base", func(t *testing.T) {
		err := NewAPI("http://[::1", "user", "token", false).
			DownloadAttachment(context.Background(), "1", info, io.Discard)
		require.ErrorContains(t, err, "unable to parse base URL")
	})
	t.Run("invalid link", func(t *testing.T) {
		invalid := info
		invalid.Links.Download = "%zz"
		err := NewAPI("https://example.com", "user", "token", false).
			DownloadAttachment(context.Background(), "1", invalid, io.Discard)
		require.ErrorContains(t, err, "unable to parse download link")
	})
	t.Run("missing client", func(t *testing.T) {
		api := NewAPI("https://example.com", "user", "token", false)
		api.v1 = nil
		require.ErrorIs(t, api.DownloadAttachment(context.Background(), "1", info, io.Discard), errNoClient)
	})
	t.Run("cancelled", func(t *testing.T) {
		server := confluencetest.New(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := NewAPI(server.URL, "user", "token", false).DownloadAttachment(ctx, "1", info, io.Discard)
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, server.Requests())
	})
	t.Run("transport", func(t *testing.T) {
		server := confluencetest.New(t)
		api := NewAPI(server.URL, "user", "token", false)
		failure := errors.New("transport failed")
		api.v1.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, failure })
		require.ErrorIs(t, api.DownloadAttachment(context.Background(), "1", info, io.Discard), failure)
	})
}

func downloadInfo(pageID, filename string) AttachmentInfo {
	info := AttachmentInfo{Filename: filename}
	info.Links.Download = "/download/attachments/" + pageID + "/" + filename
	return info
}

type downloadBody struct {
	io.ReadCloser
	read   int64
	closed bool
}

func (b *downloadBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	return n, err
}

func (b *downloadBody) Close() error {
	b.closed = true
	return b.ReadCloser.Close()
}

func observeDownload(api *API) *downloadBody {
	body := &downloadBody{}
	base := api.v1.http.Transport
	api.v1.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response, err := base.RoundTrip(r)
		if err != nil {
			return response, err
		}
		body.ReadCloser = response.Body
		response.Body = body
		return response, nil
	})
	return body
}

type downloadWriter func([]byte) (int, error)

func (w downloadWriter) Write(p []byte) (int, error) { return w(p) }
