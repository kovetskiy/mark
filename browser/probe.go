package browser

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// probePath is the cheapest endpoint that says who the caller is, and it
// exists on Server, Data Center and Cloud alike.
//
// A 200 from it is not proof of a login. An instance with anonymous access
// answers an anonymous caller with 200 and a user whose type is "anonymous",
// and an SSO proxy may redirect an unauthenticated request to its login page,
// which the client follows to a 200 that is not JSON at all. Either way the
// browser already holds a cookie by then, so trusting the status alone would
// end the login on its very first poll, before the user has typed anything.
const probePath = "/rest/api/user/current"

// anonymousUserType is the type /rest/api/user/current reports for a caller
// that is not logged in, on an instance that lets such callers in.
const anonymousUserType = "anonymous"

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
	defer func() { _, _ = io.Copy(io.Discard, response.Body) }()

	if response.StatusCode != http.StatusOK {
		// A 401 is the expected answer while the user has not logged in yet
		// and is not worth a line each second. Anything else is a surprise
		// worth seeing with --log-level DEBUG when a login mysteriously times
		// out.
		if response.StatusCode != http.StatusUnauthorized {
			log.Debug().Msgf("session probe answered %d", response.StatusCode)
		}

		return false
	}

	var user struct {
		Type string `json:"type"`
	}

	if err := json.NewDecoder(response.Body).Decode(&user); err != nil {
		// Most likely an SSO proxy's login page, reached by following its
		// redirect: the user has not got through it yet.
		log.Debug().Err(err).Msg("session probe answered 200 with something other than a user")
		return false
	}

	// Silent for the same reason as a 401: on an instance with anonymous
	// access, this is the answer every poll gets until the user logs in.
	return user.Type != anonymousUserType
}
