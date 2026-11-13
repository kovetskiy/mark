package confluence

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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

// newHTTPClient builds the client used for every Confluence call: base, when
// it is given, with mark's retries wrapped around its transport, or else a
// client of mark's own.
//
// A cookie jar is always installed, --insecure-skip-tls-verify or not.
// Confluence Server hands out a session cookie on the first authenticated
// call and expects it back.
func newHTTPClient(insecureSkipVerify bool, base *http.Client) *http.Client {
	if base != nil {
		// A copy, so that the caller's client goes on working as it did.
		client := *base
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		client.Transport = &retryTransport{base: transport}
		if client.Jar == nil {
			client.Jar = newCookieJar()
		}
		client.CheckRedirect = checkRedirect(client.CheckRedirect)

		return &client
	}

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

	return &http.Client{
		Jar:           newCookieJar(),
		Transport:     &retryTransport{base: transport},
		CheckRedirect: checkRedirect(nil),
	}
}

// maxRedirects is how many redirects one request follows, as many as
// net/http's own policy allows.
const maxRedirects = 10

// checkRedirect is the redirect policy of the client: next, or net/http's
// own limit when that is nil, after the Authorization header has been taken
// off a request that a redirect sends to another server than the one it was
// made to.
//
// net/http keeps the header for any port and scheme of the same host name,
// but the basic auth or token is the configured server's, as the same host
// and port are what SameInstance and attachment links are held to. It is
// kept from http to https on the same host and the default ports, which is
// how a proxy moves a whole site to TLS. Once off it stays off, as it does
// in net/http, for a chain that comes back.
//
// The warning is for the redirect that leaves the server, when it is
// followed: net/http puts the header back on every later hop, and those are
// not news.
func checkRedirect(next func(req *http.Request, via []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		leaves := false
		if len(via) > 0 && req.Header.Get("Authorization") != "" {
			hops := append(via[1:len(via):len(via)], req)
			for _, hop := range hops {
				if keepsAuthorization(via[0].URL, hop.URL) {
					continue
				}

				req.Header.Del("Authorization")
				leaves = hop == req

				break
			}
		}

		var err error
		switch {
		case next != nil:
			err = next(req, via)
		case len(via) >= maxRedirects:
			err = fmt.Errorf("stopped after %d redirects", maxRedirects)
		}

		if leaves && err == nil {
			log.Warn().
				Str("requested", serverOf(via[0].URL)).
				Str("redirected_to", serverOf(req.URL)).
				Msg("a redirect led to another server than the one requested, " +
					"so the request goes on without its Authorization header; " +
					"if Confluence is served there, use that address in the base URL")
		}

		return err
	}
}

// keepsAuthorization says whether a request first made to requested carries
// its Authorization header on to target: the same scheme, host and port, or
// https on the same host when both are on their default ports.
//
// A user name in the base URL is in requested, and a relative Location
// carries it on to target; it is whoever configured the base URL's, so it
// makes neither another server. One a Location puts there itself does.
func keepsAuthorization(requested, target *url.URL) bool {
	if target.User.String() == requested.User.String() {
		target = withoutUser(target)
	}

	from, errA := originOf(withoutUser(requested))
	to, errB := originOf(target)
	if errA != nil || errB != nil {
		return false
	}

	if from.scheme == "http" && from.port == 80 && to.scheme == "https" && to.port == 443 {
		return from.host == to.host
	}

	return from == to
}

// withoutUser is u without the user name and password in it.
func withoutUser(u *url.URL) *url.URL {
	if u.User == nil {
		return u
	}

	bare := *u
	bare.User = nil

	return &bare
}

// serverOf names the server u reaches, for a message: its scheme, host and
// port, without the path and query of the one request.
func serverOf(u *url.URL) string {
	return u.Scheme + "://" + u.Host
}

// newCookieJar returns a jar for Confluence Server's session cookie, or nil
// if one cannot be made.
func newCookieJar() http.CookieJar {
	// cookiejar.New never returns an error for a nil options value, but a jar
	// is not worth failing construction over if that ever changes.
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Warn().Err(err).Msg("unable to create cookie jar; session cookies will not be retained")
		return nil
	}

	return jar
}

// retryTransport retries requests that Confluence rejected without acting on
// them. It sits below client.do, which does not retry on its own, so this is
// the only place retries happen.
type retryTransport struct {
	base http.RoundTripper

	// sleep stands in for the wait between attempts; see sleepContext. It is
	// nil outside tests.
	sleep func(time.Duration)
}

// backoffContextKey is the key under which client.do hands a write's caller
// context to the retry transport. The write itself goes out on a context that
// cannot be cancelled, so that what Confluence did with it is not lost; the
// wait before sending it again does no work on Confluence, and is what a
// stopped run must not be held up by.
type backoffContextKey struct{}

// backoffContext is the context the wait before retrying req is cut short by:
// the caller's, for a write sent on a context of its own, and otherwise req's.
func backoffContext(req *http.Request) context.Context {
	if caller, ok := req.Context().Value(backoffContextKey{}).(context.Context); ok {
		return caller
	}

	return req.Context()
}

// sleepContext sits out d, and returns ctx's error instead if ctx ends first.
//
// sleep, when it is not nil, does the sitting out in place of a timer, and
// ctx is asked once it is done. It is what the retry transport's tests
// replace, so that a test can make the wait instant, or end the context
// during it.
func sleepContext(ctx context.Context, d time.Duration, sleep func(time.Duration)) error {
	if sleep != nil {
		sleep(d)

		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
// replay, so it is left alone too. retryUnavailable retries the writes that
// can be checked for having landed.
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
	return readOnly(method)
}

// readOnly reports whether a request with method has no side effects on
// Confluence: whether it can be replayed, or abandoned part way through,
// without leaving behind something done that the run does not know of.
func readOnly(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// retryableError reports whether a transport-level failure of req is worth
// retrying. Only side-effect-free methods qualify: a timeout partway through a
// POST may mean the server already committed the write.
//
// Nor is a timeout retried, a read's included. The response header and dial
// timeouts are what stop a run against a server that accepts the connection
// and goes quiet, or one that cannot be reached at all; each attempt waits the
// whole timeout out, so retrying them would make a hung Confluence cost every
// read four times as long -- about eight minutes rather than two -- before it
// failed. What is retried is a failure that comes at once: a connection reset
// or closed under the request.
func retryableError(err error, req *http.Request) bool {
	if err == nil {
		return false
	}
	if !readOnly(req.Method) {
		return false
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// A request whose context is done was given up on by its caller, and
	// must not be retried, whatever error it ended with.
	return req.Context().Err() == nil
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
	if d, ok := parseRetryAfter(retryAfter, now); ok && d > 0 {
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
	// populates GetBody for the in-memory body client.do sends.
	canReplay := req.Body == nil || req.GetBody != nil

	for attempt := 1; ; attempt++ {
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
			if !canReplay || !retryableError(err, req) || attempt == maxAttempts {
				return nil, err
			}
		case retryableStatus(resp.StatusCode, req.Method):
			if !canReplay || attempt == maxAttempts {
				return resp, nil
			}
		default:
			return resp, nil
		}

		var retryAfter string
		if err == nil {
			retryAfter = resp.Header.Get("Retry-After")
			// The body must be drained and closed or the connection leaks.
			drain(resp)
		}

		wait := backoffFor(attempt, retryAfter, time.Now())

		event := log.Warn().
			Str("method", req.Method).
			Str("url", req.URL.Redacted()).
			Int("attempt", attempt).
			Dur("retry_in", wait)
		if err != nil {
			event = event.Err(err)
		} else {
			event = event.Int("status", resp.StatusCode)
		}
		event.Msg("Confluence request failed, retrying")

		// The wait respects the caller's context, so a cancelled run stops
		// waiting instead of finishing its backoff first. Three retries at the
		// cap is a minute and a half a caller could not otherwise escape -- a
		// write's included, though the write itself is not cut short.
		if err := sleepContext(backoffContext(req), wait, t.sleep); err != nil {
			return nil, err
		}
	}
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
