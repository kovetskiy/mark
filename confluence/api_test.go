package confluence

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPageCache(t *testing.T) {
	api := &API{
		pageCache: make(map[string]*PageInfo),
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

	// 4. Cache Invalidation by ID removes entry
	api.pageCache[key] = cachedPage
	api.invalidatePageByID(cachedPage.ID)
	_, ok = api.pageCache[key]
	assert.False(t, ok)
}
