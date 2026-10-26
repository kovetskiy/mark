package confluence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
// carried the write out, asking for a wait of 7 seconds. A FailFunc on the fake is the other case: a write
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
			resp.Header.Set("Retry-After", "7")
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
	api.unavailableWait = func(int, string) {}
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

// inFlight answers the first write that matches 503 without carrying it out,
// and lets land carry it out just before the second one arrives: the first
// was still on its way when mark read that it had not landed.
func inFlight(server *confluencetest.Server, match func(*http.Request) bool, land func()) {
	var seen atomic.Int32
	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if !match(r) {
			return 0, "", false
		}
		if seen.Add(1) == 1 {
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		if seen.Load() == 2 {
			land()
		}
		return 0, "", false
	})
}

func TestCreatePageThatLandedLateIsKept(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	server.AddSpace("DOCS")
	var landed *confluencetest.Page
	inFlight(server, isPost("/rest/api/content/"), func() {
		landed = server.AddPage("DOCS", "Late", "page", "")
	})

	page, err := api.CreatePage("DOCS", "page", nil, "Late", "")
	require.NoError(t, err, "the second create's duplicate title is the first one landing")
	assert.Equal(t, landed.ID, page.ID)
}

// A page with the title under another parent is not the one the create made,
// so mark does not adopt it and write over it.
func TestCreatePageDoesNotAdoptAPageUnderAnotherParent(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	server.AddSpace("DOCS")
	parent := server.AddPage("DOCS", "Parent", "page", "")
	other := server.AddPage("DOCS", "Other", "page", "")
	var taken atomic.Bool
	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if isPost("/rest/api/content/")(r) && !taken.Swap(true) {
			server.AddPage("DOCS", "Taken", "page", other.ID)
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})

	parentInfo, err := api.GetPageByID(parent.ID)
	require.NoError(t, err)

	_, err = api.CreatePage("DOCS", "page", parentInfo, "Taken", "")
	require.ErrorContains(t, err, "already exists")
}

// The update still in flight lands as the version mark sends again, which
// Confluence refuses as a conflict. That is mark's own version, not an edit
// --no-overwrite has to protect.
func TestUpdatePageThatLandedLateIsNotAConflict(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	api.KeepConcurrentEdits = true
	id := server.AddPage("DOCS", "Target", "page", "").ID
	inFlight(server, func(r *http.Request) bool { return isContentPut(r, id) }, func() {
		server.WritePage(id, "<p>from mark</p>")
	})

	page, err := api.GetPageByID(id)
	require.NoError(t, err)

	require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "", "", ""))
	assert.EqualValues(t, 2, server.Page(id).Version, "no version on top of the one that landed")
	assert.EqualValues(t, 2, page.Version.Number)
}

// No request carries a context a caller can cancel, so a deadline is
// net/http's own, such as its wait for response headers, and as ambiguous as
// a 504.
func TestATimeoutIsUnavailable(t *testing.T) {
	timeout := fmt.Errorf("timeout awaiting response headers: %w", context.DeadlineExceeded)

	err := newTransportError(nil, "update page", &url.Error{Op: "Put", URL: "https://x", Err: timeout})
	assert.True(t, isUnavailable(err))

	err = newTransportError(nil, "read page", &url.Error{Op: "Get", URL: "https://x", Err: timeout})
	assert.False(t, isUnavailable(err), "a read changed nothing, and the transport has retried it")

	err = newTransportError(nil, "update page", &url.Error{Op: "parse", URL: "%", Err: errors.New("bad")})
	assert.False(t, isUnavailable(err), "a request that could not be built was never sent")

	err = newTransportError(nil, "update page", &url.Error{Op: "Put", URL: "https://x", Err: &net.DNSError{Err: "no such host"}})
	assert.False(t, isUnavailable(err), "a name that did not resolve stopped the request before it was sent")
}

// A single edit by somebody else during the outage makes the version the
// update would have made, but it is not mark's.
func TestUpdatePageUnavailableThenEditedOnceIsAConflict(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	api.KeepConcurrentEdits = true
	id := server.AddPage("DOCS", "Target", "page", "").ID

	page, err := api.GetPageByID(id)
	require.NoError(t, err)

	var once atomic.Bool
	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if isContentPut(r, id) && !once.Swap(true) {
			server.EditPage(id, "<p>by hand</p>")
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})

	err = api.UpdatePage(page, "<p>from mark</p>", false, "", "", "")
	require.ErrorIs(t, err, errConflict)
	assert.Contains(t, err.Error(), "--no-overwrite")
	assert.Equal(t, "<p>by hand</p>", server.Page(id).Body)
}

// The last attempt is checked for having landed like any other.
func TestCreatePageThatLandedOnTheLastAttemptIsKept(t *testing.T) {
	api, server := newUnavailableAPI(t, false, maxAttempts, isPost("/rest/api/content/"))
	failWhen(server, false, nil)
	server.AddSpace("DOCS")
	refuse(server, false, maxAttempts-1, isPost("/rest/api/content/"))

	page, err := api.CreatePage("DOCS", "page", nil, "Last", "")
	require.NoError(t, err)
	assert.Equal(t, "Last", server.Page(page.ID).Title)
	assert.Equal(t, maxAttempts, server.CountRequests(http.MethodPost, "/rest/api/content"))
}

func TestCreatePageUnderAParentThatLandedIsKept(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 1, isPost("/rest/api/content/"))
	server.AddSpace("DOCS")
	parent, err := api.GetPageByID(server.AddPage("DOCS", "Parent", "page", "").ID)
	require.NoError(t, err)

	page, err := api.CreatePage("DOCS", "page", parent, "Child", "")
	require.NoError(t, err)
	assert.Equal(t, parent.ID, server.Page(page.ID).ParentID)
	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/rest/api/content"))
}

func TestCreatePageUnderAFolderThatLandedIsKept(t *testing.T) {
	api, server := newUnavailableAPI(t, true, 1, isPost("/api/v2/pages"))
	failWhen(server, true, nil)
	server.AddSpace("DOCS")
	folder := server.AddFolder("DOCS", "Folder", "", "")

	page, err := api.CreatePageWithFolderParent("DOCS", "page", folder.ID, "In a folder", "")
	require.NoError(t, err)
	assert.Equal(t, folder.ID, server.Page(page.ID).ParentID)
	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/api/v2/pages"))
}

// A Retry-After on an unavailable write is waited out before reading whether
// it landed.
func TestUnavailableWriteWaitsForRetryAfter(t *testing.T) {
	var id string
	api, server := newUnavailableAPI(t, false, 1, func(r *http.Request) bool { return isContentPut(r, id) })
	var waited []string
	api.unavailableWait = func(_ int, retryAfter string) { waited = append(waited, retryAfter) }
	id = server.AddPage("DOCS", "Target", "page", "").ID

	page, err := api.GetPageByID(id)
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "", "", ""))
	assert.Equal(t, []string{"7"}, waited)
}

func TestUpdatePageUnavailableThenEditedOnceIsAConflictThroughTheGateway(t *testing.T) {
	api, server := newUnavailableAPI(t, true, 0, nil)
	api.KeepConcurrentEdits = true
	id := server.AddPage("DOCS", "Target", "page", "").ID

	page, err := api.GetPageByID(id)
	require.NoError(t, err)

	var once atomic.Bool
	failWhen(server, true, func(r *http.Request) (int, string, bool) {
		if isContentPut(r, id) && !once.Swap(true) {
			server.EditPage(id, "<p>by hand</p>")
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})

	err = api.UpdatePage(page, "<p>from mark</p>", false, "", "", "")
	require.ErrorIs(t, err, errConflict)
	assert.Equal(t, "<p>by hand</p>", server.Page(id).Body)
}

// A version mark cannot tell is its own is not taken for it: mark writes its
// content once more rather than risk leaving somebody else's in its place.
func TestUpdatePageWhoseAuthorCannotBeReadIsWrittenAgain(t *testing.T) {
	var id string
	api, server := newUnavailableAPI(t, false, 1, func(r *http.Request) bool { return isContentPut(r, id) })
	id = server.AddPage("DOCS", "Target", "page", "").ID

	page, err := api.GetPageByID(id)
	require.NoError(t, err)
	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if strings.HasSuffix(r.URL.Path, "/user/current") {
			return http.StatusInternalServerError, `{"message":"broken"}`, true
		}
		return 0, "", false
	})

	require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "", "", ""))
	assert.Equal(t, "<p>from mark</p>", server.Page(id).Body)
	assert.EqualValues(t, 3, server.Page(id).Version)
}

// The update sent again after a conflict is settled the same way as the first.
func TestUpdatePageRetriedAfterAConflictThatLandedIsKept(t *testing.T) {
	var id string
	var puts atomic.Int32
	api, server := newUnavailableAPI(t, false, 1, func(r *http.Request) bool {
		return isContentPut(r, id) && puts.Load() == 2
	})
	id = server.AddPage("DOCS", "Target", "page", "").ID

	page, err := api.GetPageByID(id)
	require.NoError(t, err)
	server.EditPage(id, "<p>by hand</p>")
	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if isContentPut(r, id) {
			puts.Add(1)
		}
		return 0, "", false
	})

	require.NoError(t, api.UpdatePage(page, "<p>from mark</p>", false, "", "", ""))
	assert.Equal(t, "<p>from mark</p>", server.Page(id).Body)
	assert.EqualValues(t, 3, server.Page(id).Version)
	assert.EqualValues(t, 3, page.Version.Number)
	assert.Equal(t, 2, countContentPuts(server, id))
}

// A page somebody else made with the title in the meantime is not the one
// the create made, even under the same parent.
func TestCreatePageDoesNotAdoptAPageSomebodyElseMade(t *testing.T) {
	api, server := newUnavailableAPI(t, false, 0, nil)
	server.AddSpace("DOCS")
	var made atomic.Bool
	failWhen(server, false, func(r *http.Request) (int, string, bool) {
		if isPost("/rest/api/content/")(r) && !made.Swap(true) {
			server.EditPage(server.AddPage("DOCS", "Taken", "page", "").ID, "<p>by hand</p>")
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})

	_, err := api.CreatePage("DOCS", "page", nil, "Taken", "")
	require.ErrorContains(t, err, "already exists")
}
