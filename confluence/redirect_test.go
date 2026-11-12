package confluence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectingFront is a server that answers every request with status and a
// Location on target, as a proxy that moves a site to another address does.
func redirectingFront(t *testing.T, target string, status int) *httptest.Server {
	t.Helper()

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target+r.URL.RequestURI())
		w.WriteHeader(status)
	}))
	t.Cleanup(front.Close)

	return front
}

// writes counts the requests the fake received that were not reads.
func writes(server *confluencetest.Server) int {
	count := 0
	for _, request := range server.Requests() {
		if !readOnly(request.Method) {
			count++
		}
	}

	return count
}

// TestWriteRedirectedAsAReadIsNotItsAnswer: a 301, 302 or 303 answer to a
// write is followed with a GET that carries no body, and what answers that
// GET says nothing of the write, so the write fails naming where it went.
// Reads through the same redirect go on working.
func TestWriteRedirectedAsAReadIsNotItsAnswer(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx := context.Background()
			server := confluencetest.New(t)
			added := server.AddPage("DOCS", "Page", "page", "")
			front := redirectingFront(t, server.URL, status)
			api := NewAPI(front.URL, Options{Username: "user", Password: "token"})

			page, err := api.GetPageByID(ctx, added.ID)
			require.NoError(t, err)
			version := page.Version.Number

			for name, write := range map[string]func() error{
				"update": func() error { return api.UpdatePage(ctx, page, "<p>new</p>", UpdateOptions{}) },
				"label": func() error {
					_, err := api.AddPageLabels(ctx, page, []string{"one"})
					return err
				},
				"unlabel": func() error { return api.DeletePageLabel(ctx, page, "one") },
				"delete":  func() error { return api.DeletePage(ctx, page.ID) },
			} {
				err := write()
				require.ErrorIs(t, err, ErrWriteRedirected, name)
				assert.Contains(t, err.Error(), "was redirected to "+server.URL, name)
				assert.Contains(t, err.Error(), "as the base URL", name)
				assert.False(t, isUnavailable(err), name)
			}

			assert.Equal(t, version, page.Version.Number, "the page's version is the one Confluence has")
			assert.Zero(t, writes(server))
		})
	}
}

// TestWriteRedirectedWithItsBodyIsMade: a 307 or 308 sends the write on as
// it was, so it reaches Confluence and its answer is the write's.
func TestWriteRedirectedWithItsBodyIsMade(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx := context.Background()
			server := confluencetest.New(t)
			added := server.AddPage("DOCS", "Page", "page", "")
			front := redirectingFront(t, server.URL, status)
			api := NewAPI(front.URL, Options{})

			page, err := api.GetPageByID(ctx, added.ID)
			require.NoError(t, err)
			require.NoError(t, api.UpdatePage(ctx, page, "<p>new</p>", UpdateOptions{}))

			stored, err := NewAPI(server.URL, Options{}).GetPageByIDExpanded(ctx, added.ID, "body.storage,version")
			require.NoError(t, err)
			assert.Equal(t, "<p>new</p>", stored.Body.Storage.Value)
		})
	}
}

// TestWriteRedirectedToALoginPageIsALoginPage: a write sent to log in was
// turned away, which is what its error goes on saying.
func TestWriteRedirectedToALoginPageIsALoginPage(t *testing.T) {
	ctx := context.Background()
	server := confluencetest.New(t)
	added := server.AddPage("DOCS", "Page", "page", "")
	api := NewAPI(server.URL, Options{})
	page, err := api.GetPageByID(ctx, added.ID)
	require.NoError(t, err)

	server.SetFailHeaders(http.Header{"Location": {"/login.action"}, "Content-Type": {"text/html"}})
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/login.action" {
			return http.StatusOK, "<html>Log in</html>", true
		}
		return http.StatusFound, "", !readOnly(r.Method)
	})

	err = api.UpdatePage(ctx, page, "<p>new</p>", UpdateOptions{})
	require.ErrorIs(t, err, ErrLoginPage)
	assert.NotErrorIs(t, err, ErrWriteRedirected)
}

// authorizations records the Authorization header of each request a server
// answers with a page that has nothing but an id.
type authorizations struct {
	mu   sync.Mutex
	seen []string
}

func (a *authorizations) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.seen = append(a.seen, r.Header.Get("Authorization"))
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"1"}`))
}

func (a *authorizations) all() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]string(nil), a.seen...)
}

// TestRedirectToAnotherPortGoesWithoutAuthorization: the Authorization header
// is for the server the base URL names, which another port of its host is
// not, for an API request and for a download alike.
func TestRedirectToAnotherPortGoesWithoutAuthorization(t *testing.T) {
	ctx := context.Background()
	seen := &authorizations{}
	other := httptest.NewServer(seen)
	t.Cleanup(other.Close)
	front := redirectingFront(t, other.URL, http.StatusFound)

	for name, options := range map[string]Options{
		"mark's client":   {Username: "user", Password: "token"},
		"a caller's own":  {Username: "user", Password: "token", HTTPClient: &http.Client{}},
		"a bearer token":  {Password: "token"},
		"caller's policy": {Password: "token", HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}},
	} {
		api := NewAPI(front.URL, options)

		_, err := api.GetPageByID(ctx, "1")
		require.NoError(t, err, name)

		info := AttachmentInfo{Filename: "a.txt"}
		info.Links.Download = "/download/attachments/1/a.txt"
		require.NoError(t, api.DownloadAttachment(ctx, "1", info, &discard{}), name)
	}

	assert.Len(t, seen.all(), 8)
	for _, header := range seen.all() {
		assert.Empty(t, header)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// TestRedirectOnTheSameServerKeepsAuthorization: a redirect to another path
// of the server asked is still a request to it.
func TestRedirectOnTheSameServerKeepsAuthorization(t *testing.T) {
	seen := &authorizations{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/moved" {
			http.Redirect(w, r, "/moved", http.StatusFound)
			return
		}
		seen.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	_, err := NewAPI(server.URL, Options{Password: "token"}).GetPageByID(context.Background(), "1")
	require.NoError(t, err)
	assert.Equal(t, []string{"Bearer token"}, seen.all())
}

// TestCheckRedirectKeepsAuthorizationForTheServerAsked: the header stays for
// the scheme, host and port first asked and for the same host's move from
// http to https, and once it is off it stays off.
func TestCheckRedirectKeepsAuthorizationForTheServerAsked(t *testing.T) {
	for _, test := range []struct {
		name string
		hops []string
		kept bool
	}{
		{"another path", []string{"https://wiki.example.org/a", "https://wiki.example.org/b"}, true},
		{"the default port written out", []string{"https://wiki.example.org/a", "https://wiki.example.org:443/a"}, true},
		{"http to https", []string{"http://wiki.example.org/a", "https://wiki.example.org/a"}, true},
		{"http to https and on", []string{"http://wiki.example.org/a", "https://wiki.example.org/a", "https://wiki.example.org/b"}, true},
		{"https to http", []string{"https://wiki.example.org/a", "http://wiki.example.org/a"}, false},
		{"another port", []string{"https://wiki.example.org/a", "https://wiki.example.org:8443/a"}, false},
		{"http to https on other ports", []string{"http://wiki.example.org:8090/a", "https://wiki.example.org:8443/a"}, false},
		{"a subdomain", []string{"https://example.org/a", "https://media.example.org/a"}, false},
		{"another port and back", []string{"https://wiki.example.org/a", "https://wiki.example.org:8443/a", "https://wiki.example.org/a"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests []*http.Request
			for _, hop := range test.hops {
				address, err := url.Parse(hop)
				require.NoError(t, err)
				requests = append(requests, &http.Request{URL: address, Header: http.Header{"Authorization": {"Bearer token"}}})
			}
			last := len(requests) - 1

			require.NoError(t, checkRedirect(nil)(requests[last], requests[:last]))
			assert.Equal(t, test.kept, requests[last].Header.Get("Authorization") != "")
		})
	}
}

// TestCheckRedirectKeepsTheLimitAndTheCallersPolicy: with no policy of the
// caller's a request follows as many redirects as net/http allows, and with
// one, that policy decides.
func TestCheckRedirectKeepsTheLimitAndTheCallersPolicy(t *testing.T) {
	address, err := url.Parse("https://wiki.example.org/a")
	require.NoError(t, err)
	request := &http.Request{URL: address, Header: http.Header{}}

	assert.NoError(t, checkRedirect(nil)(request, make([]*http.Request, maxRedirects-1)))
	assert.Error(t, checkRedirect(nil)(request, make([]*http.Request, maxRedirects)))

	refused := errors.New("refused")
	policy := checkRedirect(func(*http.Request, []*http.Request) error { return refused })
	assert.ErrorIs(t, policy(request, []*http.Request{request}), refused)
}
