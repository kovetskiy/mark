package page

import (
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

// TestFolderCacheConcurrentAccess hammers the process-wide folder cache from
// many goroutines. Before the mutex, an unsynchronised map write here was not
// a subtle wrong answer but a hard "concurrent map writes" throw, and the race
// detector reports it even when the throw does not fire.
//
// The test lives in package page rather than page_test because the cache and
// its accessors are unexported.
func TestFolderCacheConcurrentAccess(t *testing.T) {
	const (
		goroutines = 16
		perRoutine = 200
	)

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range perRoutine {
				// Interleave distinct and shared keys so writers collide both
				// on the map itself and on individual entries.
				unique := "space-" + strconv.Itoa(g)
				shared := "shared"
				title := "folder-" + strconv.Itoa(i)

				cacheFolder(unique, "parent", title, strconv.Itoa(g*perRoutine+i))
				cacheFolder(shared, "parent", "contended", strconv.Itoa(i))

				_, _ = cachedFolderID(unique, "parent", title)
				_, _ = cachedFolderID(shared, "parent", "contended")
				_, _ = cachedFolderID("absent", "parent", title)
			}
		}(g)
	}
	wg.Wait()

	// Every unique key written should still be readable: a torn map would
	// typically have thrown by now, but this also catches lost writes.
	for g := range goroutines {
		id, ok := cachedFolderID("space-"+strconv.Itoa(g), "parent", "folder-0")
		assert.True(t, ok, "entry for goroutine %d went missing", g)
		assert.Equal(t, strconv.Itoa(g*perRoutine), id)
	}

	_, ok := cachedFolderID("shared", "parent", "contended")
	assert.True(t, ok, "the contended entry should hold one of the written values")
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
