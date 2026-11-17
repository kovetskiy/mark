package confluence

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
				"attach": func() error {
					_, err := api.CreateAttachment(ctx, page.ID, "a.txt", "", strings.NewReader("a"))
					return err
				},
				"unwatch": func() error { return api.UnwatchPage(ctx, page.ID, "user") },
			} {
				err := write()
				require.ErrorIs(t, err, ErrWriteRedirected, name)
				assert.Contains(t, err.Error(), "was redirected to "+server.URL, name)
				assert.Contains(t, err.Error(), "served at "+server.URL+", use that address in the base URL", name)
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
		require.NoError(t, api.DownloadAttachment(ctx, "1", info, io.Discard), name)
	}

	assert.Len(t, seen.all(), 8)
	for _, header := range seen.all() {
		assert.Empty(t, header)
	}
}

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

// TestRedirectKeepsAuthorizationUnderABaseURLWithAUserName: a user name in
// the base URL goes along on a relative redirect and makes it no other
// server, so the request keeps the header it was given.
func TestRedirectKeepsAuthorizationUnderABaseURLWithAUserName(t *testing.T) {
	seen := &authorizations{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/moved" {
			http.Redirect(w, r, "/moved", http.StatusFound)
			return
		}
		seen.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	base := strings.Replace(server.URL, "://", "://name@", 1)
	_, err := NewAPI(base, Options{Password: "token"}).GetPageByID(context.Background(), "1")
	require.NoError(t, err)
	assert.Equal(t, []string{"Bearer token"}, seen.all())
}

// dataCenterBehind is an API for the fake dressed as Data Center, which has
// no content move endpoint, reached through a front that answers a post to
// movepage.action itself, with answer, and hands everything else on.
func dataCenterBehind(t *testing.T, answer func(w http.ResponseWriter, r *http.Request, server http.Handler)) (*API, *confluencetest.Server) {
	t.Helper()

	server := confluencetest.New(t)
	server.SetDataCenter()
	server.SetFail(func(r *http.Request) (int, string, bool) {
		return http.StatusNotFound, `{"message":"no such endpoint"}`, strings.Contains(r.URL.Path, "/move/")
	})

	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(address)

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/pages/movepage.action" {
			answer(w, r, proxy)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	api := NewAPI(front.URL, Options{Username: "user", Password: "token"})
	api.moveSettleDelay = 0

	return api, server
}

// TestMoveActionThatRedirectsToThePageHasMovedIt: movepage.action may answer
// with a redirect to the page it moved, so where the page is decides.
func TestMoveActionThatRedirectsToThePageHasMovedIt(t *testing.T) {
	api, server := dataCenterBehind(t, func(w http.ResponseWriter, r *http.Request, server http.Handler) {
		server.ServeHTTP(httptest.NewRecorder(), r)
		http.Redirect(w, r, "/rest/api/content/"+r.URL.Query().Get("pageId"), http.StatusFound)
	})
	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)

	require.NoError(t, api.MoveContentBefore(context.Background(), second.ID, first.ID))
	assert.Equal(t, []string{second.ID, first.ID}, server.ChildOrder(parent.ID))
}

// TestMoveActionRedirectedAsAReadSaysSo: a redirect that kept the post from
// movepage.action is what the failed move is put down to.
func TestMoveActionRedirectedAsAReadSaysSo(t *testing.T) {
	api, server := dataCenterBehind(t, func(w http.ResponseWriter, r *http.Request, _ http.Handler) {
		http.Redirect(w, r, "/rest/api/content/"+r.URL.Query().Get("pageId"), http.StatusMovedPermanently)
	})
	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)

	err := api.MoveContentBefore(context.Background(), second.ID, first.ID)
	require.ErrorIs(t, err, ErrWriteRedirected)
	assert.Equal(t, []string{first.ID, second.ID}, server.ChildOrder(parent.ID))

	// The redirect stayed on the server, which is no other address to use.
	assert.NotContains(t, err.Error(), "is served at")
	assert.Contains(t, err.Error(), "not one that redirects to it")
}

// TestRedirectToAnotherServerIsWarnedOfOnceWhenFollowed: the warning that a
// request goes on without its header is for the redirect that leaves the
// server, not for each one after it, and not for one that is not followed.
func TestRedirectToAnotherServerIsWarnedOfOnceWhenFollowed(t *testing.T) {
	var buffer bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&buffer)
	t.Cleanup(func() { log.Logger = previous })

	seen := &authorizations{}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/moved" {
			http.Redirect(w, r, "/moved", http.StatusFound)
			return
		}
		seen.ServeHTTP(w, r)
	}))
	t.Cleanup(other.Close)
	front := redirectingFront(t, other.URL, http.StatusFound)
	const warning = "goes on without its Authorization header"

	_, err := NewAPI(front.URL, Options{Password: "token"}).GetPageByID(context.Background(), "1")
	require.NoError(t, err)
	assert.Equal(t, []string{""}, seen.all())
	assert.Equal(t, 1, strings.Count(buffer.String(), warning), buffer.String())

	buffer.Reset()
	unfollowed := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	_, err = NewAPI(front.URL, Options{Password: "token", HTTPClient: unfollowed}).GetPageByID(context.Background(), "1")
	require.Error(t, err)
	assert.Zero(t, strings.Count(buffer.String(), warning), buffer.String())
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
		{"a user name carried on", []string{"https://name@wiki.example.org/a", "https://name@wiki.example.org/b"}, true},
		{"a user name left behind", []string{"https://name@wiki.example.org/a", "https://wiki.example.org/b"}, true},
		{"a user name of the redirect's", []string{"https://wiki.example.org/a", "https://other@wiki.example.org/b"}, false},
		{"a user name carried to another port", []string{"https://name@wiki.example.org/a", "https://name@wiki.example.org:8443/a"}, false},
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

// advised is the address a redirected write's error says to use in the base
// URL, or "" when it names none.
func advised(err error) string {
	match := regexp.MustCompile(`; if Confluence is served at (\S+), use that address in the base URL$`).
		FindStringSubmatch(err.Error())
	if match == nil {
		return ""
	}

	return match[1]
}

// TestWriteRedirectedNamesTheBaseURLThatReachesIt: when the redirect leads
// to the same request under another base URL, that base URL is the address
// to use, context path included, and a write made with it arrives.
func TestWriteRedirectedNamesTheBaseURLThatReachesIt(t *testing.T) {
	ctx := context.Background()
	server := confluencetest.New(t)
	added := server.AddPage("DOCS", "Page", "page", "")

	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(address)

	// Confluence under /wiki, with everything else of the server redirecting
	// there: a path under /confluence to the same one under /wiki.
	underWiki := http.NewServeMux()
	underWiki.Handle("/wiki/", http.StripPrefix("/wiki", proxy))
	underWiki.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/wiki"+strings.TrimPrefix(r.URL.RequestURI(), "/confluence"), http.StatusMovedPermanently)
	})
	behind := httptest.NewServer(underWiki)
	t.Cleanup(behind.Close)

	// Another server, redirecting to the first under the path it was asked.
	front := redirectingFront(t, behind.URL, http.StatusFound)

	withName := func(base string) string { return strings.Replace(base, "://", "://name@", 1) }

	for name, base := range map[string]string{
		"another server, to a context path":        front.URL,
		"another server, a context path replaced":  front.URL + "/confluence",
		"another server, with a trailing slash":    front.URL + "/confluence/",
		"the same server, a context path added":    behind.URL,
		"the same server, a context path replaced": behind.URL + "/confluence",
		"a user name in the base URL":              withName(behind.URL),
		"a user name and another server":           withName(front.URL) + "/confluence",
	} {
		t.Run(name, func(t *testing.T) {
			api := NewAPI(base, Options{Username: "user", Password: "token"})
			page, err := api.GetPageByID(ctx, added.ID)
			require.NoError(t, err)

			err = api.UpdatePage(ctx, page, "<p>"+name+"</p>", UpdateOptions{})
			require.ErrorIs(t, err, ErrWriteRedirected)
			require.Equal(t, behind.URL+"/wiki", advised(err), err.Error())

			api = NewAPI(advised(err), Options{Username: "user", Password: "token"})
			page, err = api.GetPageByID(ctx, added.ID)
			require.NoError(t, err)
			require.NoError(t, api.UpdatePage(ctx, page, "<p>"+name+"</p>", UpdateOptions{}))

			stored, err := api.GetPageByIDExpanded(ctx, added.ID, "body.storage")
			require.NoError(t, err)
			assert.Equal(t, "<p>"+name+"</p>", stored.Body.Storage.Value)
		})
	}
}

// TestWriteRedirectedAdvice: the address named is the base URL under which
// the request would have been the one the redirect led to; a redirect that
// is to no such request names the server when it is another, and nothing
// when it is not.
func TestWriteRedirectedAdvice(t *testing.T) {
	const none = ""

	for _, test := range []struct {
		name   string
		base   string
		target string
		want   string
	}{
		{"http to https", "http://wiki.example.org", "https://wiki.example.org/rest/api/content/7?status=current", "https://wiki.example.org"},
		{"http to https under a context path", "http://wiki.example.org/wiki", "https://wiki.example.org/wiki/rest/api/content/7?status=current", "https://wiki.example.org/wiki"},
		{"another host", "https://old.example.org", "https://wiki.example.org/rest/api/content/7?status=current", "https://wiki.example.org"},
		{"another port", "https://wiki.example.org", "https://wiki.example.org:8443/rest/api/content/7?status=current", "https://wiki.example.org:8443"},
		{"a context path added", "https://wiki.example.org", "https://wiki.example.org/wiki/rest/api/content/7?status=current", "https://wiki.example.org/wiki"},
		{"a deeper context path added", "https://wiki.example.org/a", "https://wiki.example.org/a/b/c/rest/api/content/7?status=current", "https://wiki.example.org/a/b/c"},
		{"a context path removed", "https://wiki.example.org/wiki", "https://wiki.example.org/rest/api/content/7?status=current", "https://wiki.example.org"},
		{"a context path replaced", "https://wiki.example.org/confluence", "https://wiki.example.org/wiki/rest/api/content/7?status=current", "https://wiki.example.org/wiki"},
		{"another scheme and host, and a context path added", "http://old.example.org", "https://wiki.example.org/wiki/rest/api/content/7?status=current", "https://wiki.example.org/wiki"},
		{"another host, and a context path removed", "https://old.example.org/wiki", "https://wiki.example.org/rest/api/content/7?status=current", "https://wiki.example.org"},
		{"an escaped context path", "https://wiki.example.org", "https://wiki.example.org/our%20wiki/rest/api/content/7?status=current", "https://wiki.example.org/our%20wiki"},
		{"a trailing slash on the base URL", "https://wiki.example.org/", "https://wiki.example.org/wiki/rest/api/content/7?status=current", "https://wiki.example.org/wiki"},
		{"a user name in the base URL", "https://name:word@wiki.example.org", "https://name:word@wiki.example.org/wiki/rest/api/content/7?status=current", "https://wiki.example.org/wiki"},
		{"a user name in the base URL, and another host", "https://name:word@old.example.org/a", "https://wiki.example.org/wiki/rest/api/content/7?status=current", "https://wiki.example.org/wiki"},

		{"the same server, an unrelated path", "https://wiki.example.org", "https://wiki.example.org/maintenance.html", none},
		{"the same server, the same request", "https://wiki.example.org/wiki", "https://wiki.example.org/wiki/rest/api/content/7?status=current", none},
		{"the same server, the query dropped", "https://wiki.example.org", "https://wiki.example.org/wiki/rest/api/content/7", none},
		{"the same server, another query", "https://wiki.example.org", "https://wiki.example.org/wiki/rest/api/content/7?status=current&os_destination=x", none},
		{"the same server, a slash at the end", "https://wiki.example.org", "https://wiki.example.org/wiki/rest/api/content/7/?status=current", none},
		{"the same server, a doubled slash", "https://wiki.example.org", "https://wiki.example.org/wiki//rest/api/content/7?status=current", none},
		{"the same server, a path that only ends alike", "https://wiki.example.org", "https://wiki.example.org/wiki/other/api/content/7?status=current", none},

		{"another server, an unrelated path", "http://wiki.example.org/wiki", "https://wiki.example.org/maintenance.html", "https://wiki.example.org"},
		{"another server, the query dropped", "http://wiki.example.org", "https://wiki.example.org/wiki/rest/api/content/7", "https://wiki.example.org"},
		{"another server, a slash at the end", "http://wiki.example.org", "https://wiki.example.org/wiki/rest/api/content/7/?status=current", "https://wiki.example.org"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := NewAPI(test.base, Options{}).v1
			requested, err := url.Parse(c.url([]string{"content", "7"}, url.Values{"status": {"current"}}))
			require.NoError(t, err)
			target, err := url.Parse(test.target)
			require.NoError(t, err)

			err = &redirectedWriteError{
				method:    http.MethodPut,
				requested: requested,
				appended:  c.appended(requested),
				answer:    &reply{Status: "200 OK", Request: &http.Request{Method: http.MethodGet, URL: target}},
			}

			assert.Equal(t, test.want, advised(err), err.Error())
			assert.NotContains(t, err.Error(), "word")
			if test.want == none {
				assert.Contains(t, err.Error(), "not one that redirects to it")
			}
		})
	}
}

// TestWriteRedirectedAdviceComparesTheQueryByItsValues: a redirect that
// writes the same query another way is still to the same request.
func TestWriteRedirectedAdviceComparesTheQueryByItsValues(t *testing.T) {
	c := NewAPI("https://wiki.example.org", Options{}).v1
	requested, err := url.Parse(c.url([]string{"content"}, url.Values{"title": {"a b"}, "spaceKey": {"DOCS"}}))
	require.NoError(t, err)
	target, err := url.Parse("https://wiki.example.org/wiki/rest/api/content?title=a%20b&spaceKey=DOCS")
	require.NoError(t, err)

	err = &redirectedWriteError{
		method:    http.MethodPost,
		requested: requested,
		appended:  c.appended(requested),
		answer:    &reply{Status: "200 OK", Request: &http.Request{Method: http.MethodGet, URL: target}},
	}
	assert.Equal(t, "https://wiki.example.org/wiki", advised(err), err.Error())
}
