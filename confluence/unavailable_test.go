package confluence

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUnavailableAPI points an API at a gateway in front of the fake. The
// gateway hands the request on and then answers the first `times` that match
// with 504, the way one that gave up waiting does after Confluence has
// carried the write out. A FailFunc on the fake is the other case: a write
// refused before Confluence did anything.
func newUnavailableAPI(
	t *testing.T, gateway bool, times int, match func(*http.Request) bool,
) (*API, *confluencetest.Server) {
	t.Helper()
	server := confluencetest.New(t)

	target, err := url.Parse(server.URL)
	require.NoError(t, err)

	var timedOut atomic.Int32
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(resp *http.Response) error {
		if match != nil && match(resp.Request) && int(timedOut.Add(1)) <= times {
			_ = resp.Body.Close()
			resp.StatusCode = http.StatusGatewayTimeout
			resp.Status = "504 Gateway Timeout"
			resp.Body = io.NopCloser(strings.NewReader("upstream timed out"))
			resp.ContentLength = -1
			resp.Header.Del("Content-Length")
			resp.Header.Set("Content-Type", "text/plain")
		}
		return nil
	}
	front := httptest.NewServer(proxy)
	t.Cleanup(front.Close)

	base := front.URL
	if gateway {
		base += "/ex/confluence/cloud-id"
	}

	api := NewAPI(base, "user", "token", false)
	api.conflictRetryDelay = 0
	api.unavailableWait = func(int) {}
	return api, server
}

func isPost(suffix string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, suffix)
	}
}

func refuse(server *confluencetest.Server, gateway bool, times int, match func(*http.Request) bool) {
	var refused atomic.Int32
	failWhen(server, gateway, func(r *http.Request) (int, string, bool) {
		if match(r) && int(refused.Add(1)) <= times {
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})
}

func TestCreatePageThatLandedIsNotSentAgain(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gateway bool
		path    string
	}{
		{"v1", false, "/rest/api/content/"},
		{"gateway", true, "/api/v2/pages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, server := newUnavailableAPI(t, tc.gateway, 1, isPost(tc.path))
			failWhen(server, tc.gateway, nil)
			server.AddSpace("DOCS")

			page, err := api.CreatePage("DOCS", "page", nil, "Once Only", "<p>body</p>")
			require.NoError(t, err)
			require.NotNil(t, page)

			assert.Equal(t, 1, server.CountRequests(http.MethodPost, tc.path),
				"a create that landed must not be sent again")
			stored := server.Page(page.ID)
			require.NotNil(t, stored, "the page returned is the one the create made")
			assert.Equal(t, "Once Only", stored.Title)
		})
	}
}

func TestCreatePageThatDidNotLandIsSentAgain(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	server.AddSpace("DOCS")
	refuse(server, false, 2, isPost("/rest/api/content/"))

	page, err := api.CreatePage("DOCS", "page", nil, "Third Time", "<p>body</p>")
	require.NoError(t, err)
	require.NotNil(t, server.Page(page.ID))
	assert.Equal(t, 3, server.CountRequests(http.MethodPost, "/rest/api/content"))
}

func TestCreatePageGivesUpAfterMaxAttempts(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	server.AddSpace("DOCS")
	refuse(server, false, maxAttempts, isPost("/rest/api/content/"))

	_, err := api.CreatePage("DOCS", "page", nil, "Never", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
	assert.Equal(t, maxAttempts, server.CountRequests(http.MethodPost, "/rest/api/content"))
}

// A 500 says Confluence failed the request, not that it is unavailable, and
// sending it again gets the same answer.
func TestCreatePageIsNotSentAgainAfterA500(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	server.AddSpace("DOCS")
	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPost {
			return http.StatusInternalServerError, `{"message":"broken"}`, true
		}
		return 0, "", false
	})

	_, err := api.CreatePage("DOCS", "page", nil, "Once Only", "")
	require.Error(t, err)
	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/rest/api/content"))
}

func TestUpdatePageThatLandedIsNotSentAgain(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gateway bool
	}{
		{"v1", false},
		{"gateway", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var id string
			api, server := newUnavailableAPI(t, tc.gateway, 1, func(r *http.Request) bool {
				return isContentPut(r, id)
			})
			failWhen(server, tc.gateway, nil)
			id = server.AddPage("DOCS", "Target", "page", "").ID

			page, err := api.GetPageByID(id)
			require.NoError(t, err)
			server.ResetRequests()

			require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "", "", ""))

			assert.Equal(t, 1, countContentPuts(server, id))
			assert.EqualValues(t, 2, server.Page(id).Version,
				"an update sent again would have made a third version")
			assert.EqualValues(t, 2, page.Version.Number)
		})
	}
}

func TestUpdatePageThatDidNotLandIsSentAgain(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	id := server.AddPage("DOCS", "Target", "page", "").ID
	refuse(server, false, 1, func(r *http.Request) bool { return isContentPut(r, id) })

	page, err := api.GetPageByID(id)
	require.NoError(t, err)

	require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "", "", ""))
	assert.Equal(t, 2, countContentPuts(server, id))
	assert.Equal(t, "<p>from mark</p>", server.Page(id).Body)
	assert.EqualValues(t, 2, server.Page(id).Version)
}

// Somebody else's version in the meantime is a conflict, and --no-overwrite
// refuses to write over it as it does over any other.
func TestUpdatePageUnavailableThenEditedIsAConflict(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	api.KeepConcurrentEdits = true
	id := server.AddPage("DOCS", "Target", "page", "").ID

	page, err := api.GetPageByID(id)
	require.NoError(t, err)

	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if isContentPut(r, id) {
			server.EditPage(id, "<p>by hand</p>")
			server.EditPage(id, "<p>by hand, twice</p>")
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})

	err = api.UpdatePage(page, "<p>from mark</p>", false, "", "", "")
	require.ErrorIs(t, err, errConflict)
	assert.Contains(t, err.Error(), "--no-overwrite")
	assert.Equal(t, "<p>by hand, twice</p>", server.Page(id).Body)
}

func TestCreateAttachmentThatLandedIsNotSentAgain(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 1, isPost("/child/attachment"))
	id := server.AddPage("DOCS", "Target", "page", "").ID

	info, err := api.CreateAttachment(id, "a.png", "mark:checksum: abc", strings.NewReader("png"))
	require.NoError(t, err)
	assert.NotEmpty(t, info.ID)
	assert.Len(t, server.Attachments(id), 1)
	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/child/attachment"))
}

func TestCreateAttachmentThatDidNotLandIsSentAgain(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	id := server.AddPage("DOCS", "Target", "page", "").ID
	refuse(server, false, 1, isPost("/child/attachment"))

	info, err := api.CreateAttachment(id, "a.png", "mark:checksum: abc", strings.NewReader("png"))
	require.NoError(t, err)
	assert.NotEmpty(t, info.ID)

	attachments := server.Attachments(id)
	require.Len(t, attachments, 1)
	assert.Equal(t, []byte("png"), attachments[0].Data, "the body is sent whole the second time")
	assert.Equal(t, 2, server.CountRequests(http.MethodPost, "/child/attachment"))
}

func TestUpdateAttachmentThatLandedIsNotSentAgain(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 1, isPost("/data"))
	id := server.AddPage("DOCS", "Target", "page", "").ID
	attachment := server.AddAttachment(id, "a.png", "mark:checksum: old")

	info, err := api.UpdateAttachment(id, attachment.ID, "a.png", "mark:checksum: new", strings.NewReader("new"))
	require.NoError(t, err)
	assert.Equal(t, attachment.ID, info.ID)
	assert.Equal(t, "mark:checksum: new", server.Attachments(id)[0].Comment)
	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/data"))
}

func TestUpdateAttachmentThatDidNotLandIsSentAgain(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	id := server.AddPage("DOCS", "Target", "page", "").ID
	attachment := server.AddAttachment(id, "a.png", "mark:checksum: old")
	refuse(server, false, 1, isPost("/data"))

	_, err := api.UpdateAttachment(id, attachment.ID, "a.png", "mark:checksum: new", strings.NewReader("new"))
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), server.Attachments(id)[0].Data)
	assert.Equal(t, 2, server.CountRequests(http.MethodPost, "/data"))
}
