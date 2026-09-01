package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extract runs ExtractMeta the way ProcessFile does, with every optional
// behaviour off, so that a test says only what the headers said.
func extract(t *testing.T, document string) (*Meta, string, error) {
	t.Helper()
	meta, body, err := ExtractMeta([]byte(document), "", false, false, "", nil, false, "", false)
	if err != nil {
		return nil, "", err
	}

	return meta, string(body), nil
}

// TestHeadersSurviveBlankLines pins the class of bug where a document that
// groups its headers loses every one below the first gap. Each comment is its
// own HTML block and the run used to end at the first byte that was not the
// previous block's last, so a blank line meant the Label was neither applied
// nor removed: it did nothing and was published as page text.
func TestHeadersSurviveBlankLines(t *testing.T) {
	meta, body, err := extract(t, `<!-- Space: TEST -->
<!-- Title: Grouped -->

<!-- Label: important -->

Body.
`)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "TEST", meta.Space)
	assert.Equal(t, "Grouped", meta.Title)
	assert.Equal(t, []string{"important"}, meta.Labels)
	assert.NotContains(t, body, "<!-- Label:", "the header must not be left in the page")
	assert.Contains(t, body, "Body.")
}

// TestHeadersStopAtRealContent pins the boundary the blank-line tolerance must
// not erase: a comment run separated from the headers by actual text is not
// metadata, and the text between them has to survive into the page.
func TestHeadersStopAtRealContent(t *testing.T) {
	meta, body, err := extract(t, `<!-- Space: TEST -->
<!-- Title: Split -->

Some prose.

<!-- Label: stranded -->
`)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Empty(t, meta.Labels, "a header below the page's own text is not metadata")
	assert.Contains(t, body, "Some prose.")
	assert.Contains(t, body, "<!-- Label: stranded -->",
		"a comment mark did not consume has to stay where the author put it")
}

// TestMacroAboveHeadersIsKept pins the reason the cut has two boundaries rather
// than one: whatever a document opens with before its headers -- a Macro
// definition is the usual case -- is not metadata and must reach the page.
func TestMacroAboveHeadersIsKept(t *testing.T) {
	meta, body, err := extract(t, `<macro>
raw
</macro>

<!-- Space: TEST -->
<!-- Title: After Macro -->

Body.
`)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "After Macro", meta.Title)
	assert.Contains(t, body, "<macro>")
	assert.NotContains(t, body, "<!-- Title:")
}
