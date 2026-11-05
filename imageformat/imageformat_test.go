package imageformat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	for value, want := range map[string]Format{
		"png":   PNG,
		"svg":   SVG,
		" SVG ": SVG,
		"\tPng": PNG,
	} {
		got, err := Parse(value)
		require.NoError(t, err, value)
		assert.Equal(t, want, got, value)
	}

	for _, value := range []string{"", " ", "jpeg", "s v g", "svgz"} {
		_, err := Parse(value)
		assert.Error(t, err, "%q", value)
	}
}

func TestValid(t *testing.T) {
	assert.True(t, PNG.Valid())
	assert.True(t, SVG.Valid())
	assert.False(t, Format("").Valid(), "the zero value is the caller's to read")
	assert.False(t, Format("SVG").Valid(), "compared as written")
	assert.False(t, Format(" svg").Valid())
}

// The values are what attachment names and checksums are built from, so they
// must not change.
func TestValues(t *testing.T) {
	assert.Equal(t, "png", string(PNG))
	assert.Equal(t, "svg", string(SVG))
}
