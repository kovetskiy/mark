package confluence

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
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

	assert.Equal(t, "image/png", attachments[0].Extensions.MediaType)
	assert.Equal(t, "image/png", attachments[0].Metadata.MediaType)
	require.NotNil(t, attachments[0].Extensions.FileSize)
	assert.Equal(t, int64(len("content")), *attachments[0].Extensions.FileSize)

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

// TestDownloadAttachmentAuthentication: Seraph saying it did not
// authenticate the download is a login page whatever type the answer names,
// JSON included, since an attachment answers in its own type. An anonymous
// session is not one: a public space serves its attachments to it.
func TestDownloadAttachmentAuthentication(t *testing.T) {
	for _, test := range []struct {
		name, contentType, reason, username, body, evidence string
	}{
		{"failed HTML", "text/html; charset=UTF-8", "AUTHENTICATED_FAILED", "", "<html>Log in</html>", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"empty failed HTML", "text/html", "AUTHENTICATED_FAILED", "", "", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"failed XHTML", "application/xhtml+xml", "AUTHENTICATION_DENIED", "", "<html/>", "X-Seraph-LoginReason: AUTHENTICATION_DENIED"},
		{"failed binary", "application/octet-stream", "AUTHENTICATED_FAILED", "", "\x00\xff", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"failed without type", "", "AUTHENTICATED_FAILED", "", "", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"failed JSON", "application/json; charset=utf-8", "AUTHENTICATED_FAILED", "", `{"attachment":true}`, "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"failed structured JSON", "Application/Vnd.Atlassian+Json; charset=UTF-8", "AUTHENTICATED_FAILED", "", `{}`, "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"failed and anonymous", "text/html", "AUTHENTICATED_FAILED", "anonymous", "login", "X-Seraph-LoginReason: AUTHENTICATED_FAILED"},
		{"anonymous", "text/html", "", "anonymous", "<html>Attachment</html>", ""},
		{"empty anonymous", "text/plain", "", "anonymous", "", ""},
		{"anonymous and OK", "image/png", "OK", "anonymous", "\x89PNG", ""},
		{"binary", "application/octet-stream", "", "", "\x00\xff\x80content", ""},
		{"HTML", "text/html", "", "", "<html>Attachment</html>", ""},
		{"XHTML", "application/xhtml+xml", "", "", "<html/>", ""},
		{"OK HTML", "text/html", "OK", "user", "<html>Attachment</html>", ""},
		{"authenticated binary", "application/octet-stream", "", "user", "content", ""},
		{"empty attachment", "application/octet-stream", "OK", "", "", ""},
		{"JSON is not decoded", "application/json", "", "", "not JSON", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := confluencetest.New(t)
			server.SetFailHeaders(http.Header{
				"Content-Type": {test.contentType}, "X-Seraph-LoginReason": {test.reason}, "X-Ausername": {test.username},
			})
			server.SetFail(func(*http.Request) (int, string, bool) {
				return http.StatusOK, test.body, true
			})
			api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
			body := observeDownload(api)
			var got bytes.Buffer
			err := api.DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), &got)
			assert.True(t, body.closed)
			if test.evidence != "" {
				require.ErrorIs(t, err, ErrLoginPage)
				assert.Empty(t, got.Bytes())
				assert.Zero(t, body.read)
				assert.Contains(t, err.Error(), `unable to download attachment "a.png"`)
				assert.Contains(t, err.Error(), test.evidence)
				assert.Contains(t, err.Error(), "did not accept the credentials", "Seraph refused them")
				assert.NotContains(t, err.Error(), "SSO", "no proxy is to blame")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.body, got.String())
		})
	}
}

// TestDownloadAttachmentWithoutCredentials: with no credentials every answer
// from Data Center says X-AUSERNAME: anonymous, and a public space's
// attachments are still the attachments.
func TestDownloadAttachmentWithoutCredentials(t *testing.T) {
	server := confluencetest.New(t)
	server.SetFailHeaders(http.Header{"Content-Type": {"image/png"}, "X-Ausername": {"anonymous"}})
	server.SetFail(func(*http.Request) (int, string, bool) { return http.StatusOK, "\x89PNG", true })

	var got bytes.Buffer
	require.NoError(t, NewAPI(server.URL, Options{}).
		DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), &got))
	assert.Equal(t, "\x89PNG", got.String())
}

// TestDownloadAttachmentRedirects: a download may be redirected, to another
// path or, on Cloud, to a media host, and may be of an attachment named
// login.action; one redirected to Seraph's /login.action, on any host and with
// no Seraph headers, is a login page, and so is one redirected to an identity
// provider that answers in HTML for an attachment recorded as a PNG.
func TestDownloadAttachmentRedirects(t *testing.T) {
	for _, test := range []struct {
		name, link, target, evidence string
	}{
		{"same host", "/redirect", "/download/attachments/PAGE/a.png", ""},
		{"another host", "/redirect", "OTHER/download/attachments/PAGE/a.png", ""},
		{"login.action attachment", "/download/attachments/PAGE/login.action", "", ""},
		{"header-free login redirect", "/redirect", "/login.action?os_destination=x", "redirected to /login.action"},
		{"login redirect to another host", "/redirect", "OTHER/login.action", "redirected to /login.action"},
		{"identity provider on another host", "/redirect", "OTHER/idp/sso", "Confluence records as image/png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := confluencetest.New(t)
			page := server.AddPage("DOCS", "Page", "page", "")
			server.AddAttachmentData(page.ID, "a.png", []byte("attachment"))
			server.AddAttachmentData(page.ID, "login.action", []byte("attachment"))
			other := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
			target := strings.NewReplacer("PAGE", page.ID, "OTHER", other).Replace(test.target)
			if !strings.HasPrefix(target, "http") {
				target = server.URL + target
			}
			server.SetFailHeaders(http.Header{"Location": {target}, "Content-Type": {"text/html"}})
			server.SetFail(func(r *http.Request) (int, string, bool) {
				if r.URL.Path == "/redirect" {
					return http.StatusFound, "", true
				}
				return http.StatusOK, "<html>Log in</html>", r.URL.Path == "/login.action" || r.URL.Path == "/idp/sso"
			})
			api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
			attachments, err := api.GetAttachments(context.Background(), page.ID)
			require.NoError(t, err)
			info := attachments[0]
			require.Equal(t, "a.png", info.Filename)
			info.Links.Context, info.Links.Download = "", strings.ReplaceAll(test.link, "PAGE", page.ID)

			var got bytes.Buffer
			err = api.DownloadAttachment(context.Background(), page.ID, info, &got)
			if test.evidence != "" {
				require.ErrorIs(t, err, ErrLoginPage)
				assert.Contains(t, err.Error(), test.evidence)
				assert.Empty(t, got.Bytes())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "attachment", got.String())
		})
	}
}

// TestDownloadAttachmentThroughTheLoginPage: Seraph may send a download to
// /login.action and on to an identity provider whose form, on another host,
// says nothing of Seraph; going through /login.action is what gives it away.
func TestDownloadAttachmentThroughTheLoginPage(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>Sign in</html>"))
	}))
	t.Cleanup(idp.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login.action" {
			http.Redirect(w, r, idp.URL+"/sso", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/login.action?os_destination=x", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	var got bytes.Buffer
	err := NewAPI(server.URL, Options{Username: "user", Password: "token"}).
		DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), &got)
	require.ErrorIs(t, err, ErrLoginPage)
	assert.Contains(t, err.Error(), "redirected to /login.action")
	assert.Empty(t, got.Bytes())
}

// TestDownloadAttachmentBackFromTheLoginPage: Seraph may send a download to
// /login.action, log it in there and send it back with a session; the
// attachment it then answers with is the download.
func TestDownloadAttachmentBackFromTheLoginPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login.action" {
			http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "s", Path: "/"})
			http.Redirect(w, r, r.URL.Query().Get("os_destination"), http.StatusFound)
			return
		}
		if _, err := r.Cookie("JSESSIONID"); err != nil {
			http.Redirect(w, r, "/login.action?os_destination="+r.URL.Path, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("attachment"))
	}))
	t.Cleanup(server.Close)

	var got bytes.Buffer
	require.NoError(t, NewAPI(server.URL, Options{Username: "user", Password: "token"}).
		DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), &got))
	assert.Equal(t, "attachment", got.String())
}

// TestDownloadAttachmentRefusedOnTheWayToTheLoginPage: Seraph names a refusal
// on the redirect to /login.action, and the login page itself may not repeat
// it; the error blames the credentials all the same.
func TestDownloadAttachmentRefusedOnTheWayToTheLoginPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login.action" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>Log in</html>"))
			return
		}
		w.Header().Set("X-Seraph-LoginReason", "AUTHENTICATED_FAILED")
		http.Redirect(w, r, "/login.action?os_destination=x", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	err := NewAPI(server.URL, Options{Username: "user", Password: "token"}).
		DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), io.Discard)
	require.ErrorIs(t, err, ErrLoginPage)
	assert.Contains(t, err.Error(), "X-Seraph-LoginReason: AUTHENTICATED_FAILED")
	assert.Contains(t, err.Error(), "did not accept the credentials")
}

func TestDownloadAttachmentStatusBeforeAuthentication(t *testing.T) {
	server := confluencetest.New(t)
	server.SetFailHeaders(http.Header{"Content-Type": {"text/html"}, "X-Seraph-LoginReason": {"AUTHENTICATED_FAILED"}})
	server.SetFail(func(*http.Request) (int, string, bool) {
		return http.StatusForbidden, "login", true
	})
	api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
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
	api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
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

// TestDownloadAttachmentLimitsTheBody: an endless body is read no further than
// one byte past the limit, without the test holding a gigabyte.
func TestDownloadAttachmentLimitsTheBody(t *testing.T) {
	api := NewAPI("https://confluence.example.org", Options{Username: "user", Password: "token"})
	body := &downloadBody{ReadCloser: io.NopCloser(endless{})}
	api.v1.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{}, Body: body, Request: r,
		}, nil
	})
	err := api.DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), io.Discard)
	require.ErrorContains(t, err, "larger than 1073741824 bytes")
	assert.Equal(t, int64(maxAttachmentDownload+1), body.read)
	assert.True(t, body.closed)
}

// endless is a body that never ends.
type endless struct{}

func (endless) Read(p []byte) (int, error) { return len(p), nil }

func TestDownloadAttachmentKeepsIOErrors(t *testing.T) {
	t.Run("writer", func(t *testing.T) {
		server := confluencetest.New(t)
		page := server.AddPage("DOCS", "Page", "page", "")
		server.AddAttachmentData(page.ID, "a.png", []byte("content"))
		api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
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
		api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
		body := observeDownload(api)
		err := api.DownloadAttachment(context.Background(), "1", downloadInfo("1", "a.png"), io.Discard)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		assert.True(t, body.closed)
	})
}

func TestDownloadAttachmentKeepsEarlyErrors(t *testing.T) {
	info := downloadInfo("1", "a.png")
	t.Run("missing link", func(t *testing.T) {
		err := NewAPI("https://example.com", Options{Username: "user", Password: "token"}).
			DownloadAttachment(context.Background(), "1", AttachmentInfo{Filename: "a.png"}, io.Discard)
		require.ErrorContains(t, err, "has no download link")
	})
	t.Run("invalid base", func(t *testing.T) {
		err := NewAPI("http://[::1", Options{Username: "user", Password: "token"}).
			DownloadAttachment(context.Background(), "1", info, io.Discard)
		require.ErrorContains(t, err, "unable to parse base URL")
	})
	t.Run("invalid link", func(t *testing.T) {
		invalid := info
		invalid.Links.Download = "%zz"
		err := NewAPI("https://example.com", Options{Username: "user", Password: "token"}).
			DownloadAttachment(context.Background(), "1", invalid, io.Discard)
		require.ErrorContains(t, err, "unable to parse download link")
	})
	t.Run("missing client", func(t *testing.T) {
		api := NewAPI("https://example.com", Options{Username: "user", Password: "token"})
		api.v1 = nil
		require.ErrorIs(t, api.DownloadAttachment(context.Background(), "1", info, io.Discard), errNoClient)
	})
	t.Run("cancelled", func(t *testing.T) {
		server := confluencetest.New(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := NewAPI(server.URL, Options{Username: "user", Password: "token"}).DownloadAttachment(ctx, "1", info, io.Discard)
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, server.Requests())
	})
	t.Run("transport", func(t *testing.T) {
		server := confluencetest.New(t)
		api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
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

// recordedDownload lists one attachment, a.png, with record merged into what
// the listing says of it, and answers its download with serve. It returns the
// attachment as listed and an API for the server.
func recordedDownload(t *testing.T, record map[string]any, serve http.HandlerFunc) (AttachmentInfo, *API) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/content/1/child/attachment" {
			serve(w, r)
			return
		}
		attachment := map[string]any{
			"id": "att1", "title": "a.png",
			"_links": map[string]any{"download": "/download/attachments/1/a.png"},
		}
		maps.Copy(attachment, record)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{attachment}, "size": 1})
	}))
	t.Cleanup(server.Close)

	api := NewAPI(server.URL, Options{Username: "user", Password: "token"})
	attachments, err := api.GetAttachments(context.Background(), "1")
	require.NoError(t, err)
	require.Len(t, attachments, 1)

	return attachments[0], api
}

// record is what Confluence's v1 listing says of a file of mediaType and
// size, in both places it says it.
func record(mediaType string, size int) map[string]any {
	return map[string]any{
		"metadata":   map[string]any{"mediaType": mediaType, "comment": ""},
		"extensions": map[string]any{"mediaType": mediaType, "fileSize": size},
	}
}

// answer serves body as contentType, with no Seraph headers.
func answer(contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}
}

// TestDownloadAttachmentAgainstTheRecord: an identity provider or a proxy
// that answers a download with its login form, on any host and with nothing
// of Seraph about it, answers in HTML for an attachment Confluence records as
// something else; and a body that is not the size Confluence records is not
// the attachment.
func TestDownloadAttachmentAgainstTheRecord(t *testing.T) {
	const png = "\x89PNG-image"
	idp := httptest.NewServer(answer("text/html; charset=UTF-8", "<html>Sign in</html>"))
	t.Cleanup(idp.Close)
	media := httptest.NewServer(answer("image/png", png))
	t.Cleanup(media.Close)

	t.Run("an identity provider on another host", func(t *testing.T) {
		info, api := recordedDownload(t, record("image/png", len(png)), func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, idp.URL+"/saml/sso", http.StatusFound)
		})
		var got bytes.Buffer
		err := api.DownloadAttachment(context.Background(), "1", info, &got)
		require.ErrorIs(t, err, ErrLoginPage)
		assert.Contains(t, err.Error(), "an HTML Content-Type for an attachment Confluence records as image/png")
		assert.Contains(t, err.Error(), idp.URL+"/saml/sso")
		assert.Contains(t, err.Error(), "SSO or a proxy")
		assert.Empty(t, got.Bytes())
	})

	t.Run("a proxy on the same host", func(t *testing.T) {
		info, api := recordedDownload(t, record("image/png", len(png)), answer("application/xhtml+xml", "<html/>"))
		var got bytes.Buffer
		err := api.DownloadAttachment(context.Background(), "1", info, &got)
		require.ErrorIs(t, err, ErrLoginPage)
		assert.Empty(t, got.Bytes())
	})

	t.Run("an HTML attachment", func(t *testing.T) {
		const page = "<html>An attachment</html>"
		info, api := recordedDownload(t, record("text/html", len(page)), answer("text/html;charset=UTF-8", page))
		var got bytes.Buffer
		require.NoError(t, api.DownloadAttachment(context.Background(), "1", info, &got))
		assert.Equal(t, page, got.String())
	})

	t.Run("a media host serving the attachment", func(t *testing.T) {
		info, api := recordedDownload(t, record("image/png", len(png)), func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, media.URL+"/file/att1/binary", http.StatusFound)
		})
		var got bytes.Buffer
		require.NoError(t, api.DownloadAttachment(context.Background(), "1", info, &got))
		assert.Equal(t, png, got.String())
	})

	t.Run("a compressed answer", func(t *testing.T) {
		info, api := recordedDownload(t, record("image/png", len(png)), func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.Header.Get("Accept-Encoding"), "gzip")
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Encoding", "gzip")
			compressed := gzip.NewWriter(w)
			_, _ = compressed.Write([]byte(png))
			_ = compressed.Close()
		})
		var got bytes.Buffer
		require.NoError(t, api.DownloadAttachment(context.Background(), "1", info, &got))
		assert.Equal(t, png, got.String())
	})

	t.Run("a Content-Length that is not the recorded size", func(t *testing.T) {
		info, api := recordedDownload(t, record("image/png", len(png)+1), answer("image/png", png))
		body := observeDownload(api)
		var got bytes.Buffer
		err := api.DownloadAttachment(context.Background(), "1", info, &got)
		require.ErrorIs(t, err, ErrAttachmentSizeMismatch)
		assert.NotErrorIs(t, err, ErrLoginPage)
		assert.Contains(t, err.Error(), fmt.Sprintf("%d bytes where Confluence records %d", len(png), len(png)+1))
		assert.Empty(t, got.Bytes())
		assert.Zero(t, body.read)
	})

	t.Run("an HTML answer that is not the recorded size", func(t *testing.T) {
		recorded := map[string]any{"extensions": map[string]any{"fileSize": len(png)}}
		info, api := recordedDownload(t, recorded, answer("text/html", "<html>Sign in</html>"))
		var got bytes.Buffer
		err := api.DownloadAttachment(context.Background(), "1", info, &got)
		require.ErrorIs(t, err, ErrLoginPage)
		assert.Empty(t, got.Bytes())
	})

	t.Run("a body without a length that is not the recorded size", func(t *testing.T) {
		long := strings.Repeat("x", 4096)
		info, api := recordedDownload(t, record("image/png", len(long)-1), func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte(long[:10]))
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(long[10:]))
		})
		var got bytes.Buffer
		err := api.DownloadAttachment(context.Background(), "1", info, &got)
		require.ErrorIs(t, err, ErrAttachmentSizeMismatch)
		assert.Contains(t, err.Error(), fmt.Sprintf("%d bytes where Confluence records %d", len(long), len(long)-1))
	})

	t.Run("nothing recorded", func(t *testing.T) {
		info, api := recordedDownload(t, nil, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, idp.URL+"/saml/sso", http.StatusFound)
		})
		var got bytes.Buffer
		require.NoError(t, api.DownloadAttachment(context.Background(), "1", info, &got))
		assert.Equal(t, "<html>Sign in</html>", got.String())
	})

	t.Run("only metadata's media type", func(t *testing.T) {
		recorded := map[string]any{"metadata": map[string]any{"mediaType": "image/png"}}
		info, api := recordedDownload(t, recorded, answer("text/html", "<html>Sign in</html>"))
		require.ErrorIs(t, api.DownloadAttachment(context.Background(), "1", info, io.Discard), ErrLoginPage)
	})

	t.Run("a size that is not a number", func(t *testing.T) {
		recorded := map[string]any{"extensions": map[string]any{"mediaType": "image/png", "fileSize": "big"}}
		info, api := recordedDownload(t, recorded, answer("image/png", png))
		var got bytes.Buffer
		require.NoError(t, api.DownloadAttachment(context.Background(), "1", info, &got))
		assert.Equal(t, png, got.String())
	})
}

// TestAttachmentExtensions: what Confluence records about a file is read where
// it is what Confluence gives, and left out otherwise, without failing the
// listing it is in.
func TestAttachmentExtensions(t *testing.T) {
	size := func(n int64) *int64 { return &n }
	for _, test := range []struct {
		name, json string
		want       AttachmentExtensions
	}{
		{"both", `{"mediaType":"image/png","fileSize":42,"comment":"c"}`, AttachmentExtensions{"image/png", size(42)}},
		{"empty file", `{"mediaType":"text/plain","fileSize":0}`, AttachmentExtensions{"text/plain", size(0)}},
		{"neither", `{}`, AttachmentExtensions{}},
		{"null size", `{"fileSize":null}`, AttachmentExtensions{}},
		{"size as text", `{"fileSize":"42"}`, AttachmentExtensions{}},
		{"fractional size", `{"fileSize":4.2}`, AttachmentExtensions{}},
		{"negative size", `{"fileSize":-1}`, AttachmentExtensions{}},
		{"media type not text", `{"mediaType":7,"fileSize":3}`, AttachmentExtensions{"", size(3)}},
		{"not an object", `"x"`, AttachmentExtensions{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var info AttachmentInfo
			require.NoError(t, json.Unmarshal([]byte(`{"title":"a.png","extensions":`+test.json+`}`), &info))
			assert.Equal(t, "a.png", info.Filename)
			assert.Equal(t, test.want, info.Extensions)
		})
	}
}
