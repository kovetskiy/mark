package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	return newClient(server.URL+basePath, server.Client(), username, password, ""), got
}

func TestClientEscapesEachPathSegment(t *testing.T) {
	c, got := clientServer(t, "/wiki/rest/api", "user", "token", http.StatusOK, "", `{}`)

	_, err := c.do(context.Background(), http.MethodGet,
		[]string{"content", "a b", "x/y", "q?#", "mark:key"}, nil, nil, &map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, "/wiki/rest/api/content/a%20b/x%2Fy/q%3F%23/mark:key", got.rawPath)
}

func TestClientKeepsATrailingSlash(t *testing.T) {
	c, got := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "", `{}`)

	_, err := c.do(context.Background(), http.MethodGet, []string{"content", ""}, nil, nil, &map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, "/rest/api/content/", got.rawPath)
}

// TestClientAtTheSiteRoot: the site client has no base path of its own.
func TestClientAtTheSiteRoot(t *testing.T) {
	c, got := clientServer(t, "", "user", "token", http.StatusOK, "", `{}`)

	_, err := c.do(context.Background(), http.MethodPost, []string{"pages", "movepage.action"}, nil, nil, &map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, "/pages/movepage.action", got.rawPath)
}

func TestClientEncodesTheQuerySorted(t *testing.T) {
	c, got := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "", `{}`)

	query := url.Values{
		"title":    {"A & B"},
		"cql":      {`type=folder AND title="x"`},
		"spaceKey": {"DOCS"},
	}
	_, err := c.do(context.Background(), http.MethodGet, []string{"search"}, query, nil, &map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, query.Encode(), got.rawQuery)
	assert.Equal(t, "cql=type%3Dfolder+AND+title%3D%22x%22&spaceKey=DOCS&title=A+%26+B", got.rawQuery)

	_, err = c.do(context.Background(), http.MethodGet, []string{"search"}, nil, nil, &map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, got.rawQuery, "no query, no question mark")
}

func TestClientSendsAndDecodesJSON(t *testing.T) {
	c, got := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "text/html", `{"id":"42","title":"T"} trailing`)

	var page PageInfo
	response, err := c.do(context.Background(), http.MethodPost, []string{"content"}, nil,
		map[string]any{"title": "T"}, &page)
	require.NoError(t, err, "the answer is decoded whatever its Content-Type, and only its first value")

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

	_, err := c.do(context.Background(), http.MethodPost, []string{"content", "1", "child", "attachment"}, nil,
		bytes.NewBufferString("--boundary--"), &map[string]any{},
		withHeader("Content-Type", "multipart/form-data; boundary=boundary"),
		withHeader("X-Atlassian-Token", "no-check"),
	)
	require.NoError(t, err)

	assert.Equal(t, "--boundary--", string(got.body))
	assert.Equal(t, []string{"multipart/form-data; boundary=boundary"}, got.header.Values("Content-Type"))
	assert.Equal(t, "no-check", got.header.Get("X-Atlassian-Token"))
}

// TestClientReturnsTheResponseWithADecodeError: an SSO login page answering
// 200 has to be told apart from a network failure, which only works when the
// response comes back alongside the error.
func TestClientReturnsTheResponseWithADecodeError(t *testing.T) {
	for name, body := range map[string]string{
		"html":  "<html>log in</html>",
		"empty": "",
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusOK, "text/html", body)

			response, err := c.do(context.Background(), http.MethodGet, []string{"content"}, nil, nil, &map[string]any{})
			require.Error(t, err)
			require.NotNil(t, response)
			assert.Equal(t, http.StatusOK, response.StatusCode)

			read, readErr := io.ReadAll(response.Body)
			require.NoError(t, readErr)
			assert.Equal(t, body, string(read), "the body is still there to read")

			transport := newTransportError(response, "read things", err)
			assert.Contains(t, transport.Error(), "body that is not JSON")
			assert.Contains(t, transport.Error(), "/rest/api/content")
		})
	}
}

// TestClientDoesNotDecodeAFailure: a 400 or worse is the caller's to report,
// body and all.
func TestClientDoesNotDecodeAFailure(t *testing.T) {
	for _, status := range []int{400, 401, 404, 409, 500, 503} {
		c, _ := clientServer(t, "/rest/api", "user", "token", status, "", `{"id":"not-decoded","message":"no"}`)

		var page PageInfo
		response, err := c.do(context.Background(), http.MethodPut, []string{"content", "1"}, nil, map[string]any{}, &page)
		require.NoError(t, err, "status %d", status)
		assert.Empty(t, page.ID, "status %d", status)

		read, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"id":"not-decoded","message":"no"}`, string(read), "status %d", status)
	}
}

func TestClientDoesNotDecodeNoContent(t *testing.T) {
	c, _ := clientServer(t, "/rest/api", "user", "token", http.StatusNoContent, "", "")

	response, err := c.do(context.Background(), http.MethodDelete, []string{"content", "1"}, nil, nil, &struct{}{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, response.StatusCode)
}

func TestClientWithoutAResponseReturnsNone(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	c := newClient(server.URL+"/rest/api", server.Client(), "user", "token", "")
	server.Close()

	response, err := c.do(context.Background(), http.MethodGet, []string{"content"}, nil, nil, &struct{}{})
	require.Error(t, err)
	assert.Nil(t, response)
}

func TestClientAuthenticates(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		c, got := clientServer(t, "/rest/api", "user", "secret", http.StatusOK, "", `{}`)

		_, err := c.do(context.Background(), http.MethodGet, []string{"user", "current"}, nil, nil, &User{})
		require.NoError(t, err)

		assert.True(t, got.basicAuthOK)
		assert.Equal(t, "user", got.user)
		assert.Equal(t, "secret", got.pass)
	})

	t.Run("bearer", func(t *testing.T) {
		c, got := clientServer(t, "/rest/api", "", "pat", http.StatusOK, "", `{}`)

		_, err := c.do(context.Background(), http.MethodGet, []string{"user", "current"}, nil, nil, &User{})
		require.NoError(t, err)

		assert.False(t, got.basicAuthOK)
		assert.Equal(t, "Bearer pat", got.header.Get("Authorization"))
	})

	t.Run("none", func(t *testing.T) {
		c, got := clientServer(t, "/rest/api", "", "", http.StatusOK, "", `{}`)

		_, err := c.do(context.Background(), http.MethodGet, []string{"user", "current"}, nil, nil, &User{})
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
	response, err := c.do(ctx, http.MethodGet, []string{"content"}, nil, nil, &struct{}{})

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, response)
	assert.Less(t, time.Since(started), 5*time.Second)
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
		_, _ = io.WriteString(w, `{"id":"7"}`)
	}))
	t.Cleanup(server.Close)

	c := newClient(server.URL+"/rest/api", server.Client(), "user", "secret", "rest:")

	var page PageInfo
	_, err := c.do(context.Background(), http.MethodPut, []string{"content", "7"}, nil, map[string]any{"title": "T"}, &page)
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

	response, err := c.do(context.Background(), http.MethodGet, []string{"content"}, nil, nil, &struct{}{})
	require.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "invalid base URL")
}
