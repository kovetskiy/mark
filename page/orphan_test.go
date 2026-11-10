package page

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
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
	ctx := t.Context()
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	inScope, err := InScope(ctx, api, "2000", Orphan{
		Path: "gone.md", PageID: "9999", Title: "Gone",
	})

	require.NoError(t, err, "a page that is already gone is not a failed run")
	assert.False(t, inScope)
}

// TestInScopeSeesFolders: --orphan-under accepts a folder, and a page in a
// folder has the folder, and whatever is above it, out of its v1 ancestors.
func TestInScopeSeesFolders(t *testing.T) {
	ctx := t.Context()
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	folder := server.AddFolder("DOCS", "Box", home.ID, "page")
	direct := server.AddPage("DOCS", "Direct", "page", folder.ID)
	nested := server.AddPage("DOCS", "Nested", "page", direct.ID)
	outside := server.AddPage("DOCS", "Outside", "page", home.ID)

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	scope, err := ResolveScope(ctx, api, "DOCS", "Box")
	require.NoError(t, err)
	require.Equal(t, folder.ID, scope)

	for _, tc := range []struct {
		name    string
		scopeID string
		page    *confluencetest.Page
		want    bool
	}{
		{"directly in the folder", folder.ID, direct, true},
		{"nested below the folder", folder.ID, nested, true},
		{"beside the folder", folder.ID, outside, false},
		{"page scope above the folder", home.ID, nested, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inScope, err := InScope(ctx, api, tc.scopeID, Orphan{
				Path: "doc.md", PageID: tc.page.ID, Title: tc.page.Title,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, inScope)
		})
	}
}

// TestInScopeLeavesV2AloneOnDataCenter: Server and Data Center have no
// folders and no v2, so v1's ancestors are the whole answer there.
func TestInScopeLeavesV2AloneOnDataCenter(t *testing.T) {
	ctx := t.Context()
	server := confluencetest.New(t)
	server.SetDataCenter()
	home := server.AddPage("DOCS", "Home", "page", "")
	other := server.AddPage("DOCS", "Other", "page", "")
	page := server.AddPage("DOCS", "Doc", "page", home.ID)

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	inScope, err := InScope(ctx, api, other.ID, Orphan{Path: "doc.md", PageID: page.ID, Title: "Doc"})
	require.NoError(t, err)
	assert.False(t, inScope)

	for _, r := range server.Requests() {
		assert.NotContains(t, r.Path, "/api/v2/")
	}
}

// TestInScopeReportsARealFailure is the other half. A 403 says nothing about
// where the page sits, and answering "not in scope" would quietly leave every
// orphan alone while looking like a clean run.
func TestInScopeReportsARealFailure(t *testing.T) {
	ctx := t.Context()
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	page := server.AddPage("DOCS", "Doc", "page", home.ID)

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/content/"+page.ID) {
			return http.StatusForbidden, `{"message":"no"}`, true
		}

		return 0, "", false
	})

	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	_, err := InScope(ctx, api, home.ID, Orphan{
		Path: "doc.md", PageID: page.ID, Title: "Doc",
	})

	require.Error(t, err)
	assert.NotErrorIs(t, err, confluence.ErrNotFound)
	assert.Contains(t, err.Error(), page.ID)
}
