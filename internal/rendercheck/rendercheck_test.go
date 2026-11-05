package rendercheck

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsImageFormat(t *testing.T) {
	assert.True(t, IsImageFormat("png"))
	assert.True(t, IsImageFormat("svg"))

	for _, format := range []string{"", " svg", "SVG", "jpeg"} {
		assert.False(t, IsImageFormat(format), format)
	}
}

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
