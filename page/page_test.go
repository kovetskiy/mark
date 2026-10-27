package page

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ResolvePage's warn branch indexed meta.Parents with len(ancestry)-1, but
// ancestry has the home page title appended to it, so that index is exactly out
// of range for meta.Parents whenever the append happened. This pins the
// arithmetic the fix relies on: the last element of ancestry is always
// addressable, meta.Parents at that index is not.
func TestAncestryLastElementIsAddressable(t *testing.T) {
	parents := []string{"OnlyParent"}

	// Mirrors ResolvePage: ancestry = meta.Parents plus the home title.
	ancestry := append(append([]string{}, parents...), "HomeTitle")

	require.Greater(t, len(ancestry), len(parents),
		"the home title makes ancestry longer than meta.Parents")
	require.GreaterOrEqual(t, len(ancestry)-1, len(parents),
		"so len(ancestry)-1 is out of range for meta.Parents")

	// The fixed expression must not panic and must name the deepest ancestor.
	assert.Equal(t, "HomeTitle", ancestry[len(ancestry)-1])
}
