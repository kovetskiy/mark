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

// stubFolderTracker is a FolderTracker holding a fixed mapping.
type stubFolderTracker struct {
	folders  map[string]string
	recorded map[string]string
}

func (s *stubFolderTracker) LookupFolder(_, path string) (string, bool, error) {
	id, ok := s.folders[path]

	return id, ok, nil
}

func (s *stubFolderTracker) RecordFolder(_, path, id string) error {
	s.recorded[path] = id

	return nil
}

// TestFolderAncestryReportsAFailedRead: a folder recorded for this position is
// read back when nothing carries its title any more, because it may simply have
// been renamed. GetFolderByID answers a 404 with (nil, nil), so an error there
// is a 401, a 403 or a 5xx that outlived every retry -- and it was logged as
// "no longer exists" and swallowed.
//
// The run then created a second folder with the same title, recorded it over
// the first, and published into it: the hierarchy split in two, by a run that
// looked like it had worked.
func TestFolderAncestryReportsAFailedRead(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	anchor := server.AddPage("DOCS", "Anchor", "page", home.ID)

	const recorded = "4242"

	anchorID := anchor.ID
	folders := []string{"Manuals-failed-read"}

	tracker := &stubFolderTracker{
		folders:  map[string]string{folderPathKey(&anchorID, folders, 0): recorded},
		recorded: map[string]string{},
	}

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/folders/"+recorded) {
			return http.StatusForbidden, `{"message":"no"}`, true
		}

		return 0, "", false
	})

	api := confluence.NewAPI(server.URL, "user", "token", false)

	_, err := EnsureFolderAncestry(false, api, "DOCS", folders, &anchorID, tracker)

	require.Error(t, err, "a folder that could not be read is not a folder that is gone")
	assert.Contains(t, err.Error(), "Manuals-failed-read")
	assert.Empty(t, server.Folders(), "and no second folder was created")
	assert.Empty(t, tracker.recorded, "nor recorded over the first")
}

// TestFolderAncestryRecreatesAFolderThatIsReallyGone is the other half: a
// recorded folder that Confluence answers 404 for really has been deleted, and
// creating it again is the right thing to do.
func TestFolderAncestryRecreatesAFolderThatIsReallyGone(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	anchor := server.AddPage("DOCS", "Anchor", "page", home.ID)

	anchorID := anchor.ID
	folders := []string{"Manuals-really-gone"}

	tracker := &stubFolderTracker{
		// An id the fake has never heard of, which it answers 404 for.
		folders:  map[string]string{folderPathKey(&anchorID, folders, 0): "9999"},
		recorded: map[string]string{},
	}

	api := confluence.NewAPI(server.URL, "user", "token", false)

	parent, err := EnsureFolderAncestry(false, api, "DOCS", folders, &anchorID, tracker)

	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.Equal(t, "Manuals-really-gone", parent.Title)
	assert.Len(t, server.Folders(), 1)
}
