package page

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInScopeTreatsADeletedPageAsGone: a tracked page deleted by hand in
// Confluence is out of scope for acting on, not a reason to end the run.
//
// GetPageByIDExpanded answers a 404 with an error and never with (nil, nil), so
// the nil check this used to rely on was unreachable and the error went
// straight up. The same repository publishes fine without --orphan-under, which
// made the flag itself look broken.
func TestInScopeTreatsADeletedPageAsGone(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	inScope, err := InScope(api, "2000", Orphan{
		Path: "gone.md", PageID: "9999", Title: "Gone",
	})

	require.NoError(t, err, "a page that is already gone is not a failed run")
	assert.False(t, inScope)
}

// TestInScopeReportsARealFailure is the other half. A 403 says nothing about
// where the page sits, and answering "not in scope" would quietly leave every
// orphan alone while looking like a clean run.
func TestInScopeReportsARealFailure(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	page := server.AddPage("DOCS", "Doc", "page", home.ID)

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/content/"+page.ID) {
			return http.StatusForbidden, `{"message":"no"}`, true
		}

		return 0, "", false
	})

	api := confluence.NewAPI(server.URL, "user", "token", false)

	_, err := InScope(api, home.ID, Orphan{
		Path: "doc.md", PageID: page.ID, Title: "Doc",
	})

	require.Error(t, err)
	assert.NotErrorIs(t, err, confluence.ErrNotFound)
	assert.Contains(t, err.Error(), page.ID)
}
