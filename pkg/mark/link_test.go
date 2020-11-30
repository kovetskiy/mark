package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkFind(t *testing.T) {
	markdown := `
	[example1](../path/to/example.md#second-heading)
	[example2](../path/to/example.md)
	[example3](#heading-in-document)
	[Text link that should be put as attachment](../path/to/example.txt)
	[Image link that should be put as attachment](../path/to/example.png)
	`

	links := collectLinksFromMarkdown(markdown)

	assert.Equal(t, "../path/to/example.md#second-heading", links[0][1])
	assert.Equal(t, "../path/to/example.md", links[0][2])
	assert.Equal(t, "second-heading", links[0][3])

	assert.Equal(t, "../path/to/example.md", links[1][1])
	assert.Equal(t, "../path/to/example.md", links[1][2])
	assert.Equal(t, "", links[1][3])

	assert.Equal(t, "#heading-in-document", links[2][1])
	assert.Equal(t, "", links[2][2])
	assert.Equal(t, "heading-in-document", links[2][3])

	assert.Equal(t, len(links), 5)
}
