package confluence_test

import (
	"net/http"
	"testing"

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
