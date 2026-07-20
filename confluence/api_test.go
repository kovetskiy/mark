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
}
