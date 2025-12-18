package page

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLinks(t *testing.T) {
	markdown := `
	[example1](../path/to/example.md#second-heading)
	[example2](../path/to/example.md)
	[example3](#heading-in-document)
	[Text link that should be put as attachment](../path/to/example.txt)
	[Image link that should be put as attachment](../path/to/example.png)
	[relative link without dots](relative-link-without-dots.md)
	[relative link without dots but with hash](relative-link-without-dots-but-with-hash.md#hash)
	[example [example]](example.md)
	`

	links := parseLinks(markdown)

	assert.Equal(t, "../path/to/example.md#second-heading", links[0].full)
	assert.Equal(t, "../path/to/example.md", links[0].filename)
	assert.Equal(t, "second-heading", links[0].hash)

	assert.Equal(t, "../path/to/example.md", links[1].full)
	assert.Equal(t, "../path/to/example.md", links[1].filename)
	assert.Equal(t, "", links[1].hash)

	assert.Equal(t, "#heading-in-document", links[2].full)
	assert.Equal(t, "", links[2].filename)
	assert.Equal(t, "heading-in-document", links[2].hash)

	assert.Equal(t, "../path/to/example.txt", links[3].full)
	assert.Equal(t, "../path/to/example.txt", links[3].filename)
	assert.Equal(t, "", links[3].hash)

	assert.Equal(t, "../path/to/example.png", links[4].full)
	assert.Equal(t, "../path/to/example.png", links[4].filename)
	assert.Equal(t, "", links[4].hash)

	assert.Equal(t, "relative-link-without-dots.md", links[5].full)
	assert.Equal(t, "relative-link-without-dots.md", links[5].filename)
	assert.Equal(t, "", links[5].hash)

	assert.Equal(t, "relative-link-without-dots-but-with-hash.md#hash", links[6].full)
	assert.Equal(t, "relative-link-without-dots-but-with-hash.md", links[6].filename)
	assert.Equal(t, "hash", links[6].hash)

	assert.Equal(t, "example.md", links[7].full)
	assert.Equal(t, len(links), 8)
}

func TestNormalizeConfluenceWebUIPath(t *testing.T) {
	t.Run("confluence-cloud-experience-prefix", func(t *testing.T) {
		input := "/ex/confluence/05594958-6d5d-4e00-9017-90926d8b82d5/wiki/spaces/DVT/pages/5645697027/DX"
		expected := "/wiki/spaces/DVT/pages/5645697027/DX"
		assert.Equal(t, expected, normalizeConfluenceWebUIPath(input))
	})

	t.Run("already-canonical-wiki", func(t *testing.T) {
		input := "/wiki/spaces/DVT/pages/5645697027/DX"
		assert.Equal(t, input, normalizeConfluenceWebUIPath(input))
	})

	t.Run("local-relative-path-unchanged", func(t *testing.T) {
		input := "./img/some-nice-image.png"
		assert.Equal(t, input, normalizeConfluenceWebUIPath(input))
	})
}
