package mark

import (
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ProcessD2/ProcessMermaid fall back to the content checksum when the author
// supplies no title, so the attachment gets a stable content-derived filename.
// That value was also passed to the ac:image template's Title field, and
// Confluence renders ac:title as a caption -- so an untitled diagram published
// with a 64-character hash printed underneath it.
func TestDiagramWithoutTitleHasNoCaption(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, attachments, err := CompileMarkdown(
		[]byte("```d2\na -> b\n```\n"),
		std, "test.md", types.MarkConfig{Features: []string{"d2"}, D2Scale: 1.0},
	)
	require.NoError(t, err)

	assert.NotContains(t, out, "ac:title=",
		"an untitled diagram must not render a caption")
	assert.Contains(t, out, "<ac:image", "the image itself must still be emitted")

	// The attachment keeps its checksum-derived name: that is what makes
	// re-publishing idempotent, and it must not change.
	require.Len(t, attachments, 1)
	assert.True(t, strings.HasSuffix(attachments[0].Filename, ".png"))
	assert.NotEmpty(t, attachments[0].Checksum)
	assert.Equal(t, attachments[0].Checksum+".png", attachments[0].Filename,
		"the filename must still be derived from the checksum")
}

// An explicit "title" in the info string is still shown.
func TestDiagramWithTitleKeepsCaption(t *testing.T) {
	std, err := stdlib.New(nil)
	require.NoError(t, err)

	out, attachments, err := CompileMarkdown(
		[]byte("```d2 title My Diagram\na -> b\n```\n"),
		std, "test.md", types.MarkConfig{Features: []string{"d2"}, D2Scale: 1.0},
	)
	require.NoError(t, err)

	assert.Contains(t, out, `ac:title="My Diagram"`)
	require.Len(t, attachments, 1)
	assert.Equal(t, "My Diagram.png", attachments[0].Filename)
}
