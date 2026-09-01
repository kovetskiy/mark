package confluence_test

import (
	"fmt"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListContentPropertiesReadsEveryPage pins the class of bug where a listing
// stops at the first page and the caller cannot tell.
//
// On Server and Data Center every manifest shard is a content property of the
// space homepage, sharing that object with `editor`,
// `content-appearance-published`, the emoji-title keys and every other app on
// the instance. Past one page a shard silently disappears from the listing, the
// caller loses every mapping it held, and its next write POSTs a key that
// already exists.
func TestListContentPropertiesReadsEveryPage(t *testing.T) {
	api, server := newAPI(t)
	page := server.AddPage("DOCS", "Home", "page", "")

	const total = 250
	for i := range total {
		server.SetSpaceProperty(page.ID, fmt.Sprintf("key-%03d", i), []byte(`"v"`))
	}

	properties, err := api.ListContentProperties(page.ID)
	require.NoError(t, err)
	assert.Len(t, properties, total, "every page of the collection has to be read")

	seen := map[string]bool{}
	for _, p := range properties {
		seen[p.Key] = true
	}
	assert.True(t, seen["key-249"], "the last property must survive the walk")

	assert.Equal(t, 3, server.CountRequests("GET", "/content/"+page.ID+"/property"),
		"250 properties at 100 per page is three requests")
}

// TestListSpacePropertiesFollowsTheCursor is the v2 half of the same bug. v2
// pages by an opaque cursor rather than by offset, so the client has to read
// _links.next instead of counting.
func TestListSpacePropertiesFollowsTheCursor(t *testing.T) {
	api, server := newAPI(t)
	space := server.AddSpace("DOCS")

	const total = 250
	for i := range total {
		server.SetSpaceProperty(space.ID, fmt.Sprintf("key-%03d", i), []byte(`"v"`))
	}

	properties, err := api.ListSpaceProperties(space.ID)
	require.NoError(t, err)
	assert.Len(t, properties, total)

	last := properties[len(properties)-1]
	assert.Equal(t, "key-249", last.Key)

	assert.Equal(t, 3, server.CountRequests("GET", "/spaces/"+space.ID+"/properties"))
}

// TestListPropertiesOnAnEmptyOwnerIsNotAnError keeps the ordinary first-run
// case working through the new loop: nothing stored is not a failure.
func TestListPropertiesOnAnEmptyOwnerIsNotAnError(t *testing.T) {
	api, server := newAPI(t)
	space := server.AddSpace("DOCS")
	page := server.AddPage("DOCS", "Home", "page", "")

	spaceProperties, err := api.ListSpaceProperties(space.ID)
	require.NoError(t, err)
	assert.Empty(t, spaceProperties)

	contentProperties, err := api.ListContentProperties(page.ID)
	require.NoError(t, err)
	assert.Empty(t, contentProperties)
}

// TestCreatingAPropertyThatAlreadyExistsIsLoud pins the diagnosis, which is the
// half of the pagination bug that made it permanent.
//
// A create that collides with a key nothing listed is not a lost race. It says
// the listing was incomplete and its mappings have already been dropped.
// Reporting it as ErrPropertyConflict made callers log "updated by a concurrent
// run" and carry on, losing the same data again on every run afterwards.
func TestCreatingAPropertyThatAlreadyExistsIsLoud(t *testing.T) {
	api, server := newAPI(t)
	space := server.AddSpace("DOCS")
	page := server.AddPage("DOCS", "Home", "page", "")

	server.SetSpaceProperty(space.ID, "mark:manifest:0", []byte(`{"v":1}`))
	server.SetSpaceProperty(page.ID, "mark:manifest:0", []byte(`{"v":1}`))

	// existing == nil is the caller saying "the listing showed no such key".
	err := api.SetSpaceProperty(space.ID, "mark:manifest:0", []byte(`{"v":2}`), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, confluence.ErrPropertyUnseen)
	assert.NotErrorIs(t, err, confluence.ErrPropertyConflict,
		"a caller that treats this as a lost race carries on losing the data")

	err = api.SetContentProperty(page.ID, "mark:manifest:0", []byte(`{"v":2}`), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, confluence.ErrPropertyUnseen)
}

// TestStalePropertyUpdateStaysAConflict is the counterpart: a write that names
// a version it no longer supersedes really is a lost race, and callers depend
// on being able to shrug that one off.
func TestStalePropertyUpdateStaysAConflict(t *testing.T) {
	api, server := newAPI(t)
	space := server.AddSpace("DOCS")

	stored := server.SetSpaceProperty(space.ID, "mark:manifest:0", []byte(`{"v":1}`))
	// Somebody else has written since; the version in hand is one behind.
	server.SetSpaceProperty(space.ID, "mark:manifest:0", []byte(`{"v":2}`))

	stale := &confluence.Property{ID: stored.ID, Key: stored.Key}
	stale.Version.Number = stored.Version

	err := api.SetSpaceProperty(space.ID, "mark:manifest:0", []byte(`{"v":3}`), stale)
	require.Error(t, err)
	assert.ErrorIs(t, err, confluence.ErrPropertyConflict)
	assert.NotErrorIs(t, err, confluence.ErrPropertyUnseen)
}
