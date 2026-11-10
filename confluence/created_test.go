package confluence_test

import (
	"context"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreatedVersionFollowsLaterWrites: a page this API created and then wrote
// again, by an update or a move, is still at a version mark wrote, so
// CreatedVersion gives the latest one; a page it did not create has none.
func TestCreatedVersionFollowsLaterWrites(t *testing.T) {
	ctx := context.Background()
	api, server := newDataCenterAPI(t)
	parent := server.AddPage("DOCS", "Parent", "page", "")
	existing := server.AddPage("DOCS", "Existing", "page", "")

	created, err := api.CreatePage(ctx, "DOCS", confluence.ContentPage, nil, "Landing", "")
	require.NoError(t, err)

	version, ok := api.CreatedVersion(created.ID)
	require.True(t, ok)
	assert.Equal(t, server.Page(created.ID).Version, version)

	require.NoError(t, api.UpdatePage(ctx, created, "<p>written</p>", confluence.UpdateOptions{}))
	version, ok = api.CreatedVersion(created.ID)
	require.True(t, ok)
	assert.Equal(t, server.Page(created.ID).Version, version, "after an update")

	require.NoError(t, api.MoveContentAppend(ctx, created.ID, parent.ID))
	version, ok = api.CreatedVersion(created.ID)
	require.True(t, ok)
	assert.Equal(t, server.Page(created.ID).Version, version, "after a move")

	page, err := api.GetPageByID(ctx, existing.ID)
	require.NoError(t, err)
	require.NoError(t, api.UpdatePage(ctx, page, "<p>written</p>", confluence.UpdateOptions{}))
	_, ok = api.CreatedVersion(existing.ID)
	assert.False(t, ok, "a page this API did not create")
}
