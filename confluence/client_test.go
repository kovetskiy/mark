package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seen is what a test server was sent.
type seen struct {
	method      string
	rawPath     string
	rawQuery    string
	header      http.Header
	body        []byte
	user, pass  string
	basicAuthOK bool
}

// clientServer answers every request with status, contentType and body, and
// records what it was sent. The client is rooted at basePath on it.
func clientServer(
	t *testing.T, basePath, username, password string, status int, contentType, body string,
) (*client, *seen) {
	t.Helper()

	got := &seen{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.rawPath = r.URL.EscapedPath()
		got.rawQuery = r.URL.RawQuery
		got.header = r.Header.Clone()
		got.body, _ = io.ReadAll(r.Body)
		got.user, got.pass, got.basicAuthOK = r.BasicAuth()

		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		} else {
			// None at all, rather than the one net/http would sniff.
			w.Header()["Content-Type"] = nil
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	return newClient(server.URL+basePath, server.Client(), username, password, ""), got
}

// cancelOnAnswer cancels a request's context as soon as its answer's headers
// are in, which is what a run stopped while the body is read looks like.
type cancelOnAnswer struct {
	base   http.RoundTripper
	cancel context.CancelFunc
}

func (c cancelOnAnswer) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(r)
	c.cancel()

	return response, err
}

// TestClientWordsALoginPageAnsweredAsTheRunStops: a login page that arrived
// as the context ended came back as a cancellation before it was given its
// answer, and formatting it dereferenced the nil answer.
func TestClientWordsALoginPageAnsweredAsTheRunStops(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>Sign in</html>")
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	httpClient := &http.Client{Transport: cancelOnAnswer{base: server.Client().Transport, cancel: cancel}}
	c := newClient(server.URL+"/rest/api", httpClient, "user", "token", "")

	_, err := c.do(ctx, nil, http.MethodGet, []string{"content", "1"}, nil, nil, &PageInfo{})
	require.Error(t, err)
	assert.True(t, IsCancellation(err))
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, err, ErrLoginPage)

	var message string
	require.NotPanics(t, func() { message = requestError("read page 1", err).Error() })
	assert.Contains(t, message, "200 OK")
	assert.Contains(t, message, "/rest/api/content/1")

	require.NotPanics(t, func() { _ = (&loginPageError{requested: "x", evidence: "y"}).Error() })
}

func TestClientBuildsThePath(t *testing.T) {
	for name, test := range map[string]struct {
		base   string
		method string
		path   []string
		want   string
	}{
		"escapes each segment": {
			"/wiki/rest/api", http.MethodGet,
			[]string{"content", "a b", "x/y", "q?#", "mark:key"},
			"/wiki/rest/api/content/a%20b/x%2Fy/q%3F%23/mark:key",
		},
		"keeps a trailing slash": {
			"/rest/api", http.MethodGet, []string{"content", ""}, "/rest/api/content/",
		},
		// The site client has no base path of its own.
		"at the site root": {
			"", http.MethodPost, []string{"pages", "movepage.action"}, "/pages/movepage.action",
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, got := clientServer(t, test.base, "user", "token", http.StatusOK, "", `{}`)

			_, err := c.do(context.Background(), nil, test.method, test.path, nil, nil, &map[string]any{})
			require.NoError(t, err)

			assert.Equal(t, test.want, got.rawPath)
		})
	}
}

func TestClientEncodesTheQuerySorted(t *testing.T) {
	c, got := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "", `{}`)

	query := url.Values{
		"title":    {"A & B"},
		"cql":      {`type=folder AND title="x"`},
		"spaceKey": {"DOCS"},
	}
	_, err := c.do(context.Background(), nil, http.MethodGet, []string{"search"}, query, nil, &map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, query.Encode(), got.rawQuery)
	assert.Equal(t, "cql=type%3Dfolder+AND+title%3D%22x%22&spaceKey=DOCS&title=A+%26+B", got.rawQuery)

	_, err = c.do(context.Background(), nil, http.MethodGet, []string{"search"}, nil, nil, &map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, got.rawQuery, "no query, no question mark")
}

func TestClientSendsAndDecodesJSON(t *testing.T) {
	c, got := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "application/json; charset=utf-8", `{"id":"42","title":"T"}`+"\n")

	var page PageInfo
	response, err := c.do(context.Background(), nil, http.MethodPost, []string{"content"}, nil,
		map[string]any{"title": "T"}, &page)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "application/json", got.header.Get("Content-Type"))
	assert.JSONEq(t, `{"title":"T"}`, string(got.body))
	assert.Equal(t, "42", page.ID)
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

// TestClientSendsAReaderAsItIs: an attachment upload names its own
// Content-Type, which must be the only one.
func TestClientSendsAReaderAsItIs(t *testing.T) {
	c, got := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "", `{}`)

	_, err := c.do(context.Background(),
		http.Header{
			"Content-Type":      {"multipart/form-data; boundary=boundary"},
			"X-Atlassian-Token": {"no-check"},
		},
		http.MethodPost, []string{"content", "1", "child", "attachment"}, nil,
		bytes.NewBufferString("--boundary--"), &map[string]any{},
	)
	require.NoError(t, err)

	assert.Equal(t, "--boundary--", string(got.body))
	assert.Equal(t, []string{"multipart/form-data; boundary=boundary"}, got.header.Values("Content-Type"))
	assert.Equal(t, "no-check", got.header.Get("X-Atlassian-Token"))
}

// TestClientReturnsTheResponseWithADecodeError: a 2xx whose body does not
// parse comes back with the response, and newTransportError says what
// answered and how.
func TestClientReturnsTheResponseWithADecodeError(t *testing.T) {
	c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "application/json", "not json")

	response, err := c.do(context.Background(), nil, http.MethodGet, []string{"content"}, nil, nil, &map[string]any{})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrLoginPage)
	require.NotNil(t, response)
	assert.Equal(t, http.StatusOK, response.StatusCode)

	transport := requestError("read things", err)
	assert.Contains(t, transport.Error(), "body that is not JSON")
	assert.Contains(t, transport.Error(), "/rest/api/content")
}

// TestClientRecognisesALoginPage: an SSO proxy answering an API call with its
// login form, in HTML, is a login page and not a body that fails to parse.
func TestClientRecognisesALoginPage(t *testing.T) {
	for _, contentType := range []string{"text/html; charset=utf-8", "application/xhtml+xml"} {
		c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, contentType, "<html>log in</html>")

		var page PageInfo
		response, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "1"}, nil, nil, &page)
		require.ErrorIs(t, err, ErrLoginPage, contentType)
		require.NotNil(t, response)
		assert.Contains(t, err.Error(), "/rest/api/content/1")
		assert.Contains(t, err.Error(), "200 OK")

		transport := requestError("read page 1", err)
		require.ErrorIs(t, transport, ErrLoginPage, "newTransportError keeps the sentinel")
		assert.Contains(t, transport.Error(), "unable to read page 1")
		assert.Contains(t, transport.Error(), "login page")
		assert.Contains(t, transport.Error(), "SSO", "and the cause worth checking first")
	}
}

// TestClientRecognisesARedirectToTheLoginPage: Seraph sends a request it does
// not authenticate to /login.action, which the HTTP client follows. Whatever
// that page answers with, the request ended on a login page, and the error
// names both where it was sent and where it ended up.
func TestClientRecognisesARedirectToTheLoginPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login.action" {
			http.Redirect(w, r, "/login.action?os_destination=x", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "please log in")
	}))
	t.Cleanup(server.Close)

	c := newClient(server.URL+"/rest/api", server.Client(), "user", "token", "")

	_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "1"}, nil, nil, &PageInfo{})
	require.ErrorIs(t, err, ErrLoginPage)
	assert.Contains(t, err.Error(), server.URL+"/rest/api/content/1", "where the request was sent")
	assert.Contains(t, err.Error(), "/login.action", "and where it ended up")

	transport := requestError("read page 1", err)
	require.ErrorIs(t, transport, ErrLoginPage)
	assert.Contains(t, transport.Error(), "unable to read page 1")
	assert.Contains(t, transport.Error(), "redirected to "+server.URL+"/login.action")
	assert.Contains(t, transport.Error(), "SSO")
}

// TestClientRecognisesARedirectToAnotherHost: an identity provider on another
// host is a login page too, unless what it answered is JSON.
func TestClientRecognisesARedirectToAnotherHost(t *testing.T) {
	for contentType, login := range map[string]bool{
		"text/plain":       true,
		"application/json": false,
	} {
		idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentType)
			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(idp.Close)

		// localhost rather than 127.0.0.1: the same server, another host.
		target := strings.Replace(idp.URL, "127.0.0.1", "localhost", 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target+"/sso", http.StatusFound)
		}))
		t.Cleanup(server.Close)

		c := newClient(server.URL+"/rest/api", server.Client(), "user", "token", "")

		_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "1"}, nil, nil, &map[string]any{})
		if login {
			require.ErrorIs(t, err, ErrLoginPage, contentType)
			assert.Contains(t, err.Error(), "redirected to localhost")
		} else {
			require.NoError(t, err, contentType)
		}
	}
}

// TestClientDoesNotDecodeAFailure: a status outside 2xx is a *StatusError
// carrying the status and the body, and nothing is decoded.
func TestClientDoesNotDecodeAFailure(t *testing.T) {
	for _, status := range []int{400, 401, 404, 409, 500, 503} {
		c, _ := clientServer(t, "/rest/api", "user", "token", status, "", `{"id":"not-decoded","message":"no"}`)

		var page PageInfo
		_, err := c.do(context.Background(), nil, http.MethodPut, []string{"content", "1"}, nil, map[string]any{}, &page)
		var statusErr *StatusError
		require.ErrorAs(t, err, &statusErr, "status %d", status)
		assert.Equal(t, status, statusErr.StatusCode)
		assert.Contains(t, statusErr.URL, "/rest/api/content/1")
		assert.Empty(t, page.ID, "status %d", status)

		assert.JSONEq(t, `{"id":"not-decoded","message":"no"}`, string(statusErr.Body), "status %d", status)
	}
}

// TestStatusErrorMatchesItsSentinels: a caller asks whether something is gone,
// or whether its write conflicted, with errors.Is.
func TestStatusErrorMatchesItsSentinels(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusNotFound: ErrNotFound,
		http.StatusConflict: ErrConflict,
	} {
		c, _ := clientServer(t, "/rest/api", "user", "token", status, "application/json", `{}`)

		_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "1"}, nil, nil, &struct{}{})
		require.ErrorIs(t, err, want, "status %d", status)
		assert.Equal(t, status, statusOf(err))

		other := ErrConflict
		if status == http.StatusConflict {
			other = ErrNotFound
		}
		assert.NotErrorIs(t, err, other, "status %d", status)

		wrapped := fmt.Errorf("unable to do the thing: %w", err)
		assert.ErrorIs(t, wrapped, want, "through a wrap too")
	}

	c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusInternalServerError, "", `oops`)
	_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "1"}, nil, nil, &struct{}{})
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.NotErrorIs(t, err, ErrConflict)
	assert.Equal(t, http.StatusInternalServerError, statusOf(err))
	assert.Contains(t, err.Error(), "500 Internal Server Error")
	assert.Contains(t, err.Error(), `"oops"`)
}

// TestClientBoundsAFailureBody: of an error body, the client reads only what
// a StatusError quotes and the byte that says there was more, however large
// the page a proxy answered with.
func TestClientBoundsAFailureBody(t *testing.T) {
	c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusBadGateway, "text/html", strings.Repeat("x", 1<<20))

	response, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "1"}, nil, nil, &struct{}{})
	assert.Len(t, response.Body, maxErrorBody+1)

	var statusErr *StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Len(t, statusErr.Body, maxErrorBody)
	assert.True(t, statusErr.Truncated)
	assert.Contains(t, err.Error(), "(truncated)")
	assert.Less(t, len(err.Error()), 2*maxErrorBody)
}

// TestClientKeepsAFailureWhoseBodyIsCutShort: a status of 400 or more is an
// answer even when its body could not be read to the end, as it was under
// gopencils. Callers act on the status -- a 404 is "not found", a 409 a
// conflict to retry -- and a read error in its place turned every one of
// those into a transport failure.
func TestClientKeepsAFailureWhoseBodyIsCutShort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Promise more than is sent, so the read ends in io.ErrUnexpectedEOF.
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"gone"`))
	}))
	t.Cleanup(server.Close)
	c := newClient(server.URL+"/rest/api", server.Client(), "user", "token", "")

	_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "1"}, nil, nil, &struct{}{})
	require.ErrorIs(t, err, ErrNotFound)
	var statusErr *StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, `{"message":"gone"`, string(statusErr.Body), "what was read is kept")
}

func TestClientDoesNotDecodeNoContent(t *testing.T) {
	c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusNoContent, "", "")

	response, err := c.do(context.Background(), nil, http.MethodDelete, []string{"content", "1"}, nil, nil, &struct{}{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, response.StatusCode)
}

func TestClientWithoutAResponseReturnsNone(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	c := newClient(server.URL+"/rest/api", server.Client(), "user", "token", "")
	server.Close()

	response, err := c.do(context.Background(), nil, http.MethodGet, []string{"content"}, nil, nil, &struct{}{})
	require.Error(t, err)
	assert.False(t, IsCancellation(err), "a refused connection is not the caller giving up")
	assert.Nil(t, response)
}

func TestClientAuthenticates(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		c, got := clientServer(t, "/rest/api", "user", "secret", http.StatusOK, "", `{}`)

		_, err := c.do(context.Background(), nil, http.MethodGet, []string{"user", "current"}, nil, nil, &User{})
		require.NoError(t, err)

		assert.True(t, got.basicAuthOK)
		assert.Equal(t, "user", got.user)
		assert.Equal(t, "secret", got.pass)
	})

	t.Run("bearer", func(t *testing.T) {
		c, got := clientServer(t, "/rest/api", "", "pat", http.StatusOK, "", `{}`)

		_, err := c.do(context.Background(), nil, http.MethodGet, []string{"user", "current"}, nil, nil, &User{})
		require.NoError(t, err)

		assert.False(t, got.basicAuthOK)
		assert.Equal(t, "Bearer pat", got.header.Get("Authorization"))
	})

	t.Run("none", func(t *testing.T) {
		c, got := clientServer(t, "/rest/api", "", "", http.StatusOK, "", `{}`)

		_, err := c.do(context.Background(), nil, http.MethodGet, []string{"user", "current"}, nil, nil, &User{})
		require.NoError(t, err)

		assert.Empty(t, got.header.Get("Authorization"))
	})
}

// TestClientIsAbortedByItsContext: a request to a server that has stopped
// answering ends when its context does, not when the transport's two-minute
// response-header timeout runs out.
func TestClientIsAbortedByItsContext(t *testing.T) {
	arrived := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(server.Close)

	c := newClient(server.URL+"/rest/api", newHTTPClient(false), "user", "token", "")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-arrived
		cancel()
	}()

	started := time.Now()
	response, err := c.do(ctx, nil, http.MethodGet, []string{"content"}, nil, nil, &struct{}{})

	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, IsCancellation(err))
	assert.Nil(t, response)
	assert.Less(t, time.Since(started), 5*time.Second)
}

// TestClientMarksABodyCutShortAsACancellation: a response that arrived and
// then lost its body to the context is a cancellation too, not a body that
// is not JSON.
func TestClientMarksABodyCutShortAsACancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":`)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(server.Close)

	// Cancelled the moment the response is handed back, before its body is
	// read.
	httpClient := server.Client()
	base := httpClient.Transport
	httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		cancel()
		return resp, err
	})

	c := newClient(server.URL+"/rest/api", httpClient, "user", "token", "")

	var page PageInfo
	response, err := c.do(ctx, nil, http.MethodGet, []string{"content", "1"}, nil, nil, &page)

	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, IsCancellation(err))
	require.NotNil(t, response, "the status arrived before the cancellation")
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

// TestClientTracesRequestAndResponse: at TRACE the whole exchange is dumped,
// with the credential redacted, under the client's prefix.
func TestClientTracesRequestAndResponse(t *testing.T) {
	var buffer bytes.Buffer
	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buffer)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"7"}`)
	}))
	t.Cleanup(server.Close)

	c := newClient(server.URL+"/rest/api", server.Client(), "user", "secret", "rest:")

	var page PageInfo
	_, err := c.do(context.Background(), nil, http.MethodPut, []string{"content", "7"}, nil, map[string]any{"title": "T"}, &page)
	require.NoError(t, err)
	assert.Equal(t, "7", page.ID, "the traced response is still decoded")

	var messages []string
	for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
		var entry struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		messages = append(messages, entry.Message)
	}

	require.Len(t, messages, 2)
	assert.True(t, strings.HasPrefix(messages[0], "rest: PUT /rest/api/content/7 HTTP/1.1"), messages[0])
	assert.Contains(t, messages[0], "Authorization: <redacted>")
	assert.Contains(t, messages[0], `{"title":"T"}`)
	assert.True(t, strings.HasPrefix(messages[1], "rest: HTTP/1.1 200 OK"), messages[1])
	assert.Contains(t, messages[1], `{"id":"7"}`)
}

func TestClientReportsABadBaseURL(t *testing.T) {
	c := newClient("http://[::1", http.DefaultClient, "user", "token", "")

	response, err := c.do(context.Background(), nil, http.MethodGet, []string{"content"}, nil, nil, &struct{}{})
	require.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "invalid base URL")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestClientRejectsDataAfterTheJSONValue: an answer is one JSON value. A body
// that goes on after it is not one, however well its start parsed.
func TestClientRejectsDataAfterTheJSONValue(t *testing.T) {
	for _, body := range []string{`{"id":"42"} trailing`, `{"id":"42"}{"id":"43"}`, `{"id":"42"}]`} {
		c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "application/json", body)

		var page PageInfo
		_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "42"}, nil, nil, &page)
		require.Error(t, err, body)
		assert.Contains(t, err.Error(), "after the JSON value", body)

		transport := requestError("read page 42", err)
		assert.Contains(t, transport.Error(), "body that is not JSON", body)
	}
}

// TestClientRejectsAContentTypeThatIsNotJSON: a 2xx that names a type other
// than JSON, and is not a login page, is not decoded as though it were.
func TestClientRejectsAContentTypeThatIsNotJSON(t *testing.T) {
	c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "text/plain", `{"id":"42"}`)

	var page PageInfo
	_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "42"}, nil, nil, &page)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrLoginPage)
	assert.Contains(t, err.Error(), "text/plain")
	assert.Empty(t, page.ID)

	for _, contentType := range []string{"", "application/vnd.atlassian+json"} {
		c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, contentType, `{"id":"42"}`)

		var page PageInfo
		_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "42"}, nil, nil, &page)
		require.NoError(t, err, "Content-Type %q", contentType)
		assert.Equal(t, "42", page.ID)
	}
}

// TestClientTakesAnEmptySuccessAsOne: a 2xx with no body has nothing to
// decode, and is a success whether or not something was asked for, with
// out left as it was.
func TestClientTakesAnEmptySuccessAsOne(t *testing.T) {
	for name, test := range map[string]struct {
		status      int
		contentType string
	}{
		"204":                  {http.StatusNoContent, ""},
		"200, no Content-Type": {http.StatusOK, ""},
		"200, JSON":            {http.StatusOK, "application/json"},
		"201, a non-JSON type": {http.StatusCreated, "text/plain"},
		"202, no Content-Type": {http.StatusAccepted, ""},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := clientServer(t, "/rest/api", "user", "token", test.status, test.contentType, "")

			page := PageInfo{ID: "untouched"}
			response, err := c.do(context.Background(), nil, http.MethodDelete, []string{"content", "1"}, nil, nil, &page)
			require.NoError(t, err)
			assert.Equal(t, test.status, response.StatusCode)
			assert.Equal(t, "untouched", page.ID)

			_, err = c.do(context.Background(), nil, http.MethodDelete, []string{"content", "1"}, nil, nil, nil)
			require.NoError(t, err)
		})
	}

	// No Content-Length either: the body is read and found empty.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(server.Close)
	c := newClient(server.URL+"/rest/api", server.Client(), "user", "token", "")

	page := PageInfo{ID: "untouched"}
	_, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "1"}, nil, nil, &page)
	require.NoError(t, err)
	assert.Equal(t, "untouched", page.ID)
}

// TestClientStreamsALargeSuccess: a success is decoded as it arrives. Only the
// start of it is kept, and at TRACE only that start is dumped, under a note of
// how much more there was.
func TestClientStreamsALargeSuccess(t *testing.T) {
	var buffer bytes.Buffer
	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buffer)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	large := strings.Repeat("x", 4<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"7","body":{"storage":{"value":"`+large+`"}}}`)
	}))
	t.Cleanup(server.Close)

	c := newClient(server.URL+"/rest/api", server.Client(), "user", "secret", "rest:")

	var page PageInfo
	response, err := c.do(context.Background(), nil, http.MethodGet, []string{"content", "7"}, nil, nil, &page)
	require.NoError(t, err)
	assert.Equal(t, "7", page.ID)
	assert.Len(t, page.Body.Storage.Value, len(large), "the whole body is decoded")

	assert.LessOrEqual(t, len(response.Body), traceDumpLimit, "only the start of it is kept")

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	require.Len(t, lines, 2)
	var entry struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &entry))
	assert.True(t, strings.HasPrefix(entry.Message, "rest: HTTP/1.1 200 OK"))
	assert.Contains(t, entry.Message, `{"id":"7"`)
	assert.Contains(t, entry.Message, "more bytes not traced")
	assert.Less(t, len(entry.Message), traceDumpLimit+1024)
}

// TestStatusErrorSaysWhatConfluenceSaid: Confluence words a failure in its
// error body, v1 and v2 each in their own shape, and that is what the error
// shows rather than the body it came in. A body in neither shape is quoted
// as it was.
func TestStatusErrorSaysWhatConfluenceSaid(t *testing.T) {
	for name, test := range map[string]struct {
		status   int
		body     string
		want     string
		wantNot  string
		sentinel error
	}{
		"v1": {
			status: http.StatusBadRequest,
			body: `{"statusCode":400,"data":{"authorized":true,"valid":false,"errors":[]},` +
				`"message":"A page with this title already exists","reason":"Bad Request"}`,
			want:    `400 Bad Request for http://`,
			wantNot: `statusCode`,
		},
		"v1 404": {
			status:   http.StatusNotFound,
			body:     `{"statusCode":404,"message":"No content found with id: 1","reason":"Not Found"}`,
			want:     `404 (Not Found) for http://`,
			sentinel: ErrNotFound,
		},
		"v2": {
			status: http.StatusConflict,
			body: `{"errors":[{"status":409,"code":"CONFLICT","title":"Version conflict",` +
				`"detail":"Version must be incremented on update. Current Version is: 3"}]}`,
			want:     `409 (Conflict) for http://`,
			wantNot:  `"errors"`,
			sentinel: ErrConflict,
		},
		"neither": {
			status: http.StatusBadGateway,
			body:   `<html>bad gateway</html>`,
			want:   `502 Bad Gateway for http://`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := clientServer(t, "/rest/api", "user", "token", test.status, "application/json", test.body)

			_, err := c.do(context.Background(), nil, http.MethodPost, []string{"content", "1"}, nil, map[string]any{}, &struct{}{})
			var statusErr *StatusError
			require.ErrorAs(t, err, &statusErr)
			assert.Contains(t, err.Error(), test.want)
			assert.Contains(t, err.Error(), "/rest/api/content/1")
			if test.wantNot != "" {
				assert.NotContains(t, err.Error(), test.wantNot, "the message, not the body it came in")
			}
			if test.sentinel != nil {
				assert.ErrorIs(t, err, test.sentinel)
			}

			switch name {
			case "v1":
				assert.Equal(t, "A page with this title already exists", statusErr.Message())
				assert.True(t, strings.HasSuffix(err.Error(), ": A page with this title already exists"), err.Error())
			case "v1 404":
				assert.Equal(t, "No content found with id: 1", statusErr.Message())
			case "v2":
				assert.Equal(t, "Version conflict: Version must be incremented on update. Current Version is: 3", statusErr.Message())
			case "neither":
				assert.Empty(t, statusErr.Message())
				assert.Contains(t, err.Error(), `output: "<html>bad gateway</html>"`)
			}
		})
	}
}

// TestClientLetsASentWriteFinish: a write that has been sent runs to its
// answer whatever becomes of the run, since Confluence may have applied it
// already, and taking its answer for a failure would leave what it did
// unrecorded. One not yet sent when the run is stopped is not sent at all.
func TestClientLetsASentWriteFinish(t *testing.T) {
	received := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received++
		w.Header().Set("Content-Type", "application/json")
		w.(http.Flusher).Flush()
		// Long enough for a cancelled request to have given up on the body.
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, `{"id":"42","version":{"number":2}}`)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stopped the moment the answer's headers are in, before its body is read.
	httpClient := server.Client()
	httpClient.Transport = cancelOnAnswer{base: httpClient.Transport, cancel: cancel}
	c := newClient(server.URL+"/rest/api", httpClient, "user", "token", "")

	var page PageInfo
	_, err := c.do(ctx, nil, http.MethodPut, []string{"content", "42"}, nil, map[string]any{}, &page)
	require.NoError(t, err, "the update was applied, and its answer read")
	assert.Equal(t, "42", page.ID)
	assert.Equal(t, 1, received)

	_, err = c.do(ctx, nil, http.MethodPut, []string{"content", "42"}, nil, map[string]any{}, &page)
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, IsCancellation(err))
	assert.Equal(t, 1, received, "a write is not started once the run is stopped")
}

// TestGetAndSendRefuseAnEmptyAnswer: do takes an empty 2xx for success, but a
// call asked for a value gets none from it. A zero PageInfo in its place was
// cached as the page, and the run went on to send requests to "content/".
func TestGetAndSendRefuseAnEmptyAnswer(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		c, _ := clientServer(t, "/rest/api", "user", "token", status, "application/json", "")

		_, err := get[PageInfo](context.Background(), c, []string{"content", "42"}, nil)
		var empty *emptyAnswerError
		require.ErrorAs(t, err, &empty, "get, status %d", status)
		assert.Contains(t, requestError("read page 42", err).Error(), "with an empty body")

		_, err = send[PageInfo](context.Background(), c, http.MethodPost, []string{"content", ""}, map[string]any{})
		require.ErrorAs(t, err, &empty, "send, status %d", status)
	}

	c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "application/json", `{"id":"42"}`)
	page, err := get[PageInfo](context.Background(), c, []string{"content", "42"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "42", page.ID)
}

// TestClientTakesAValueWhoseConnectionFailsAfterIt: a connection that fails
// once the JSON value has come whole -- a proxy closing it before the end of a
// chunked body -- is not a body that goes on after the value. The value is the
// answer, and a write that got it happened.
func TestClientTakesAValueWhoseConnectionFailsAfterIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()

		value := `{"id":"42"}`
		_, _ = fmt.Fprintf(buffered,
			"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n",
			len(value), value)
		_ = buffered.Flush()
		// Closed without the last chunk: reading on fails with
		// io.ErrUnexpectedEOF.
	}))
	t.Cleanup(server.Close)
	c := newClient(server.URL+"/rest/api", server.Client(), "user", "token", "")

	var page PageInfo
	_, err := c.do(context.Background(), nil, http.MethodPut, []string{"content", "42"}, nil, map[string]any{}, &page)
	require.NoError(t, err)
	assert.Equal(t, "42", page.ID)
}

// TestClientCutsShortAWriteBackoff: a write is not cut short once sent, but
// the wait before sending it again after a 429 does nothing on Confluence, and
// a stopped run must not sit it out. It held a cancelled run for a throttled
// tenant's whole Retry-After, three times over, for every write.
func TestClientCutsShortAWriteBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		// Stopped once the throttled answer is on its way.
		cancel()
	}))
	t.Cleanup(server.Close)

	c := newClient(server.URL+"/rest/api", newHTTPClient(false), "user", "token", "")

	started := time.Now()
	_, err := c.do(ctx, nil, http.MethodDelete, []string{"content", "1"}, nil, nil, &struct{}{})

	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, IsCancellation(err))
	assert.Less(t, time.Since(started), 5*time.Second, "the backoff is cut short")
	assert.Equal(t, int32(1), received.Load(), "the write is not sent again")
}

// TestGetAndSendRefuseAnAnswerNamingNoID: "null" and "{}" decode, into a page
// as empty as no body at all. A page or folder Confluence answers with always
// has its ID, and one without was cached and then sent requests to "content/".
func TestGetAndSendRefuseAnAnswerNamingNoID(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"title":"Untitled"}`} {
		c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "application/json", body)

		_, err := get[PageInfo](context.Background(), c, []string{"content", "42"}, nil)
		var empty *emptyAnswerError
		require.ErrorAs(t, err, &empty, "page, %s", body)
		assert.Contains(t, requestError("read page 42", err).Error(), "naming no id")

		_, err = send[FolderInfo](context.Background(), c, http.MethodPost, []string{"folders"}, map[string]any{})
		require.ErrorAs(t, err, &empty, "folder, %s", body)

		_, err = get[contentV2](context.Background(), c, []string{"pages", "42"}, nil)
		require.ErrorAs(t, err, &empty, "v2 content, %s", body)
	}

	// What is not named by an ID is not asked for one.
	c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "application/json", `{}`)
	_, err := get[listing[PageInfo]](context.Background(), c, []string{"content"}, nil)
	require.NoError(t, err)
}

// TestClientDoesNotTraceAnEmptyAnswerAsUnread: a 204, or a 2xx that says its
// body is empty, is not read, and has nothing to read. The trace said "the
// rest of the body was not read" for every one of them, which reads as an
// exchange cut short when looking at a bug report's log.
func TestClientDoesNotTraceAnEmptyAnswerAsUnread(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusOK} {
		var buffer bytes.Buffer
		previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
		log.Logger = zerolog.New(&buffer)
		zerolog.SetGlobalLevel(zerolog.TraceLevel)

		c, _ := clientServer(t, "/rest/api", "user", "token", status, "application/json", "")
		c.trace = &tracer{"rest:"}

		_, err := c.do(context.Background(), nil, http.MethodDelete, []string{"content", "1"}, nil, nil, &struct{}{})

		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)

		require.NoError(t, err)
		assert.Contains(t, buffer.String(), strconv.Itoa(status))
		assert.NotContains(t, buffer.String(), "not read", "status %d", status)
	}
}
