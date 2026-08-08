package confluence

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryableStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		method string
		want   bool
	}{
		{"429 on GET", http.StatusTooManyRequests, http.MethodGet, true},
		// The case that mattered: a throttled write is rejected before
		// Confluence acts on it, so replaying cannot duplicate a page.
		{"429 on POST", http.StatusTooManyRequests, http.MethodPost, true},
		{"429 on PUT", http.StatusTooManyRequests, http.MethodPut, true},

		{"503 on GET", http.StatusServiceUnavailable, http.MethodGet, true},
		{"500 on GET", http.StatusInternalServerError, http.MethodGet, true},
		{"502 on HEAD", http.StatusBadGateway, http.MethodHead, true},

		// A 5xx on a write is ambiguous: the page may already exist.
		{"503 on POST", http.StatusServiceUnavailable, http.MethodPost, false},
		{"500 on PUT", http.StatusInternalServerError, http.MethodPut, false},
		{"501 on GET", http.StatusNotImplemented, http.MethodGet, false},

		{"200", http.StatusOK, http.MethodGet, false},
		{"404", http.StatusNotFound, http.MethodGet, false},
		{"401", http.StatusUnauthorized, http.MethodGet, false},
		{"409", http.StatusConflict, http.MethodPut, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, retryableStatus(tt.status, tt.method))
		})
	}
}

func TestRetryableError(t *testing.T) {
	someErr := errors.New("connection reset")

	assert.True(t, retryableError(someErr, http.MethodGet))
	assert.True(t, retryableError(someErr, http.MethodHead))
	// A transport error partway through a write may still have been committed.
	assert.False(t, retryableError(someErr, http.MethodPost))
	assert.False(t, retryableError(someErr, http.MethodPut))
	assert.False(t, retryableError(nil, http.MethodGet))
}

func TestParseRetryAfterSeconds(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	d, ok := parseRetryAfter("30", now)
	require.True(t, ok)
	assert.Equal(t, 30*time.Second, d)

	d, ok = parseRetryAfter("0", now)
	require.True(t, ok)
	assert.Equal(t, time.Duration(0), d)

	_, ok = parseRetryAfter("", now)
	assert.False(t, ok)

	_, ok = parseRetryAfter("not-a-number", now)
	assert.False(t, ok)

	_, ok = parseRetryAfter("-5", now)
	assert.False(t, ok, "a negative delay is malformed and should be ignored")
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	d, ok := parseRetryAfter("Sat, 08 Aug 2026 12:00:45 GMT", now)
	require.True(t, ok)
	assert.Equal(t, 45*time.Second, d)

	// A date already in the past means "retry now", not a negative wait.
	d, ok = parseRetryAfter("Sat, 08 Aug 2026 11:59:00 GMT", now)
	require.True(t, ok)
	assert.Equal(t, time.Duration(0), d)
}

func TestBackoffForHonoursRetryAfter(t *testing.T) {
	now := time.Now()
	assert.Equal(t, 5*time.Second, backoffFor(1, "5", now))
	// Even a server asking for a very long wait is capped.
	assert.Equal(t, maxBackoff, backoffFor(1, "3600", now))
}

func TestBackoffForGrowsAndIsJittered(t *testing.T) {
	now := time.Now()

	// Without Retry-After the delay is exponential with jitter in
	// [backoff/2, backoff]. Assert on the bounds rather than an exact value.
	for attempt, want := range map[int]time.Duration{
		1: baseBackoff,
		2: 2 * baseBackoff,
		3: 4 * baseBackoff,
	} {
		for range 50 {
			got := backoffFor(attempt, "", now)
			assert.GreaterOrEqual(t, got, want/2)
			assert.LessOrEqual(t, got, want)
		}
	}

	// Late attempts are capped.
	assert.LessOrEqual(t, backoffFor(20, "", now), maxBackoff)
}

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newTestTransport returns a retryTransport whose sleeps are recorded instead
// of performed, so retry tests run instantly.
func newTestTransport(t *testing.T, fn roundTripperFunc) (*retryTransport, *[]time.Duration) {
	t.Helper()
	var slept []time.Duration
	return &retryTransport{
		base:  fn,
		sleep: func(d time.Duration) { slept = append(slept, d) },
	}, &slept
}

func respond(status int, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       http.NoBody,
	}
}

func TestRoundTripRetriesUntilSuccess(t *testing.T) {
	var calls int
	transport, slept := newTestTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return respond(http.StatusServiceUnavailable, nil), nil
		}
		return respond(http.StatusOK, nil), nil
	})

	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/rest/api/content", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 3, calls)
	assert.Len(t, *slept, 2, "two failures means two waits")
}

func TestRoundTripGivesUpAfterMaxAttempts(t *testing.T) {
	var calls int
	transport, _ := newTestTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		return respond(http.StatusServiceUnavailable, nil), nil
	})

	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/rest/api/content", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
		"the final response is returned rather than swallowed")
	assert.Equal(t, maxAttempts, calls)
}

func TestRoundTripDoesNotRetryPostOn5xx(t *testing.T) {
	var calls int
	transport, _ := newTestTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		return respond(http.StatusServiceUnavailable, nil), nil
	})

	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/rest/api/content", http.NoBody)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, 1, calls, "a 5xx on POST must not be replayed")
}

func TestRoundTripRetriesPostOn429(t *testing.T) {
	var calls int
	transport, _ := newTestTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return respond(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"0"}}), nil
		}
		return respond(http.StatusOK, nil), nil
	})

	// A *bytes.Buffer body is what gopencils sends, and it is what makes
	// net/http populate GetBody so the request can be replayed.
	req, err := http.NewRequest(
		http.MethodPost,
		"https://example.invalid/rest/api/content",
		bytes.NewBufferString(`{"title":"x"}`),
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 2, calls)
}

// TestRoundTripReplaysRequestBody proves the retried attempt carries the body
// again rather than sending an empty one.
func TestRoundTripReplaysRequestBody(t *testing.T) {
	var bodies []string
	var calls int
	transport, _ := newTestTransport(t, func(r *http.Request) (*http.Response, error) {
		calls++
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		bodies = append(bodies, string(b))
		if calls == 1 {
			return respond(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"0"}}), nil
		}
		return respond(http.StatusOK, nil), nil
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"https://example.invalid/rest/api/content",
		bytes.NewBufferString(`{"title":"x"}`),
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, []string{`{"title":"x"}`, `{"title":"x"}`}, bodies)
}

// TestRoundTripDoesNotReplayUnrewindableBody covers the safety valve: a body
// that cannot be rewound is sent once and the response returned as-is, rather
// than replayed with an empty body.
func TestRoundTripDoesNotReplayUnrewindableBody(t *testing.T) {
	var calls int
	transport, _ := newTestTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		return respond(http.StatusTooManyRequests, nil), nil
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"https://example.invalid/rest/api/content",
		io.NopCloser(bytes.NewBufferString("stream")),
	)
	require.NoError(t, err)
	require.Nil(t, req.GetBody, "an opaque reader gives net/http nothing to rewind")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, 1, calls)
}

func TestRoundTripUsesRetryAfterForWait(t *testing.T) {
	var calls int
	transport, slept := newTestTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return respond(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"7"}}), nil
		}
		return respond(http.StatusOK, nil), nil
	})

	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/rest/api/content", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Len(t, *slept, 1)
	assert.Equal(t, 7*time.Second, (*slept)[0], "the server's Retry-After wins over backoff")
}

func TestRoundTripRetriesTransportErrorOnGet(t *testing.T) {
	var calls int
	transport, _ := newTestTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		if calls < 2 {
			return nil, errors.New("connection refused")
		}
		return respond(http.StatusOK, nil), nil
	})

	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/rest/api/content", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 2, calls)
}

func TestRoundTripDoesNotRetryTransportErrorOnPost(t *testing.T) {
	var calls int
	transport, _ := newTestTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("connection reset by peer")
	})

	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/rest/api/content", http.NoBody)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req) //nolint:bodyclose // the base always errors, so there is no response
	require.Error(t, err)
	assert.Equal(t, 1, calls,
		"the write may already have been committed, so it must not be replayed")
}

// TestNewHTTPClientHasCookieJar guards the regression that made
// --insecure-skip-tls-verify behave differently from the default: gopencils
// only creates a jar when it is not handed a client.
func TestNewHTTPClientHasCookieJar(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		client := newHTTPClient(insecure)
		assert.NotNil(t, client.Jar, "insecure=%v should still retain session cookies", insecure)
	}
}

func TestNewHTTPClientSetsTimeouts(t *testing.T) {
	client := newHTTPClient(false)

	retry, ok := client.Transport.(*retryTransport)
	require.True(t, ok)
	transport, ok := retry.base.(*http.Transport)
	require.True(t, ok)

	assert.Equal(t, responseHeaderTimeout, transport.ResponseHeaderTimeout)
	assert.Equal(t, tlsHandshakeTimeout, transport.TLSHandshakeTimeout)
	assert.Zero(t, client.Timeout,
		"a whole-exchange timeout would cut off large attachment uploads")
}
