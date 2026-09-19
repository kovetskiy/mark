package browser

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// probePath is the cheapest endpoint that answers differently for an
// authenticated and an anonymous caller, and it exists on Server, Data Center
// and Cloud alike.
const probePath = "/rest/api/user/current"

// Authenticates reports whether cookies are enough to reach the Confluence API
// at baseURL.
//
// This is the only definition of "the login worked" in mark. Waiting for a
// cookie of a particular name -- JSESSIONID on Server, cloud.session.token on
// Cloud, something else again behind an SSO proxy -- would both miss
// deployments and accept cookies that authenticate nothing. Asking the API is
// provider-agnostic and tests the thing that actually matters.
func Authenticates(ctx context.Context, client *http.Client, baseURL string, cookies []*http.Cookie) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(baseURL, "/")+probePath, nil)
	if err != nil {
		log.Debug().Err(err).Msg("unable to build the session probe request")
		return false
	}

	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	// A shallow copy with no jar: this runs once a second while a login is in
	// progress, against the same *http.Client the rest of the run uses, whose
	// jar is always installed. Anonymous probes get a Set-Cookie of their own,
	// and http.Client.send appends whatever the jar holds to the Cookie header
	// this function already built -- so without this, a later probe could
	// carry a stale JSESSIONID alongside the fresh one, and the server is free
	// to honour the stale cookie. The cookies passed in are exactly what
	// should be sent; nothing should accumulate between polls.
	probeClient := *client
	probeClient.Jar = nil

	response, err := probeClient.Do(request)
	if err != nil {
		log.Debug().Err(err).Msg("session probe did not reach Confluence")
		return false
	}

	defer response.Body.Close()

	// Drained so the connection can be reused: this runs once a second while
	// a login is in progress.
	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode == http.StatusOK {
		return true
	}

	// A 401 is the expected answer while the user has not logged in yet and is
	// not worth a line each second. Anything else is a surprise worth seeing
	// with --log-level DEBUG when a login mysteriously times out.
	if response.StatusCode != http.StatusUnauthorized {
		log.Debug().Msgf("session probe answered %d", response.StatusCode)
	}

	return false
}
