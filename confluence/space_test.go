package confluence_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindHomePageIsCached pins the N+1 this call used to be. It is made for
// every non-blogpost document, again for any page that turns out to have no
// ancestors, and once more when the manifest loads; on a scoped token each of
// those costs two round trips, because the v1 refusal is what sends it to v2.
func TestFindHomePageIsCached(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	for range 20 {
		page, err := api.FindHomePage("DOCS")
		require.NoError(t, err)
		require.NotNil(t, page)
		assert.Equal(t, home.ID, page.ID)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/space/DOCS"),
		"twenty files in one space should cost one space lookup")
}

// TestFindHomePageCachesFailures covers the case the cache can least afford to
// skip: a space that cannot be resolved cannot become resolvable mid-run, and
// re-asking pays both round trips again for every file in it.
func TestFindHomePageCachesFailures(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.SetFail(scopedTokenV1Gone(http.StatusNotFound))

	for range 5 {
		_, err := api.FindHomePage("NOPE")
		require.Error(t, err)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/space/NOPE"))
	assert.Equal(t, 1, server.CountRequests("GET", "/api/v2/spaces"))
}

// TestGetSpaceIDIsCached is the same property for the other space lookup, which
// is made once per folder-bearing file.
func TestGetSpaceIDIsCached(t *testing.T) {
	api, server := newAPI(t)
	space := server.AddSpace("DOCS")

	for range 20 {
		id, err := api.GetSpaceID("DOCS")
		require.NoError(t, err)
		assert.Equal(t, space.ID, id)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/space/DOCS"))
}

func TestGetSpaceIDCachesPerKey(t *testing.T) {
	api, server := newAPI(t)
	docs := server.AddSpace("DOCS")
	team := server.AddSpace("TEAM")

	for range 3 {
		id, err := api.GetSpaceID("DOCS")
		require.NoError(t, err)
		assert.Equal(t, docs.ID, id)

		id, err = api.GetSpaceID("TEAM")
		require.NoError(t, err)
		assert.Equal(t, team.ID, id)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/space/DOCS"))
	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/space/TEAM"))
}

// TestFindHomePageV1WithoutAHomepageIsAnError pins the class of bug where a 200
// is taken as an answer without checking that an answer came with it.
//
// v1 returns the space whether or not the homepage expansion produced anything,
// and the zero PageInfo that came back carried an empty id -- which callers
// then used as a content id, publishing under nothing at all. The v2 path has
// always distinguished "space has no homepage" from "space not found"; v1 has
// to say the same thing rather than falling through to a v2 that does not exist
// on Server or Data Center.
func TestFindHomePageV1WithoutAHomepageIsAnError(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")

	page, err := api.FindHomePage("DOCS")
	require.Error(t, err)
	assert.Nil(t, page)
	assert.Contains(t, err.Error(), "has no home page")
	assert.NotContains(t, err.Error(), "not found",
		"the space key is correct; sending people to hunt for a typo is the wrong answer")

	assert.Equal(t, 0, server.CountRequests("GET", "/api/v2/spaces"),
		"v1 answered, so there is nothing for v2 to add")
}

// TestTransientFailureIsNotCached: memoising the space lookups is correct
// because a space's home page and id cannot change mid-run. That premise covers
// a 404 and a space with no home page -- both conclusions -- and not a
// throttling burst or a gateway error, which are moments.
//
// The retry transport gives up after four attempts, so remembering one of those
// made a few unlucky seconds fail every remaining file in the space. Before the
// memoisation, the next document simply asked again and succeeded.
func TestTransientFailureIsNotCached(t *testing.T) {
	api, server := newAPI(t)
	space := server.AddSpace("DOCS")
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	failing := true
	server.SetFail(func(r *http.Request) (int, string, bool) {
		// Both the v1 lookup and the v2 fallback, or the fallback answers and
		// there is no failure to remember.
		if failing && strings.Contains(r.URL.Path, "space") {
			return http.StatusServiceUnavailable, "upstream is busy", true
		}
		return 0, "", false
	})

	_, err := api.FindHomePage("DOCS")
	require.Error(t, err, "the outage is reported")

	// The outage passes, as outages do.
	failing = false

	found, err := api.FindHomePage("DOCS")
	require.NoError(t, err, "the next document must not inherit the failure")
	require.NotNil(t, found)
	assert.Equal(t, home.ID, found.ID)

	id, err := api.GetSpaceID("DOCS")
	require.NoError(t, err)
	assert.Equal(t, space.ID, id)
}

// serverWithV1 stands in for Server or Data Center, where there is no /api/v2
// at all, with v1's space endpoint refusing with the status v1 gives while it
// says to fail.
func serverWithV1(v1 func() (int, bool)) func(r *http.Request) (int, string, bool) {
	return func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/api/v2/") {
			return http.StatusNotFound, `{"message":"null for uri"}`, true
		}
		if strings.HasPrefix(r.URL.Path, "/rest/api/space/") {
			if status, fail := v1(); fail {
				return status, `{"message":"v1 refused"}`, true
			}
		}
		return 0, "", false
	}
}

// TestFindHomePageServerOutageIsNotCachedAsMissing: on Server the v2 fallback
// always 404s, and that 404 used to be wrapped alongside v1's error. A v1
// outage then matched ErrNotFound, was cached as a missing space, and every
// later file in the space failed long after v1 had recovered.
func TestFindHomePageServerOutageIsNotCachedAsMissing(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	failing := true
	server.SetFail(serverWithV1(func() (int, bool) {
		return http.StatusServiceUnavailable, failing
	}))

	_, err := api.FindHomePage("DOCS")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503", "the outage is what is reported")
	assert.NotErrorIs(t, err, confluence.ErrNotFound,
		"v2's 404 on Server says nothing about the space")

	failing = false

	found, err := api.FindHomePage("DOCS")
	require.NoError(t, err, "the next lookup must not inherit the outage")
	require.NotNil(t, found)
	assert.Equal(t, home.ID, found.ID)
}

// TestFindHomePageServerReportsUnauthorized: bad credentials on Server are a
// 401 from v1, and are neither reported nor remembered as a missing space.
func TestFindHomePageServerReportsUnauthorized(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.SetFail(serverWithV1(func() (int, bool) {
		return http.StatusUnauthorized, true
	}))

	for range 2 {
		_, err := api.FindHomePage("DOCS")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "401 (Unauthorized)")
		assert.NotErrorIs(t, err, confluence.ErrNotFound)
	}

	assert.Equal(t, 2, server.CountRequests("GET", "/rest/api/space/DOCS"),
		"a refusal is not a conclusion about the space, so it is asked again")
}

// TestGatewayFindHomePageNotFoundIsCached: through the gateway v1 is never
// asked, so v2's 404 is the answer, and is remembered as one.
func TestGatewayFindHomePageNotFoundIsCached(t *testing.T) {
	api, server := newGatewayAPI(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/api/v2/spaces") {
			return http.StatusNotFound, `{"message":"no such space"}`, true
		}
		return 0, "", false
	})

	for range 2 {
		_, err := api.FindHomePage("NOPE")
		require.Error(t, err)
		assert.ErrorIs(t, err, confluence.ErrNotFound)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/api/v2/spaces"))
	assert.Zero(t, server.CountRequests("GET", "/rest/api/space/"))
}
