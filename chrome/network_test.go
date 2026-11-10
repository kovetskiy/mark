package chrome

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/kovetskiy/mark/v17/internal/lazy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withoutLocalNetworkChecks turns off Chrome's own refusal, in some releases,
// of a request from a page to a server on the same machine. The recorder has
// to listen on the machine running the test, and what is tested is that mark's
// options keep the browser from every host, not only local ones. The features
// chromedp turns off are repeated, since a flag given twice is replaced.
var withoutLocalNetworkChecks = chromedp.Flag("disable-features",
	"Translate,BlinkGenPropertyTrees,LocalNetworkAccessChecks")

// requestRecorder is an HTTP server that counts what reaches it.
type requestRecorder struct {
	*httptest.Server

	mu   sync.Mutex
	seen []string
}

func newRequestRecorder(t *testing.T) *requestRecorder {
	t.Helper()

	recorder := &requestRecorder{}
	recorder.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.mu.Lock()
		recorder.seen = append(recorder.seen, r.Host+r.URL.Path)
		recorder.mu.Unlock()

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(recorder.Close)

	return recorder
}

func (r *requestRecorder) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.seen...)
}

// urls names the recorder by its IP literal and by localhost, which are the
// two ways a document can reach a server on the machine rendering it without
// asking a resolver for anything else.
func (r *requestRecorder) urls(t *testing.T, path string) []string {
	t.Helper()

	_, port, err := net.SplitHostPort(r.Listener.Addr().String())
	require.NoError(t, err)

	return []string{
		fmt.Sprintf("http://127.0.0.1:%s/%s", port, path),
		fmt.Sprintf("http://localhost:%s/%s", port, path),
	}
}

// TestRenderLoadsNothingFromTheNetwork: a picture is drawn from the document
// alone, so an image, a stylesheet or a script the drawing names is not
// fetched, whether the address is a name or an IP literal, and the drawing is
// still captured.
func TestRenderLoadsNothingFromTheNetwork(t *testing.T) {
	recorder := newRequestRecorder(t)

	var svg strings.Builder

	svg.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">`)

	for _, url := range recorder.urls(t, "image") {
		fmt.Fprintf(&svg, `<image width="10" height="10" href="%s"/>`, url)
	}

	for _, url := range recorder.urls(t, "css") {
		fmt.Fprintf(&svg, `<rect width="10" height="10" style="fill:url(%s)"/>`, url)
	}

	svg.WriteString(`<rect x="20" y="20" width="20" height="20" fill="red"/></svg>`)

	browser := &Browser{session: lazy.New(func(life context.Context) (*session, error) {
		return startSessionWith(life, withoutLocalNetworkChecks)
	}, func(s *session) { s.cancel() }, errClosed)}
	t.Cleanup(browser.Close)

	png, _, _, err := browser.PNGFromSVG(t.Context(), []byte(svg.String()), "document.querySelector(\"svg\")", 1)
	require.NoError(t, err)
	assert.NotEmpty(t, png)

	assert.Empty(t, recorder.requests(), "the browser made a request while drawing")
}

// TestPageLoadsNothingFromTheNetwork: the same for an HTML page, as mermaid's
// is, where a script can ask for things an SVG image cannot.
func TestPageLoadsNothingFromTheNetwork(t *testing.T) {
	recorder := newRequestRecorder(t)

	var page strings.Builder

	page.WriteString(`<!DOCTYPE html><html><body>`)

	urls := recorder.urls(t, "page")
	for _, url := range urls {
		fmt.Fprintf(&page, `<img src="%s"><link rel="stylesheet" href="%s.css">`, url, url)
	}

	page.WriteString(`<script>window.done = Promise.allSettled([`)

	for _, url := range urls {
		fmt.Fprintf(&page, `fetch(%q), new Promise(r => { const w = new WebSocket(%q); w.onerror = w.onopen = r; }),`,
			url+"-fetch", strings.Replace(url, "http:", "ws:", 1)+"-ws")
	}

	page.WriteString(`]).then(() => "settled");</script></body></html>`)

	s, err := startSessionWith(t.Context(), withoutLocalNetworkChecks)
	require.NoError(t, err)
	t.Cleanup(s.cancel)

	ctx := s.ctx

	require.NoError(t, chromedp.Do(ctx,
		chromedp.Navigate("data:text/html,"+strings.ReplaceAll(page.String(), "#", "%23")),
	))

	settled, err := chromedp.Run(ctx, chromedp.Evaluate[string]("window.done", chromedp.EvalAwaitPromise))
	require.NoError(t, err)
	assert.Equal(t, "settled", settled)

	assert.Empty(t, recorder.requests(), "the browser made a request while drawing")
}
