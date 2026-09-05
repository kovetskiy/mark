package svg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDimensions covers the sizes an SVG can state, and the one it cannot: a
// diagram too wide for its frame is drawn as width="100%", which is not a
// number of pixels. Read as 100 of them, a wide diagram is laid out as a narrow
// one, so anything that is not an absolute length falls back to the viewBox.
func TestDimensions(t *testing.T) {
	tests := []struct {
		name   string
		svg    string
		width  float64
		height float64
	}{
		{
			name:   "width and height attributes",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="200"></svg>`,
			width:  300,
			height: 200,
		},
		{
			name:   "px is a number of pixels like any other",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="450px" height="300px"></svg>`,
			width:  450,
			height: 300,
		},
		{
			// Reported as stated: what a whole number of pixels is depends on
			// what the caller does with it, and a scale applied to a rounded
			// length rounds twice.
			name:   "a fraction is not rounded here",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="85.4375" height="42.5"></svg>`,
			width:  85.4375,
			height: 42.5,
		},
		{
			name:   "no width or height at all falls back to the viewBox",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 480"></svg>`,
			width:  640,
			height: 480,
		},
		{
			name:   "and so does whichever of the two is missing",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="320" viewBox="0 0 640 480"></svg>`,
			width:  320,
			height: 480,
		},
		{
			name:   "a percentage is not a size",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" viewBox="0 0 800 600"></svg>`,
			width:  800,
			height: 600,
		},
		{
			name:   "nor is a length in em",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" width="20em" height="15em" viewBox="0 0 320 240"></svg>`,
			width:  320,
			height: 240,
		},
		{
			name:   "the viewBox may separate its numbers with commas",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0,0,640,480"></svg>`,
			width:  640,
			height: 480,
		},
		{
			name:   "nothing to go on is reported as nothing",
			svg:    `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
			width:  0,
			height: 0,
		},
		{
			// d2 wraps its diagram in an outer svg, and mermaid may be preceded
			// by a declaration: the root element is what states the size,
			// whatever comes before it in the document.
			name:   "a declaration ahead of the root is not the root",
			svg:    `<?xml version="1.0"?><!-- drawn --><svg width="120" height="60"></svg>`,
			width:  120,
			height: 60,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height := Dimensions(test.svg)

			assert.InDelta(t, test.width, width, 0.0001)
			assert.InDelta(t, test.height, height, 0.0001)
		})
	}
}

// TestPixels covers the other half: an attachment is measured in whole pixels,
// and a length nobody stated has no measurement at all rather than a zero one.
func TestPixels(t *testing.T) {
	assert.Equal(t, "85", Pixels(85.4375))
	assert.Equal(t, "43", Pixels(42.5))
	assert.Equal(t, "300", Pixels(300))
	assert.Empty(t, Pixels(0))
}
