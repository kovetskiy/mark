package page

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
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
