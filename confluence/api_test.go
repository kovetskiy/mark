package confluence

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPageCache(t *testing.T) {
	api := &API{
		pageCache:     make(map[string]*PageInfo),
		pageCacheByID: make(map[string]*PageInfo),
	}

	space := "TEST"
	title := "My Page"
	pageType := "page"

	// 1. Cache Hit returns cached page without network request
	cachedPage := &PageInfo{
		ID:    "12345",
		Title: title,
		Type:  pageType,
	}

	key := pageCacheKey(space, title, pageType)
	api.pageCache[key] = cachedPage

	// This must not panic (meaning it didn't use api.rest) and must return the cached page.
	res, err := api.FindPage(space, title, pageType)
	assert.NoError(t, err)
	assert.Equal(t, cachedPage, res)
	assert.NotSame(t, cachedPage, res) // Verify defensive copy

	// 2. Cache Hit on non-existent page (cached nil result)
	api.pageCache[key] = nil
	res, err = api.FindPage(space, title, pageType)
	assert.NoError(t, err)
	assert.Nil(t, res)

	// 3. Cache Invalidation removes entry
	api.pageCache[key] = cachedPage
	api.invalidatePage(space, title, pageType)
	_, ok := api.pageCache[key]
	assert.False(t, ok)

	// 4. Cache Version Update updates version in-place
	api.pageCache[key] = cachedPage
	api.pageCacheByID[cachedPage.ID] = cachedPage
	api.updateCachedPageVersion(cachedPage.ID, 42)
	assert.Equal(t, int64(42), api.pageCache[key].Version.Number)

	// 5. Concurrent Cache Operations
	// Validates RWMutex under concurrent goroutines using tight loops to create contention.
	done := make(chan bool)
	go func() {
		for i := 0; i < 1000; i++ {
			api.updateCachedPageVersion(cachedPage.ID, int64(i))
		}
		done <- true
	}()
	for i := 0; i < 1000; i++ {
		_, err = api.FindPage(space, title, pageType)
		assert.NoError(t, err)
	}
	<-done
}
