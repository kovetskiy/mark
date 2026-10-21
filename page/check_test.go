package page

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

// TestCheckExternalStopsWithTheRun: a link check carries the run's context.
// It used not to, so a stopped run first waited out every external link left
// on the page, HEAD and then GET, each up to the client's timeout. Nor is a
// check that ended with the run remembered as the link's answer.
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
