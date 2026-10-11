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

// TestARequestCarriesTheContextItIsGiven: each request is made under the ctx
// passed to the method that makes it, and under nothing else. A cancelled one
// fails at once, without a request; the same API asked under a live one
// answers as though nothing had happened.
func TestARequestCarriesTheContextItIsGiven(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	stored := server.AddPage("DOCS", "Existing", "page", "")
	api := NewAPI(server.URL, "user", "token", false)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for name, call := range map[string]func(ctx context.Context) error{
		"GetPageByID": func(ctx context.Context) error {
			_, err := api.GetPageByID(ctx, stored.ID)
			return err
		},
		"UpdatePage": func(ctx context.Context) error {
			page, err := api.GetPageByID(context.Background(), stored.ID)
			require.NoError(t, err)
			return api.UpdatePage(ctx, page, "<p>new</p>", false, "", "", "")
		},
		"GetChildPages": func(ctx context.Context) error {
			_, err := api.GetChildPages(ctx, stored.ID)
			return err
		},
	} {
		before := len(server.Requests())
		err := call(cancelled)
		require.ErrorIs(t, err, context.Canceled, name)
		assert.True(t, IsCancellation(err), name)

		requests := server.Requests()[before:]
		for _, r := range requests {
			// UpdatePage's own read of the page goes out under Background.
			assert.NotEqual(t, http.MethodPut, r.Method, "%s: nothing was written under a cancelled context", name)
		}

		require.NoError(t, call(context.Background()), "%s: the API holds no context of its own", name)
	}
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

	server.SetFail(func(r *http.Request) (int, string, bool) {
		holdUntilCancelled(r, cancel)
		return http.StatusServiceUnavailable, `{}`, true
	})

	started := time.Now()
	_, err := api.FindPage(ctx, "DOCS", "Anything", "page")

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

	server.SetFail(func(*http.Request) (int, string, bool) {
		time.AfterFunc(50*time.Millisecond, cancel)
		return http.StatusTooManyRequests, `{}`, true
	})

	started := time.Now()
	_, err := api.FindPage(ctx, "DOCS", "Anything", "page")

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(started), 5*time.Second)
	assert.Equal(t, 1, server.CountRequests(http.MethodGet, "/rest/api/content"), "no retry was sent")
}

// TestACancelledLookupIsNotRemembered: a lookup the caller stopped is no
// answer, so neither the space id nor the platform is cached from it, and the
// same API asked under a live context asks again.
func TestACancelledLookupIsNotRemembered(t *testing.T) {
	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	api := NewAPI(server.URL, "user", "token", false)

	ctx, cancel := context.WithCancel(context.Background())
	server.SetFail(func(r *http.Request) (int, string, bool) {
		holdUntilCancelled(r, cancel)
		return http.StatusServiceUnavailable, `{}`, true
	})

	_, err := api.GetSpaceID(ctx, "DOCS")
	require.ErrorIs(t, err, context.Canceled)
	_, err = api.Cloud(ctx)
	require.ErrorIs(t, err, context.Canceled)

	server.SetFail(nil)

	id, err := api.GetSpaceID(context.Background(), "DOCS")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	cloud, err := api.Cloud(context.Background())
	require.NoError(t, err)
	assert.True(t, cloud, "the fake's current user has an accountId")
}
