package confluence_test

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAPI(t *testing.T) (*confluence.API, *confluencetest.Server) {
	t.Helper()
	server := confluencetest.New(t)
	return confluence.NewAPI(server.URL, "user", "token", false), server
}

func TestFindPage(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Getting Started", "page", "")

	page, err := api.FindPage("DOCS", "Getting Started", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "Getting Started", page.Title)
	assert.Equal(t, "page", page.Type)
}

func TestFindPageNotFoundReturnsNilWithoutError(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")

	page, err := api.FindPage("DOCS", "Nope", "page")
	require.NoError(t, err)
	assert.Nil(t, page)
}

// TestFindPageIsCachedAcrossCalls pins the behaviour the page cache exists for:
// a repeated lookup must not produce a second HTTP request.
func TestFindPageIsCachedAcrossCalls(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Cached", "page", "")

	for range 3 {
		page, err := api.FindPage("DOCS", "Cached", "page")
		require.NoError(t, err)
		require.NotNil(t, page)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/content"),
		"repeated FindPage calls should be served from the cache")
}

// TestFindPageNegativeResultIsCached covers the miss path: a page that does not
// exist must also be remembered, or every ancestry walk re-queries it.
func TestFindPageNegativeResultIsCached(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")

	for range 3 {
		page, err := api.FindPage("DOCS", "Missing", "page")
		require.NoError(t, err)
		require.Nil(t, page)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/content"))
}

func TestFindPageReturnsAncestors(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	mid := server.AddPage("DOCS", "Mid", "page", root.ID)
	server.AddPage("DOCS", "Leaf", "page", mid.ID)

	page, err := api.FindPage("DOCS", "Leaf", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	require.Len(t, page.Ancestors, 2)
	// Confluence returns ancestors root-first.
	assert.Equal(t, "Root", page.Ancestors[0].Title)
	assert.Equal(t, "Mid", page.Ancestors[1].Title)
}

func TestCreatePage(t *testing.T) {
	api, server := newAPI(t)
	parent := server.AddPage("DOCS", "Parent", "page", "")

	parentInfo, err := api.FindPage("DOCS", "Parent", "page")
	require.NoError(t, err)

	created, err := api.CreatePage("DOCS", "page", parentInfo, "Child", "<p>hi</p>")
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "Child", created.Title)

	stored := server.Page(created.ID)
	require.NotNil(t, stored)
	assert.Equal(t, parent.ID, stored.ParentID)
	assert.Equal(t, "<p>hi</p>", stored.Body)
}

func TestCreatePageDuplicateTitleIsAnError(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Taken", "page", "")

	_, err := api.CreatePage("DOCS", "page", nil, "Taken", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestUpdatePageIncrementsVersion(t *testing.T) {
	api, server := newAPI(t)
	stored := server.AddPage("DOCS", "Target", "page", "")

	page, err := api.FindPage("DOCS", "Target", "page")
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Version.Number)

	err = api.UpdatePage(page, "<p>new</p>", false, "msg", "", "")
	require.NoError(t, err)

	after := server.Page(stored.ID)
	require.NotNil(t, after)
	assert.EqualValues(t, 2, after.Version)
	assert.Equal(t, "msg", after.Message)
	assert.Equal(t, "<p>new</p>", after.Body)
}

// TestUpdatePageKeepsVersionInSync pins the write-back at the end of
// UpdatePage: the caller's PageInfo is advanced to the version just published,
// so consecutive updates through the same value keep working.
func TestUpdatePageKeepsVersionInSync(t *testing.T) {
	api, server := newAPI(t)
	stored := server.AddPage("DOCS", "Target", "page", "")

	page, err := api.FindPage("DOCS", "Target", "page")
	require.NoError(t, err)

	require.NoError(t, api.UpdatePage(page, "<p>first</p>", false, "", "", ""))
	assert.EqualValues(t, 2, page.Version.Number)

	require.NoError(t, api.UpdatePage(page, "<p>second</p>", false, "", "", ""))
	assert.EqualValues(t, 3, page.Version.Number)

	assert.EqualValues(t, 3, server.Page(stored.ID).Version)
}

// TestUpdatePageStaleVersionConflicts covers the case the write-back cannot
// help with: two independently fetched copies of the same page. The second
// update carries a superseded version and Confluence rejects it, which is the
// 409 that issue #139 works around after page creation.
func TestUpdatePageStaleVersionConflicts(t *testing.T) {
	api, server := newAPI(t)
	stored := server.AddPage("DOCS", "Target", "page", "")

	first, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)
	second, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)

	require.NoError(t, api.UpdatePage(first, "<p>first</p>", false, "", "", ""))

	err = api.UpdatePage(second, "<p>second</p>", false, "", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "409")
}

func TestGetPageByID(t *testing.T) {
	api, server := newAPI(t)
	stored := server.AddPage("DOCS", "ByID", "page", "")

	page, err := api.GetPageByID(stored.ID)
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "ByID", page.Title)
}

func TestGetPageByIDMissing(t *testing.T) {
	api, _ := newAPI(t)

	_, err := api.GetPageByID("does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// TestGetAttachmentsPaginates walks past the client's 100-item page size, which
// the previous test suite could not reach at all.
func TestGetAttachmentsPaginates(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Attachments", "page", "")

	const total = 250
	for i := range total {
		server.AddAttachment(page.ID, "file-"+strconv.Itoa(i)+".png", "mark:checksum: "+strconv.Itoa(i))
	}

	attachments, err := api.GetAttachments(page.ID)
	require.NoError(t, err)
	assert.Len(t, attachments, total)
	assert.Equal(t, 3, server.CountRequests("GET", "/child/attachment"),
		"250 attachments at a page size of 100 should take three requests")
}

func TestGetPageLabelsPaginates(t *testing.T) {
	api, server := newAPI(t)
	stored := server.AddPage("DOCS", "Labelled", "page", "")

	page, err := api.FindPage("DOCS", "Labelled", "page")
	require.NoError(t, err)

	labels := make([]string, 0, 120)
	for i := range 120 {
		labels = append(labels, "label-"+strconv.Itoa(i))
	}
	_, err = api.AddPageLabels(page, labels)
	require.NoError(t, err)

	info, err := api.GetPageLabels(page, "global")
	require.NoError(t, err)
	assert.Len(t, info.Labels, 120)

	after := server.Page(stored.ID)
	require.NotNil(t, after)
	assert.Len(t, after.Labels, 120)
}

func TestDeletePageLabel(t *testing.T) {
	api, server := newAPI(t)
	stored := server.AddPage("DOCS", "Labelled", "page", "")

	page, err := api.FindPage("DOCS", "Labelled", "page")
	require.NoError(t, err)

	_, err = api.AddPageLabels(page, []string{"keep", "drop"})
	require.NoError(t, err)

	_, err = api.DeletePageLabel(page, "drop")
	require.NoError(t, err)

	after := server.Page(stored.ID)
	require.NotNil(t, after)
	assert.Equal(t, []string{"keep"}, after.Labels)
}

func TestGetInlineCommentsPaginates(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Commented", "page", "")

	const total = 150
	for i := range total {
		server.AddComment(page.ID, confluencetest.InlineComment{
			Location:  "inline",
			MarkerRef: "ref-" + strconv.Itoa(i),
			Selection: "selection " + strconv.Itoa(i),
		})
	}

	comments, err := api.GetInlineComments(page.ID)
	require.NoError(t, err)
	require.NotNil(t, comments)
	assert.Len(t, comments.Results, total)
	assert.Equal(t, 2, server.CountRequests("GET", "/child/comment"))
}

func TestFindHomePage(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	page, err := api.FindHomePage("DOCS")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "Home", page.Title)
}

func TestFindRootPage(t *testing.T) {
	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")
	server.AddPage("DOCS", "Leaf", "page", root.ID)

	page, err := api.FindRootPage("DOCS")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "Root", page.Title)
}

func TestGetUserByName(t *testing.T) {
	api, server := newAPI(t)
	server.AddUser(confluencetest.User{
		AccountID: "acct-1",
		Username:  "jdoe",
		FullName:  "Jane Doe",
	})

	user, err := api.GetUserByName("Jane Doe")
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, "acct-1", user.AccountID)
}

func TestGetUserByNameMissing(t *testing.T) {
	api, _ := newAPI(t)

	_, err := api.GetUserByName("Nobody At All")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetCurrentUser(t *testing.T) {
	api, _ := newAPI(t)

	user, err := api.GetCurrentUser()
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, "current", user.Username)
}

func TestCreateAttachment(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Attach", "page", "")

	info, err := api.CreateAttachment(
		page.ID,
		"diagram.png",
		"mark:checksum: abc",
		bytes.NewReader([]byte("not really a png")),
	)
	require.NoError(t, err)
	assert.Equal(t, "diagram.png", info.Filename)

	stored := server.Attachments(page.ID)
	require.Len(t, stored, 1)
	assert.Equal(t, "mark:checksum: abc", stored[0].Comment)
}

// TestGetSpaceID documents current behaviour, which is not the intended
// behaviour. GetSpaceID says it tries v1 "first (more reliable for space lookup
// by key)" and falls back to v2, but its v1 struct decodes `id` as a string
// while Confluence v1 returns a JSON number. The decode fails, gopencils
// returns the error, and the v1 branch is skipped on every call -- so the
// fallback is in fact the only path that ever succeeds.
//
// The assertion below is deliberately written against what happens today. When
// the v1 decode is fixed, this test should start failing and be updated to
// assert that no v2 request is made.
func TestGetSpaceID(t *testing.T) {
	api, server := newAPI(t)
	space := server.AddSpace("DOCS")

	id, err := api.GetSpaceID("DOCS")
	require.NoError(t, err)
	assert.Equal(t, space.ID, id)

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/space/DOCS"),
		"the v1 lookup is attempted")
	assert.Equal(t, 1, server.CountRequests("GET", "/api/v2/spaces"),
		"but its response cannot be decoded, so v2 is always used")
}

// TestRetriesThroughTheRealClientStack drives a retry end to end: real
// confluence.API, real gopencils, real HTTP, with the fake returning 503 for
// the first two GETs. Before the retry transport existed this returned an
// error on the first 503, because gopencils only retried transport-level
// failures and a 503 arrives with a nil error.
func TestRetriesThroughTheRealClientStack(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Flaky", "page", "")

	var seen int
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/content") {
			return 0, "", false
		}
		seen++
		if seen <= 2 {
			return http.StatusServiceUnavailable, `{"message":"overloaded"}`, true
		}
		return 0, "", false
	})

	page, err := api.FindPage("DOCS", "Flaky", "page")
	require.NoError(t, err, "two 503s should be retried, not surfaced")
	require.NotNil(t, page)
	assert.Equal(t, "Flaky", page.Title)
	assert.Equal(t, 3, server.CountRequests("GET", "/rest/api/content"))
}

// TestRateLimitIsRetried covers the 429 path, which is what a throttled
// Confluence Cloud tenant actually returns.
func TestRateLimitIsRetried(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Throttled", "page", "")

	var seen int
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/content") {
			return 0, "", false
		}
		seen++
		if seen == 1 {
			return http.StatusTooManyRequests, `{"message":"rate limited"}`, true
		}
		return 0, "", false
	})

	page, err := api.FindPage("DOCS", "Throttled", "page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, 2, server.CountRequests("GET", "/rest/api/content"))
}

// TestPersistent5xxStillFails confirms retries are bounded and the eventual
// error still reaches the caller.
func TestPersistent5xxStillFails(t *testing.T) {
	api, server := newAPI(t)
	server.AddPage("DOCS", "Broken", "page", "")

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/content") {
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})

	_, err := api.FindPage("DOCS", "Broken", "page")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
	assert.Equal(t, 4, server.CountRequests("GET", "/rest/api/content"),
		"bounded at maxAttempts")
}

// TestCreatePageIsNotRetriedOn5xx is the safety property: replaying a failed
// create could leave two pages behind.
func TestCreatePageIsNotRetriedOn5xx(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPost {
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})

	_, err := api.CreatePage("DOCS", "page", nil, "Once Only", "")
	require.Error(t, err)
	assert.Equal(t, 1, server.CountRequests("POST", "/rest/api/content"),
		"a create must be attempted exactly once")
}

// TestGetUserByNameIsCached pins the fix for the @mention N+1: every mention in
// a document renders through the "user" stdlib template func, so an uncached
// lookup meant one CQL search per occurrence.
func TestGetUserByNameIsCached(t *testing.T) {
	api, server := newAPI(t)
	server.AddUser(confluencetest.User{
		AccountID: "acct-1",
		Username:  "jdoe",
		FullName:  "Jane Doe",
	})

	for range 20 {
		user, err := api.GetUserByName("Jane Doe")
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, "acct-1", user.AccountID)
	}

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/search"),
		"twenty mentions of the same person should cost one search")
}

// TestGetUserByNameCachesMisses covers the case the cache can least afford to
// skip: an unknown name renders nothing, so nothing stops the document from
// asking again for every occurrence.
func TestGetUserByNameCachesMisses(t *testing.T) {
	api, server := newAPI(t)

	for range 5 {
		_, err := api.GetUserByName("Nobody At All")
		require.Error(t, err)
	}

	// A miss costs two requests, not one: GetUserByName tries /search/user and
	// then falls back to the legacy /search path when that yields no results.
	// The point of the assertion is that five lookups cost the same as one.
	assert.Equal(t, 2, server.CountRequests("GET", "/rest/api/search"),
		"a failed lookup should be remembered too")
}

func TestGetUserByNameCachesPerName(t *testing.T) {
	api, server := newAPI(t)
	server.AddUser(confluencetest.User{AccountID: "a", FullName: "Ann"})
	server.AddUser(confluencetest.User{AccountID: "b", FullName: "Bob"})

	for range 3 {
		ann, err := api.GetUserByName("Ann")
		require.NoError(t, err)
		assert.Equal(t, "a", ann.AccountID)

		bob, err := api.GetUserByName("Bob")
		require.NoError(t, err)
		assert.Equal(t, "b", bob.AccountID)
	}

	assert.Equal(t, 2, server.CountRequests("GET", "/rest/api/search"),
		"one search per distinct name")
}

// TestIsCloudConcurrentCallsProbeOnce covers the memoisation and its safety
// together: many goroutines calling IsCloud must agree on the answer, and the
// Cloud-only probe must be issued at most once. Before sync.Once the flag was
// read and written without synchronisation, so concurrent callers raced on it
// and each could issue its own probe.
func TestIsCloudConcurrentCallsProbeOnce(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")

	const goroutines = 24

	results := make([]bool, goroutines)
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release them together to maximise contention
			results[i] = api.IsCloud()
		}(i)
	}
	close(start)
	wg.Wait()

	for i, got := range results {
		assert.Equal(t, results[0], got, "goroutine %d disagreed about IsCloud", i)
	}

	assert.LessOrEqual(t, server.CountRequests("GET", "/api/v2/spaces"), 1,
		"the Cloud probe should be issued at most once regardless of caller count")
}

// TestIsCloudCachesAcrossSequentialCalls is the plain-path counterpart: the
// answer is memoised, so a second call does not re-probe.
func TestIsCloudCachesAcrossSequentialCalls(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")

	first := api.IsCloud()
	before := server.CountRequests("GET", "/api/v2/spaces")

	for range 5 {
		assert.Equal(t, first, api.IsCloud())
	}

	assert.Equal(t, before, server.CountRequests("GET", "/api/v2/spaces"),
		"repeat calls must not re-probe")
}

// scopedTokenV1Gone makes the v1 space endpoint answer as it does for an
// Atlassian scoped API token: those tokens lack the classic scopes, so v1
// reports the space as absent even though it exists and v2 can see it.
func scopedTokenV1Gone(status int) confluencetest.FailFunc {
	return func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/rest/api/space/") {
			return status, `{"message":"no permission"}`, true
		}
		return 0, "", false
	}
}

// TestFindHomePageFallsBackToV2 is the case from issue #341: with a scoped
// token the v1 space endpoint 404s and mark aborted with "can't obtain home
// page from space" before ever reaching a working v2 endpoint.
func TestFindHomePageFallsBackToV2(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.SetFail(scopedTokenV1Gone(http.StatusNotFound))

	page, err := api.FindHomePage("DOCS")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "Home", page.Title)
	assert.Equal(t, home.ID, page.ID)

	assert.Equal(t, 1, server.CountRequests("GET", "/rest/api/space/DOCS"),
		"v1 is still tried first")
	assert.Equal(t, 1, server.CountRequests("GET", "/api/v2/spaces"),
		"and v2 resolves the space once v1 refuses")
}

// TestFindHomePageFallsBackOnAnyNonOK covers the widening the fallback is
// deliberately written for: a token with partial scopes draws 401/403 from v1
// rather than 404, and those must fall through too.
func TestFindHomePageFallsBackOnAnyNonOK(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			api, server := newAPI(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			server.SetFail(scopedTokenV1Gone(status))

			page, err := api.FindHomePage("DOCS")
			require.NoError(t, err)
			require.NotNil(t, page)
			assert.Equal(t, "Home", page.Title)
		})
	}
}

// TestFindHomePagePrefersV1 pins that the fallback is a fallback: a classic
// token still gets its answer from v1, with no extra v2 round trip.
func TestFindHomePagePrefersV1(t *testing.T) {
	api, server := newAPI(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	page, err := api.FindHomePage("DOCS")
	require.NoError(t, err)
	assert.Equal(t, "Home", page.Title)

	assert.Equal(t, 0, server.CountRequests("GET", "/api/v2/spaces"),
		"v1 answered, so v2 must not be consulted")
}

// TestFindHomePageMissingSpace: when neither API knows the space, the error
// must name the space rather than surfacing a bare 404.
func TestFindHomePageMissingSpace(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.SetFail(scopedTokenV1Gone(http.StatusNotFound))

	_, err := api.FindHomePage("NOPE")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOPE")
	assert.Contains(t, err.Error(), "not found")
}

// TestFindHomePageSpaceWithoutHomepage separates "no such space" from "space
// has no homepage" -- reporting the latter as not-found sends people hunting
// for a typo in a space key that is correct.
func TestFindHomePageSpaceWithoutHomepage(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.SetFail(scopedTokenV1Gone(http.StatusNotFound))

	_, err := api.FindHomePage("DOCS")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no home page")
	assert.NotContains(t, err.Error(), "not found")
}

// TestFindHomePageReportsV1ErrorWhenV2Unavailable guards the diagnosability of
// the most common first failure mark can hit: FindHomePage is the first call
// made for any page, so wrong credentials surface here.
//
// Server/DC has no /api/v2, so the fallback always ends in a 404 from a path
// that never existed. Reporting that instead of v1's answer would tell a Server
// user with a bad token "404 (Not Found)" when the truth is 401.
func TestFindHomePageReportsV1ErrorWhenV2Unavailable(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.HasPrefix(r.URL.Path, "/rest/api/space/") {
			return http.StatusUnauthorized, `{"message":"bad credentials"}`, true
		}
		// Confluence Server has no v2 API; every path under it is absent.
		if strings.HasPrefix(r.URL.Path, "/api/v2") {
			return http.StatusNotFound, `<html>404</html>`, true
		}
		return 0, "", false
	})

	_, err := api.FindHomePage("DOCS")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401",
		"the v1 status is the one that explains the failure")
	assert.Contains(t, err.Error(), "Unauthorized")
}
