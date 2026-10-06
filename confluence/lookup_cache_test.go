package confluence_test

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyFront stands between the client and the fake, answering matching
// requests with status for as long as failing is set and passing everything
// else through.
//
// The fake's own failure hook cannot send headers, and without Retry-After the
// retry transport backs off for seconds per attempt before giving up. Zero
// makes it give up at once while still exhausting every attempt, which is the
// failure a lookup cache has to decide whether to remember.
func flakyFront(
	t *testing.T, server *confluencetest.Server, status int, matches func(*http.Request) bool,
) (*confluence.API, *atomic.Bool) {
	t.Helper()

	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)

	failing := &atomic.Bool{}
	failing.Store(true)

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() && matches(r) {
			w.Header().Set("Retry-After", "0")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"try again later"}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	return confluence.NewAPI(front.URL, "user", "token", false), failing
}

// TestGetUserByNameDoesNotCacheTransientFailures: the user lookup is memoised
// because a name resolves the same way all run, and that covers a name nobody
// has. It does not cover a throttled or failing search, which used to be
// remembered like an answer -- so one bad moment while rendering the first
// @mention of someone failed every later mention of them, in every file.
func TestGetUserByNameDoesNotCacheTransientFailures(t *testing.T) {
	for name, status := range map[string]int{
		"throttled":    http.StatusTooManyRequests,
		"server error": http.StatusServiceUnavailable,
	} {
		t.Run(name, func(t *testing.T) {
			server := confluencetest.New(t)
			server.AddUser(confluencetest.User{AccountID: "acct-1", FullName: "Jane Doe"})

			api, failing := flakyFront(t, server, status, func(r *http.Request) bool {
				return strings.HasPrefix(r.URL.Path, "/rest/api/search")
			})

			_, err := api.GetUserByName("Jane Doe")
			require.Error(t, err, "the outage is reported")

			failing.Store(false)

			user, err := api.GetUserByName("Jane Doe")
			require.NoError(t, err, "the next mention must not inherit the failure")
			assert.Equal(t, "acct-1", user.AccountID)
		})
	}
}

// TestGetSpaceIDNotFoundIsCached: a v2 listing that answers with no such space
// is a conclusion, and remembered like one. It used to be a bare string, which
// worthCaching did not recognise, so every folder-bearing file asked again.
func TestGetSpaceIDNotFoundIsCached(t *testing.T) {
	api, server := newAPI(t)
	server.SetFail(scopedTokenV1Gone(http.StatusNotFound))

	for range 5 {
		_, err := api.GetSpaceID("NOPE")
		require.Error(t, err)
		assert.ErrorIs(t, err, confluence.ErrNotFound)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/api/v2/spaces"))
}

// serverWithoutV2 fails v1's space lookup with v1Status and answers every v2
// path 404, as Server and Data Center do.
func serverWithoutV2(v1Status int) confluencetest.FailFunc {
	return func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2/") {
			return http.StatusNotFound, `{"message":"null for uri"}`, true
		}
		return scopedTokenV1Gone(v1Status)(r)
	}
}

// TestGetSpaceIDReportsV1ReasonOnServer: Server has no /api/v2, so the v2
// fallback always ends in a 404 there. Reporting that instead of v1's answer
// turned a Server user's 401 into a misleading "not found".
func TestGetSpaceIDReportsV1ReasonOnServer(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.SetFail(serverWithoutV2(http.StatusUnauthorized))

	_, err := api.GetSpaceID("DOCS")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.NotErrorIs(t, err, confluence.ErrNotFound,
		"a refusal is not a missing space")
}

// TestGetSpaceIDServerOutageIsNotCached: the same v2 404 used to make a v1
// outage on Server look like a missing space, which the cache then remembered
// for the rest of the run.
func TestGetSpaceIDServerOutageIsNotCached(t *testing.T) {
	server := confluencetest.New(t)
	space := server.AddSpace("DOCS")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/api/v2/") {
			return http.StatusNotFound, `{"message":"null for uri"}`, true
		}
		return 0, "", false
	})

	api, failing := flakyFront(t, server, http.StatusServiceUnavailable, func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, "/rest/api/space/")
	})

	_, err := api.GetSpaceID("DOCS")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")

	failing.Store(false)

	id, err := api.GetSpaceID("DOCS")
	require.NoError(t, err, "the next file must not inherit the outage")
	assert.Equal(t, space.ID, id)
}
