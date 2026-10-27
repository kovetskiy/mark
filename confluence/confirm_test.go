package confluence

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rewriteWrites puts a proxy in front of the fake that answers each PUT with
// a 200 HTML page, as some proxies and older releases do: having passed it on
// to the fake first when forward is set, or in place of it otherwise.
func rewriteWrites(t *testing.T, fake *confluencetest.Server, forward bool) string {
	t.Helper()

	target, err := url.Parse(fake.URL)
	require.NoError(t, err)

	proxy := httputil.NewSingleHostReverseProxy(target)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			proxy.ServeHTTP(w, r)
			return
		}

		if forward {
			recorder := httptest.NewRecorder()
			proxy.ServeHTTP(recorder, r)
			if recorder.Code != http.StatusOK {
				w.WriteHeader(recorder.Code)
				_, _ = io.Copy(w, recorder.Body)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body>OK</body></html>")
	}))
	t.Cleanup(server.Close)

	return server.URL
}

// TestUpdatePageTakesAnUpdateAppliedBehindAnHTMLAnswer: an update Confluence
// applied, answered with HTML by something in front of it, is an update made:
// the page is read back at the version it created. Failing it would leave
// that version unrecorded, and --no-overwrite would take the page for one
// edited in Confluence on every later run.
func TestUpdatePageTakesAnUpdateAppliedBehindAnHTMLAnswer(t *testing.T) {
	fake := confluencetest.New(t)
	fake.AddSpace("DOCS")
	stored := fake.AddPage("DOCS", "Doc", "page", "")
	api := NewAPI(rewriteWrites(t, fake, true), "user", "token", false)

	page, err := api.GetPageByID(context.Background(), stored.ID)
	require.NoError(t, err)

	require.NoError(t, api.UpdatePage(context.Background(), page, "<p>two</p>", false, "", "", ""))
	assert.Equal(t, int64(2), page.Version.Number)
	assert.Equal(t, int64(2), fake.Page(stored.ID).Version)
}

// TestUpdatePageRefusesAnHTMLAnswerToAnUpdateNotApplied: the same answer to
// an update that never reached Confluence -- an SSO proxy's login form, told
// by nothing but its HTML -- is still the failure it was.
func TestUpdatePageRefusesAnHTMLAnswerToAnUpdateNotApplied(t *testing.T) {
	fake := confluencetest.New(t)
	fake.AddSpace("DOCS")
	stored := fake.AddPage("DOCS", "Doc", "page", "")
	api := NewAPI(rewriteWrites(t, fake, false), "user", "token", false)

	page, err := api.GetPageByID(context.Background(), stored.ID)
	require.NoError(t, err)

	err = api.UpdatePage(context.Background(), page, "<p>two</p>", false, "", "", "")
	require.ErrorIs(t, err, ErrLoginPage)
	assert.Equal(t, int64(1), page.Version.Number)
	assert.Equal(t, int64(1), fake.Page(stored.ID).Version)
}

// TestConfirmVersionLeavesARefusalAlone: only an answer that is not
// Confluence's is checked against the page. A status, a cancellation and a
// login page with more to go on than its HTML are what they say.
func TestConfirmVersionLeavesARefusalAlone(t *testing.T) {
	fake := confluencetest.New(t)
	fake.AddSpace("DOCS")
	stored := fake.AddPage("DOCS", "Doc", "page", "")
	api := NewAPI(fake.URL, "user", "token", false)
	page := &PageInfo{ID: stored.ID, Type: "page"}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for name, err := range map[string]error{
		"status":       &StatusError{StatusCode: http.StatusBadRequest, Status: "400 Bad Request"},
		"cancellation": cancellation(cancelled, context.Canceled),
		"login page":   &loginPageError{requested: "x", evidence: "redirected to /login.action", strong: true},
	} {
		fake.ResetRequests()
		// The fake's page is at version 1: a check would find it there.
		assert.Same(t, err, api.confirmVersion(context.Background(), page, 1, err), name)
		assert.Empty(t, fake.Requests(), "%s: the page is not read", name)
	}

	body := &bodyError{answer: &reply{Status: "200 OK"}, err: io.ErrUnexpectedEOF}
	require.NoError(t, api.confirmVersion(context.Background(), page, 1, body))
	assert.Same(t, error(body), api.confirmVersion(context.Background(), page, 2, body),
		"a page not at the version the write created was not written")
}
