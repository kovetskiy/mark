package confluence

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// unavailableError marks a write Confluence may or may not have carried out:
// it answered 502, 503 or 504, or the connection failed before any answer
// came. A gateway gives up waiting while Confluence goes on with the request,
// so neither says the write did not happen.
type unavailableError struct{ error }

func (e unavailableError) Unwrap() error { return e.error }

func isUnavailable(err error) bool {
	var unavailable unavailableError
	return errors.As(err, &unavailable)
}

func unavailableStatus(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
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
func retryUnavailable[T any](
	api *API,
	what string,
	send func() (T, error),
	landed func() (T, bool, error),
) (T, error) {
	for attempt := 1; ; attempt++ {
		result, err := send()
		if err == nil || attempt == maxAttempts {
			return result, err
		}
		if !isUnavailable(err) {
			// A write sent again can fail because the one before it landed
			// after all, still in flight when landed was asked: a create on
			// the title it took, an update on the version it made.
			if attempt > 1 {
				if landedResult, ok, landedErr := landed(); landedErr == nil && ok {
					log.Warn().Err(err).Msgf("Confluence did %s, though it answered otherwise", what)
					return landedResult, nil
				}
			}
			return result, err
		}

		api.unavailableWait(attempt)

		result, ok, landedErr := landed()
		if landedErr != nil {
			var zero T
			return zero, fmt.Errorf(
				"%w; reading whether Confluence did %s anyway: %w", err, what, landedErr,
			)
		}
		if ok {
			log.Warn().Err(err).Msgf("Confluence did %s, though it answered otherwise", what)
			return result, nil
		}

		log.Warn().Err(err).Int("attempt", attempt).Msgf("unable to %s, retrying", what)
	}
}

// waitUnavailable is the wait before reading whether an unavailable write
// landed: the transport's backoff, which gives a gateway that timed out
// time to let the request finish behind it.
func waitUnavailable(attempt int) {
	time.Sleep(backoffFor(attempt, "", time.Now()))
}
