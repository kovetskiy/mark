package confluence

import (
	"net/http"
	"net/http/httptest"
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

	// 4. Cache Version Update replaces cached pointer with copy containing updated version (across all alias pointers mapping to the same ID)
	aliasPage := &PageInfo{
		ID:    cachedPage.ID,
		Title: "Alias Title",
		Type:  pageType,
	}
	aliasKey := pageCacheKey(space, "Alias Title", pageType)
	api.pageCache[key] = cachedPage
	api.pageCache[aliasKey] = aliasPage
	api.pageCacheByID[cachedPage.ID] = cachedPage
	api.updateCachedPageVersion(cachedPage.ID, 42)
	assert.Equal(t, int64(42), api.pageCache[key].Version.Number)
	assert.Equal(t, int64(42), api.pageCache[aliasKey].Version.Number)

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

func TestPageCacheFindPageMissAndHit(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Return a mock Confluence response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"results": [
				{
					"id": "12345",
					"title": "My Page",
					"type": "page",
					"version": {
						"number": 1
					}
				}
			],
			"_links": {
				"base": "http://mock-confluence"
			}
		}`))
	}))
	defer server.Close()

	api := NewAPI(server.URL, "username", "password", true)

	space := "TEST"
	title := "My Page"
	pageType := "page"

	// First call: cache miss, triggers HTTP request to server
	res1, err := api.FindPage(space, title, pageType)
	assert.NoError(t, err)
	assert.NotNil(t, res1)
	assert.Equal(t, "12345", res1.ID)
	assert.Equal(t, 1, callCount)

	// Second call: cache hit, should NOT trigger HTTP request
	res2, err := api.FindPage(space, title, pageType)
	assert.NoError(t, err)
	assert.NotNil(t, res2)
	assert.Equal(t, "12345", res2.ID)
	assert.Equal(t, 1, callCount) // callCount remains 1!
	assert.Equal(t, res1.Version.Number, res2.Version.Number)
}

func TestPageCacheFindPageNegativeCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results": []}`))
	}))
	defer server.Close()

	api := NewAPI(server.URL, "username", "password", true)

	space := "TEST"
	title := "Missing Page"
	pageType := "page"

	// First call: cache miss, returns nil
	res1, err := api.FindPage(space, title, pageType)
	assert.NoError(t, err)
	assert.Nil(t, res1)
	assert.Equal(t, 1, callCount)

	// Second call: cache hit, returns nil without network call
	res2, err := api.FindPage(space, title, pageType)
	assert.NoError(t, err)
	assert.Nil(t, res2)
	assert.Equal(t, 1, callCount)
}

func TestPageCacheZeroValueAPI(t *testing.T) {
	// Verifies lazy initialization handles zero-valued API structures
	api := &API{}

	// invalidatePage should not panic
	api.invalidatePage("TEST", "Title", "page")

	// updateCachedPageVersion should not panic
	api.updateCachedPageVersion("12345", 2)

	// setCacheEntry should not panic
	api.pageCacheMutex.Lock()
	api.setCacheEntry("key", &PageInfo{ID: "123"})
	api.pageCacheMutex.Unlock()
}
