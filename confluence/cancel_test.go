package confluence_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// holdUntilCancelled stops the run from inside a request and keeps that request
// open until the client gives up on it, so the call is cut short in flight. It
// gives up itself after promptly, for a client that never would.
func holdUntilCancelled(r *http.Request, cancel context.CancelFunc) {
	cancel()

	select {
	case <-r.Context().Done():
	case <-time.After(promptly):
	}
}

// TestCancelDuringTheV2FallbackIsReported: FindHomePage and GetSpaceID fall
// back to v2 when v1 refuses, and report v1's refusal when v2 fails too. A run
// stopped during the fallback is not v2 failing: wrapped behind v1's 403, the
// cancellation was out of errors.Is's reach.
func TestCancelDuringTheV2FallbackIsReported(t *testing.T) {
	for name, lookup := range map[string]func(*confluence.API) error{
		"FindHomePage": func(api *confluence.API) error { _, err := api.FindHomePage("DOCS"); return err },
		"GetSpaceID":   func(api *confluence.API) error { _, err := api.GetSpaceID("DOCS"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			api, server := newAPI(t)
			server.AddSpace("DOCS")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			api.SetContext(ctx)

			server.SetFail(func(r *http.Request) (int, string, bool) {
				if strings.HasPrefix(r.URL.Path, "/api/v2/") {
					holdUntilCancelled(r, cancel)
					return http.StatusInternalServerError, `{"message":"too late"}`, true
				}
				if strings.Contains(r.URL.Path, "/space/") {
					return http.StatusForbidden, `{"message":"scope"}`, true
				}
				return 0, "", false
			})

			require.ErrorIs(t, lookup(api), context.Canceled)
		})
	}
}

// TestCancelStopsTheMoveAction: movepage.action was the one request that did
// not carry the API's context, so a stopped run sat out a blocked action until
// the transport gave up on it.
func TestCancelStopsTheMoveAction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	api, server := newDataCenterAPIWith(t, func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/pages/movepage.action" {
			holdUntilCancelled(r, cancel)
			return http.StatusInternalServerError, `{"message":"too late"}`, true
		}
		return 0, "", false
	})
	api.SetContext(ctx)

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)

	start := time.Now()
	err := api.MoveContentBefore(second.ID, first.ID)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), promptly)
}

// TestCancelledMoveActionIsNotRemembered: an answer from movepage.action that
// the run's cancellation cut short is no answer. Read as one, a login page's
// headers on it closed the action to every later move on the API, cancellation
// or not, and the error no longer said the run had been stopped.
func TestCancelledMoveActionIsNotRemembered(t *testing.T) {
	fake := confluencetest.New(t)
	fake.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2") || strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		return 0, "", false
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cutShort atomic.Bool
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pages/movepage.action" && cutShort.CompareAndSwap(false, true) {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("X-Seraph-Loginreason", "AUTHENTICATED_FAILED")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Log"))
			w.(http.Flusher).Flush()
			// Long enough for the client to have the status and headers in
			// hand, so it is the body that the cancellation cuts short.
			time.Sleep(200 * time.Millisecond)
			holdUntilCancelled(r, cancel)
			return
		}
		fake.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	api := confluence.NewAPI(front.URL, "user", "token", false)
	require.False(t, api.IsCloud())
	api.SetContext(ctx)

	parent := fake.AddPage("DOCS", "Parent", "page", "")
	first := fake.AddPage("DOCS", "First", "page", parent.ID)
	second := fake.AddPage("DOCS", "Second", "page", parent.ID)

	require.ErrorIs(t, api.MoveContentBefore(second.ID, first.ID), context.Canceled)

	api.SetContext(context.Background())
	require.NoError(t, api.MoveContentBefore(second.ID, first.ID))
	assert.Equal(t, []string{second.ID, first.ID}, fake.ChildOrder(parent.ID))
	assert.Equal(t, 1, fake.CountRequests(http.MethodPost, "/pages/movepage.action"),
		"the action has to be asked again once the run is no longer stopped")
}

// TestCancelledCloudProbeDoesNotCloseTheMoveEndpoint: a move endpoint's 404 is
// taken as the endpoint being missing only outside Cloud, and IsCloud answers
// false for a probe the run's cancellation cut short. That false was taken for
// an answer, and every later move on the API skipped the endpoint for the
// Data Center fallbacks.
func TestCancelledCloudProbeDoesNotCloseTheMoveEndpoint(t *testing.T) {
	api, server := newAPI(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api.SetContext(ctx)

	var probed atomic.Bool
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") {
			return http.StatusNotFound, `{"message":"no such page"}`, true
		}
		if strings.HasPrefix(r.URL.Path, "/api/v2/") && probed.CompareAndSwap(false, true) {
			holdUntilCancelled(r, cancel)
			return http.StatusInternalServerError, `{"message":"too late"}`, true
		}
		return 0, "", false
	})

	parent := server.AddPage("DOCS", "Parent", "page", "")
	first := server.AddPage("DOCS", "First", "page", parent.ID)
	second := server.AddPage("DOCS", "Second", "page", parent.ID)

	require.ErrorIs(t, api.MoveContentBefore(second.ID, first.ID), context.Canceled)

	api.SetContext(context.Background())
	_ = api.MoveContentBefore(second.ID, first.ID)
	assert.Equal(t, 2, server.CountRequests(http.MethodPut, "/move/"),
		"the endpoint has to be asked again once the run is no longer stopped")
}
