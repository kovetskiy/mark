package confluence

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdUntilCancelled blocks a request the fake is handling until the client
// gives up on it, cancelling the caller's context as soon as it arrives.
func holdUntilCancelled(r *http.Request, cancel context.CancelFunc) {
	cancel()
	select {
	case <-r.Context().Done():
	case <-time.After(30 * time.Second):
	}
}

func TestContextDefaultsToBackground(t *testing.T) {
	api := &API{}
	assert.Equal(t, context.Background(), api.Context())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api.SetContext(ctx)
	assert.Equal(t, ctx, api.Context())
}

// TestCancellingTheContextAbortsARequestInFlight: the request a run is
// blocked on ends with the run's context, with an error errors.Is recognises,
// and nothing blames an SSO page for it.
func TestCancellingTheContextAbortsARequestInFlight(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	api := NewAPI(server.URL, "user", "token", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api.SetContext(ctx)

	server.SetFail(func(r *http.Request) (int, string, bool) {
		holdUntilCancelled(r, cancel)
		return http.StatusServiceUnavailable, `{}`, true
	})

	started := time.Now()
	_, err := api.FindPage("DOCS", "Anything", "page")

	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "not JSON")
	assert.Less(t, time.Since(started), 5*time.Second)
}

// TestCancellingTheContextCutsTheBackoffShort: a throttled request waits out
// a Retry-After only for as long as the run is still going.
func TestCancellingTheContextCutsTheBackoffShort(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	server.SetFailHeaders(http.Header{"Retry-After": {"30"}})
	api := NewAPI(server.URL, "user", "token", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api.SetContext(ctx)

	server.SetFail(func(*http.Request) (int, string, bool) {
		time.AfterFunc(50*time.Millisecond, cancel)
		return http.StatusTooManyRequests, `{}`, true
	})

	started := time.Now()
	_, err := api.FindPage("DOCS", "Anything", "page")

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(started), 5*time.Second)
	assert.Equal(t, 1, server.CountRequests(http.MethodGet, "/rest/api/content"), "no retry was sent")
}

// TestACancelledLookupIsNotRemembered: a lookup the caller stopped is no
// answer, so neither the space id nor the platform is cached from it, and an
// API reused under a live context asks again.
func TestACancelledLookupIsNotRemembered(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	api := NewAPI(server.URL, "user", "token", false)

	ctx, cancel := context.WithCancel(context.Background())
	api.SetContext(ctx)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		holdUntilCancelled(r, cancel)
		return http.StatusServiceUnavailable, `{}`, true
	})

	_, err := api.GetSpaceID("DOCS")
	require.ErrorIs(t, err, context.Canceled)
	_, err = api.cloud()
	require.ErrorIs(t, err, context.Canceled)

	server.SetFail(nil)
	api.SetContext(context.Background())

	id, err := api.GetSpaceID("DOCS")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	cloud, err := api.cloud()
	require.NoError(t, err)
	assert.True(t, cloud, "the fake's current user has an accountId")
}
