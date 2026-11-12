package rendercheck

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsScale(t *testing.T) {
	assert.True(t, IsScale(0.5))
	assert.True(t, IsScale(3))

	for _, scale := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		assert.False(t, IsScale(scale), scale)
	}
}

func TestImageAlign(t *testing.T) {
	align, ok := ImageAlign(" Center ")
	assert.True(t, ok)
	assert.Equal(t, "center", align)

	_, ok = ImageAlign("middle")
	assert.False(t, ok)
}

func TestFeatures(t *testing.T) {
	known := []string{"math", "mermaid"}

	for _, features := range [][]string{nil, {}, {NoFeatures}, {"math"}, {"mermaid", "math"}} {
		assert.NoError(t, Features(features, known), features)
	}

	for _, features := range [][]string{{"maths"}, {"Math"}, {" math"}, {""}, {"math", "other"}} {
		assert.ErrorContains(t, Features(features, known), "expected any of: math, mermaid, or none", features)
	}

	for _, features := range [][]string{{NoFeatures, "math"}, {"math", NoFeatures}, {NoFeatures, NoFeatures}} {
		assert.ErrorContains(t, Features(features, known), "cannot be combined", features)
	}
}
