package confluence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"resty.dev/v3"
)

// newSilentServerAPI returns an API aimed at a server that accepts every
// request and never answers it, with a response header timeout short enough
// to reach and no wait between retries, and the number of requests the
// server has seen.
func newSilentServerAPI(t *testing.T) (*API, *atomic.Int32) {
	t.Helper()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	api := NewAPI(server.URL, "user", "token", false)

	retry, ok := api.rest.Client().Transport.(*retryTransport)
	require.True(t, ok)
	retry.sleep = func(time.Duration) {}

	transport, ok := retry.base.(*http.Transport)
	require.True(t, ok)
	transport.ResponseHeaderTimeout = 50 * time.Millisecond

	return api, &requests
}

// TestTimeoutIsNotACancellation: net/http's "timeout awaiting response
// headers" satisfies errors.Is(err, context.DeadlineExceeded), as a dial's
// "i/o timeout" does, and neither is the run being stopped. Taking one for
// a cancellation made IsCloud probe again on every call -- each probe waiting
// out the full header timeout while holding the mutex every other caller
// queues on -- instead of remembering the answer it got.
func TestTimeoutIsNotACancellation(t *testing.T) {
	api, requests := newSilentServerAPI(t)

	_, err := api.v2().Get("spaces")
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded,
		"the premise: a network timeout looks like a context deadline to errors.Is")
	assert.NoError(t, api.Context().Err())

	requests.Store(0)

	assert.False(t, api.IsCloud(), "a probe nothing answered is not Cloud")
	probed := requests.Load()
	require.Positive(t, probed)

	assert.False(t, api.IsCloud())
	assert.Equal(t, probed, requests.Load(), "the probe's answer is remembered")
}

// TestTransportErrorKeepsItsDiagnosisOnATimeout: a response that arrived and
// then failed with a timeout is still explained -- where it came from, what
// status it had, and what usually causes it -- since the run was not stopped.
// A request whose own context ended is the one left unexplained.
func TestTransportErrorKeepsItsDiagnosisOnATimeout(t *testing.T) {
	response := func(ctx context.Context) *resty.Response {
		request := resty.New().R().SetContext(ctx)
		raw, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/rest/api/content/1", nil)
		require.NoError(t, err)

		return &resty.Response{
			Request:     request,
			RawResponse: &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Request: raw},
		}
	}

	timeout := errors.Join(errors.New("reading the body"), context.DeadlineExceeded)

	err := newTransportError(response(context.Background()), "read content 1", timeout)
	assert.Contains(t, err.Error(), "SSO login page")
	assert.Contains(t, err.Error(), "https://example.invalid/rest/api/content/1")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = newTransportError(response(ctx), "read content 1", context.Canceled)
	assert.NotContains(t, err.Error(), "SSO login page")
	assert.ErrorIs(t, err, context.Canceled)
}
