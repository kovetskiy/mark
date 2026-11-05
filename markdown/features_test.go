package mark

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFeatureListsAgree: every goldmark feature is registered in order and
// known, and every default is known, so that no list can name a feature the
// others have never heard of.
func TestFeatureListsAgree(t *testing.T) {
	known := KnownFeatures()

	assert.True(t, slices.IsSorted(known), "KnownFeatures should be sorted")
	assert.ElementsMatch(t, slices.Collect(maps.Keys(markdownFeatures)), featureOrder)

	for _, name := range featureOrder {
		assert.Contains(t, known, name)
	}

	for _, name := range DefaultFeatures() {
		assert.Contains(t, known, name)
	}
}

// TestFeatureListsAreCopies: a caller changing what it was given changes
// nothing for the next one.
func TestFeatureListsAreCopies(t *testing.T) {
	KnownFeatures()[0] = "changed"
	DefaultFeatures()[0] = "changed"

	assert.NotContains(t, KnownFeatures(), "changed")
	assert.NotContains(t, DefaultFeatures(), "changed")
}
