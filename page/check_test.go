package page

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLinkChecks(t *testing.T) {
	all := LinkChecks{Internal: true, Confluence: true, External: true}

	for name, tt := range map[string]struct {
		values   []string
		expected LinkChecks
	}{
		"nothing":            {nil, LinkChecks{}},
		"empty string":       {[]string{""}, LinkChecks{}},
		"one":                {[]string{"internal"}, LinkChecks{Internal: true}},
		"comma separated":    {[]string{"internal,confluence"}, LinkChecks{Internal: true, Confluence: true}},
		"repeated flag":      {[]string{"internal", "confluence"}, LinkChecks{Internal: true, Confluence: true}},
		"spaced and cased":   {[]string{" Internal , CONFLUENCE "}, LinkChecks{Internal: true, Confluence: true}},
		"all":                {[]string{"all"}, all},
		"all beside another": {[]string{"external", "all"}, all},
		"repeats are fine":   {[]string{"internal", "internal"}, LinkChecks{Internal: true}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParseLinkChecks(tt.values)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}

	t.Run("an unknown value is rejected", func(t *testing.T) {
		_, err := ParseLinkChecks([]string{"internal", "sideways"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sideways")
		assert.Contains(t, err.Error(), "confluence")
	})
}

func TestLinkChecksAny(t *testing.T) {
	assert.False(t, LinkChecks{}.Any())
	assert.True(t, LinkChecks{Internal: true}.Any())
	assert.True(t, LinkChecks{Confluence: true}.Any())
	assert.True(t, LinkChecks{External: true}.Any())
}

// TestLinkCheckerAsksOnce pins the cache. A URL on twenty pages should cost one
// request, not twenty.
func TestLinkCheckerAsksOnce(t *testing.T) {
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewLinkChecker(LinkChecks{External: true})
	for range 5 {
		assert.NoError(t, checker.CheckExternal(context.Background(), server.URL+"/page"))
	}

	assert.Equal(t, int64(1), atomic.LoadInt64(&requests))
}

// unreachableFinder fails the test that asks it for a page.
type unreachableFinder struct{ t *testing.T }

func (f unreachableFinder) FindPage(context.Context, string, string, string) (*confluence.PageInfo, error) {
	f.t.Error("a forgotten page was looked up")
	return nil, nil
}

// TestForgetPagesKeepsExternalAnswers: forgetting the ac: links noted for one
// file leaves what external URLs answered, so the next file does not ask again.
func TestForgetPagesKeepsExternalAnswers(t *testing.T) {
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewLinkChecker(LinkChecks{Confluence: true, External: true})
	require.NoError(t, checker.CheckExternal(context.Background(), server.URL))
	checker.NotePage("DOCS", "Missing", "a.md")

	checker.ForgetPages()

	missing, err := checker.MissingPages(context.Background(), unreachableFinder{t})
	require.NoError(t, err)
	assert.Empty(t, missing)

	require.NoError(t, checker.CheckExternal(context.Background(), server.URL))
	assert.Equal(t, int64(1), atomic.LoadInt64(&requests))
}

// TestLinkCheckerFallsBackToGet covers hosts that refuse HEAD while serving the
// same URL perfectly well, which is common enough to matter.
func TestLinkCheckerFallsBackToGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	assert.NoError(t, NewLinkChecker(LinkChecks{External: true}).CheckExternal(context.Background(), server.URL+"/page"))
}

func TestLinkCheckerReportsFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := NewLinkChecker(LinkChecks{External: true}).CheckExternal(context.Background(), server.URL+"/gone")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")

	// Nothing listening at all.
	err = NewLinkChecker(LinkChecks{External: true}).CheckExternal(context.Background(), "http://127.0.0.1:1/nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unreachable")
}

// TestLinkCheckerOffAsksNothing: only "external" may make network requests.
func TestLinkCheckerOffAsksNothing(t *testing.T) {
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
	}))
	defer server.Close()

	// Asking for internal or Confluence checks must not put mark on the
	// network; that is the point of the set being a set.
	for _, checks := range []LinkChecks{{}, {Internal: true}, {Confluence: true}, {Internal: true, Confluence: true}} {
		assert.NoError(t, NewLinkChecker(checks).CheckExternal(context.Background(), server.URL))
	}
	assert.Nil(t, (*LinkChecker)(nil).CheckExternal(context.Background(), server.URL))

	assert.Zero(t, atomic.LoadInt64(&requests))
}

// TestCheckExternalStopsWithTheRun: a link check carries the run's context,
// so a stopped run does not first wait out every external link left on the
// page, HEAD and then GET, each up to the client's timeout. Nor is a check
// that ended with the run remembered as the link's answer.
func TestCheckExternalStopsWithTheRun(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	checker := NewLinkChecker(LinkChecks{External: true})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	started := time.Now()
	err := checker.CheckExternal(ctx, server.URL+"/slow")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 5*time.Second)

	checker.mu.Lock()
	_, remembered := checker.seen[server.URL+"/slow"]
	checker.mu.Unlock()
	assert.False(t, remembered, "a check the run gave up on is not the link's answer")
}

// answerThenCancel answers every request 200 and then cancels the run, which
// is a check that completed just as the run was stopped.
type answerThenCancel struct{ cancel context.CancelFunc }

func (a answerThenCancel) RoundTrip(r *http.Request) (*http.Response, error) {
	a.cancel()

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       http.NoBody,
		Request:    r,
	}, nil
}

// TestCheckExternalKeepsAnAnswerThatCameAsTheRunStopped: a check that got its
// answer has it, and is remembered, however soon after it the run stopped.
func TestCheckExternalKeepsAnAnswerThatCameAsTheRunStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checker := NewLinkChecker(LinkChecks{External: true})
	checker.Client = &http.Client{Transport: answerThenCancel{cancel: cancel}}

	require.NoError(t, checker.CheckExternal(ctx, "https://example.test/page"))

	checker.mu.Lock()
	answer, remembered := checker.seen["https://example.test/page"]
	checker.mu.Unlock()
	assert.True(t, remembered)
	assert.NoError(t, answer)
}

// answerGetThenCancel refuses a HEAD with 405 and answers the GET after it
// with status, cancelling the run as it does.
type answerGetThenCancel struct {
	cancel context.CancelFunc
	status int
}

func (a answerGetThenCancel) RoundTrip(r *http.Request) (*http.Response, error) {
	status := http.StatusMethodNotAllowed
	if r.Method == http.MethodGet {
		a.cancel()
		status = a.status
	}

	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       http.NoBody,
		Request:    r,
	}, nil
}

// TestCheckExternalKeepsAFailureThatCameAsTheRunStopped: a link that answered
// 404 answered, and that is the check's result even when the run stopped
// before it was read -- not the run's cancellation in its place.
func TestCheckExternalKeepsAFailureThatCameAsTheRunStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checker := NewLinkChecker(LinkChecks{External: true})
	checker.Client = &http.Client{Transport: answerGetThenCancel{cancel: cancel, status: http.StatusNotFound}}

	err := checker.CheckExternal(ctx, "https://example.test/gone")
	require.Error(t, err)
	assert.NotErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "Not Found")

	checker.mu.Lock()
	_, remembered := checker.seen["https://example.test/gone"]
	checker.mu.Unlock()
	assert.True(t, remembered)
}

// missingFinder knows no page at all, and may be asked from several goroutines.
type missingFinder struct{}

func (missingFinder) FindPage(context.Context, string, string, string) (*confluence.PageInfo, error) {
	return nil, nil
}

// TestLinkCheckerConcurrentAccess races NotePage, MissingPages and ForgetPages
// on one checker; run under -race, it fails if any of them touches the pending
// links outside the lock.
func TestLinkCheckerConcurrentAccess(t *testing.T) {
	const (
		goroutines = 8
		perRoutine = 100
	)

	checker := NewLinkChecker(LinkChecks{Confluence: true})

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(2)
		go func(g int) {
			defer wg.Done()
			for i := range perRoutine {
				checker.NotePage("DOCS", "page-"+strconv.Itoa(g)+"-"+strconv.Itoa(i), "a.md")
				checker.NotePage("DOCS", "shared", "b.md")
				if i%25 == 0 {
					checker.ForgetPages()
				}
			}
		}(g)
		go func() {
			defer wg.Done()
			for range perRoutine / 10 {
				_, err := checker.MissingPages(context.Background(), missingFinder{})
				assert.NoError(t, err)
			}
		}()
	}
	wg.Wait()

	checker.ForgetPages()
	checker.NotePage("DOCS", "b", "b.md")
	checker.NotePage("DOCS", "a", "a.md")
	missing, err := checker.MissingPages(context.Background(), missingFinder{})
	require.NoError(t, err)
	assert.Equal(t, []string{
		`a.md: link "ac:a" does not resolve: there is no page "a" in space "DOCS"`,
		`b.md: link "ac:b" does not resolve: there is no page "b" in space "DOCS"`,
	}, missing)
}
