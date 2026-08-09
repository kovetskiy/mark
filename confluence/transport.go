package confluence

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// maxAttempts bounds the total number of tries for a single request,
	// including the first.
	maxAttempts = 4

	// maxBackoff caps a single wait, so a large Retry-After from a throttled
	// Confluence Cloud tenant cannot stall a run indefinitely.
	maxBackoff = 30 * time.Second

	// baseBackoff is the first delay; subsequent delays double it.
	baseBackoff = time.Second

	// dialTimeout, tlsHandshakeTimeout and responseHeaderTimeout bound the
	// phases of a request that can otherwise hang forever. Deliberately no
	// http.Client.Timeout: that caps the whole exchange including the request
	// body, which would break large attachment uploads on slow links.
	// responseHeaderTimeout is the one that matters for a server that accepts
	// the connection and then goes quiet.
	dialTimeout           = 30 * time.Second
	tlsHandshakeTimeout   = 15 * time.Second
	responseHeaderTimeout = 2 * time.Minute
	idleConnTimeout       = 90 * time.Second
)

// newHTTPClient builds the client used for every Confluence call.
//
// A cookie jar is always installed. Confluence Server hands out a session
// cookie on the first authenticated call and expects it back; previously the
// jar was only present in the default case, because the previous HTTP layer
// created one only when it was not given a client, so
// --insecure-skip-tls-verify silently dropped session affinity.
func newHTTPClient(insecureSkipVerify bool) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}

	if insecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{
			// Reached only when the user opts in with
			// --insecure-skip-tls-verify, for self-signed Confluence Server
			// instances.
			InsecureSkipVerify: true, //nolint:gosec // G402: explicitly requested by the operator
		}
	}

	// cookiejar.New never returns an error for a nil options value, but a jar
	// is not worth failing construction over if that ever changes.
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Warn().Err(err).Msg("unable to create cookie jar; session cookies will not be retained")
	}

	return &http.Client{
		Jar:       jar,
		Transport: &retryTransport{base: transport, sleep: time.Sleep},
	}
}

// retryTransport retries requests that Confluence rejected without acting on
// them. It sits below resty, whose own retry is left disabled, so this is the
// only place retries happen -- and unlike resty's default conditions it is
// method-aware, so a failed POST is never replayed.
type retryTransport struct {
	base  http.RoundTripper
	sleep func(time.Duration)
}

// retryableStatus reports whether a response status is worth retrying for the
// given method.
//
// 429 is retried for every method: Confluence rejects a throttled request
// before acting on it, so replaying it cannot duplicate work.
//
// 5xx is retried only for methods without side effects. A 5xx on POST is
// ambiguous -- the page may well have been created before the error -- and
// retrying it risks duplicate pages or attachments. A 5xx on PUT is equally
// ambiguous and would surface as a confusing 409 version conflict on the
// replay, so it is left alone too.
func retryableStatus(status int, method string) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if status < 500 || status > 599 {
		return false
	}
	// 501 is a permanent "not implemented"; replaying it never helps.
	if status == http.StatusNotImplemented {
		return false
	}
	return method == http.MethodGet || method == http.MethodHead
}

// retryableError reports whether a transport-level failure is worth retrying.
// Only side-effect-free methods qualify: a timeout partway through a POST may
// mean the server already committed the write.
func retryableError(err error, method string) bool {
	if err == nil {
		return false
	}
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	// A request cancelled by the caller's context must not be retried.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// parseRetryAfter interprets the Retry-After header, which Confluence Cloud
// sends with 429s either as a number of seconds or as an HTTP date.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := when.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// backoffFor returns the wait before the given attempt (1-based), preferring a
// server-supplied Retry-After and otherwise using exponential backoff with
// jitter to avoid synchronising retries across a batch of files.
func backoffFor(attempt int, retryAfter string, now time.Time) time.Duration {
	if d, ok := parseRetryAfter(retryAfter, now); ok {
		return min(d, maxBackoff)
	}

	backoff := baseBackoff << (attempt - 1)
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	// Full jitter over [backoff/2, backoff). Retry spacing is a scheduling
	// concern, not a security one, so a cheap PRNG is the right tool.
	half := backoff / 2
	return half + time.Duration(rand.Int64N(int64(half)+1)) //nolint:gosec // G404: jitter, not a secret
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A request whose body cannot be rewound must not be replayed. net/http
	// populates GetBody for the in-memory body types resty uses.
	canReplay := req.Body == nil || req.GetBody != nil

	var lastResp *http.Response
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			body, err := rewind(req)
			if err != nil {
				return nil, err
			}
			req = body
		}

		resp, err := t.base.RoundTrip(req)

		switch {
		case err != nil:
			lastResp, lastErr = nil, err
			if !canReplay || !retryableError(err, req.Method) || attempt == maxAttempts {
				return nil, err
			}
		case retryableStatus(resp.StatusCode, req.Method):
			lastResp, lastErr = resp, nil
			if !canReplay || attempt == maxAttempts {
				return resp, nil
			}
		default:
			return resp, nil
		}

		var retryAfter string
		if lastResp != nil {
			retryAfter = lastResp.Header.Get("Retry-After")
			// The body must be drained and closed or the connection leaks.
			drain(lastResp)
		}

		wait := backoffFor(attempt, retryAfter, time.Now())

		event := log.Warn().
			Str("method", req.Method).
			Str("url", req.URL.Redacted()).
			Int("attempt", attempt).
			Dur("retry_in", wait)
		if lastErr != nil {
			event = event.Err(lastErr)
		} else {
			event = event.Int("status", lastResp.StatusCode)
		}
		event.Msg("Confluence request failed, retrying")

		t.sleep(wait)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return lastResp, nil
}

// rewind returns a copy of req with a fresh body for a retry.
func rewind(req *http.Request) (*http.Request, error) {
	if req.Body == nil || req.GetBody == nil {
		return req, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Body = body
	return clone, nil
}

func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
}
