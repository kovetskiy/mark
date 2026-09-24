package confluence_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// promptly bounds how long a cancelled call may take to return. Every case
// below would otherwise take at least tens of seconds: a handler that never
// answers, or a Retry-After of thirty.
const promptly = 5 * time.Second

// TestCancelStopsARequestInFlight: a request to a server that has stopped
// answering used to run until the transport's two-minute header timeout,
// whatever the caller wanted. With the API's context cancelled it returns at
// once, and says why.
func TestCancelStopsARequestInFlight(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	api := confluence.NewAPI(server.URL, "user", "token", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api.SetContext(ctx)

	go func() {
		<-entered
		cancel()
	}()

	start := time.Now()
	_, err := api.GetPageByID("42")

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), promptly)
}

// TestCancelInterruptsTheRetryBackoff: retryTransport has always meant to stop
// waiting when the request is cancelled, but nothing gave a request a context
// that could be, so a throttled run sat out every backoff. Up to three waits at
// the thirty-second cap is a minute and a half.
func TestCancelInterruptsTheRetryBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		// Cancelled once the throttled answer is on its way, so it is the
		// wait before the retry that has to notice, not the request.
		time.AfterFunc(50*time.Millisecond, cancel)
	}))
	t.Cleanup(server.Close)

	api := confluence.NewAPI(server.URL, "user", "token", false)
	api.SetContext(ctx)

	start := time.Now()
	_, err := api.GetPageByID("42")

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), promptly, "the backoff was sat out")
	assert.Equal(t, int32(1), requests.Load(), "nothing was retried after the cancellation")
}

// TestCancelledRequestsAreNotSent: once the context is done, a request fails
// before it reaches the network.
func TestCancelledRequestsAreNotSent(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")

	api := confluence.NewAPI(server.URL, "user", "token", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api.SetContext(ctx)

	_, err := api.FindPage("DOCS", "Anything", "page")

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, server.Requests())
}

// TestCancelledLookupsAreNotCached: the space and user lookups remember what
// they are told, "not found" included, for the rest of the API's life. A
// cancelled lookup was told nothing, and remembering its error as the answer
// would fail the same lookup later -- in the manifest save a stopped run still
// makes on its way out, or in the next run of a library caller that reuses the
// API.
func TestCancelledLookupsAreNotCached(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddUser(confluencetest.User{AccountID: "acct-jane", Username: "jane", FullName: "Jane Doe"})

	api := confluence.NewAPI(server.URL, "user", "token", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api.SetContext(ctx)

	_, err := api.FindHomePage("DOCS")
	require.ErrorIs(t, err, context.Canceled)
	_, err = api.GetUserByName("Jane Doe")
	require.ErrorIs(t, err, context.Canceled)
	_, err = api.GetSpaceID("DOCS")
	require.ErrorIs(t, err, context.Canceled)

	api.SetContext(context.Background())

	found, err := api.FindHomePage("DOCS")
	require.NoError(t, err)
	assert.Equal(t, home.ID, found.ID)

	user, err := api.GetUserByName("Jane Doe")
	require.NoError(t, err)
	assert.Equal(t, "acct-jane", user.AccountID)

	id, err := api.GetSpaceID("DOCS")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

// TestCancelledCloudProbeIsNotRemembered: IsCloud answers once per API value.
// A probe cut short is not an answer, and remembering it as "not Cloud" would
// send everything after it down the Server path.
func TestCancelledCloudProbeIsNotRemembered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	t.Cleanup(server.Close)

	api := confluence.NewAPI(server.URL, "user", "token", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api.SetContext(ctx)

	assert.False(t, api.IsCloud(), "a cancelled probe learns nothing")

	api.SetContext(context.Background())
	assert.True(t, api.IsCloud(), "the next call probes again")
}

// TestContextDefaultsToBackground: an API nobody gave a context behaves as it
// always has.
func TestContextDefaultsToBackground(t *testing.T) {
	api := confluence.NewAPI("https://example.invalid", "user", "token", false)
	assert.Equal(t, context.Background(), api.Context())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api.SetContext(ctx)
	assert.Equal(t, ctx, api.Context())
}
