package confluence

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// unavailableError marks a write Confluence may or may not have carried out:
// it answered 502, 503 or 504, or the connection failed before any answer
// came. A gateway gives up waiting while Confluence goes on with the request,
// so neither says the write did not happen. retryAfter is the answer's
// Retry-After header, if it had one.
type unavailableError struct {
	error
	retryAfter string
}

func (e unavailableError) Unwrap() error { return e.error }

func isUnavailable(err error) bool {
	var unavailable unavailableError
	return errors.As(err, &unavailable)
}

// unavailableAnswer reports whether a response to method says a write may or
// may not have happened. A read is left unmarked: the transport has retried
// it already, and it changed nothing.
func unavailableAnswer(method string, status int) bool {
	if method == http.MethodGet || method == http.MethodHead {
		return false
	}
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// unavailableFailure reports whether err, from a request that got no answer,
// is a write that may have reached Confluence. net/http names the method in
// the url.Error it returns; a request that could not be built names "parse",
// and a payload that could not be encoded is no url.Error at all. A name that
// did not resolve, a refused connection and a certificate that was not
// trusted all stop a request before it is sent.
func unavailableFailure(err error) bool {
	var failed *url.Error
	if !errors.As(err, &failed) {
		return false
	}

	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &dns) || errors.As(err, &certificate) || errors.Is(err, syscall.ECONNREFUSED) {
		return false
	}

	switch failed.Op {
	case "Post", "Put", "Patch", "Delete":
		return true
	default:
		return false
	}
}

// retryUnavailable sends a write until it succeeds, fails another way, or has
// been tried maxAttempts times. The transport cannot replay a write the way it
// replays a read: a create sent twice makes two pages, or fails on the title
// the first one took. So after each unavailable answer, landed is asked
// whether the write took effect after all, and the write is sent again only
// when it says no. landed returns an error to stop retrying.
//
// A write sent again can also fail because the one before it landed after
// all, still in flight when landed was asked: a create on the title it took,
// an update on the version it made. landed is asked once more then too.
func retryUnavailable[T any](
	api *API,
	what string,
	send func() (T, error),
	landed func() (T, bool, error),
) (T, error) {
	wait := api.unavailableWait
	if wait == nil {
		wait = waitUnavailable
	}

	adopt := func(result T, err error) (T, error) {
		log.Warn().Err(err).Msgf("Confluence did %s, though it answered otherwise", what)
		return result, nil
	}

	for attempt := 1; ; attempt++ {
		result, err := send()
		if err == nil {
			return result, nil
		}

		var unavailable unavailableError
		if !errors.As(err, &unavailable) {
			if attempt > 1 {
				if landedResult, ok, landedErr := landed(); landedErr == nil && ok {
					return adopt(landedResult, err)
				}
			}
			return result, err
		}

		wait(attempt, unavailable.retryAfter)

		landedResult, ok, landedErr := landed()
		switch {
		case landedErr != nil:
			var zero T
			return zero, fmt.Errorf("unable to %s (%w): %w", what, err, landedErr)
		case ok:
			return adopt(landedResult, err)
		case attempt == maxAttempts:
			return result, err
		}

		log.Warn().Err(err).Int("attempt", attempt).Msgf("unable to %s, retrying", what)
	}
}

// waitUnavailable is the wait before reading whether an unavailable write
// landed: the transport's backoff, or as long as Retry-After asks, which gives
// a gateway that timed out time to let the request finish behind it.
func waitUnavailable(attempt int, retryAfter string) {
	time.Sleep(backoffFor(attempt, retryAfter, time.Now()))
}
