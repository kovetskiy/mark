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

// TestIncludeAmongHeadersStaysInBody pins the fix for an Include written inside
// the header block. Includes are expanded later, from the body, but the header
// run was cut out in one piece -- so the directive was deleted before anything
// could expand it and the page went up missing a whole section, with a zero
// exit code.
func TestIncludeAmongHeadersStaysInBody(t *testing.T) {
	meta, body, err := extract(t, `<!-- Space: TEST -->
<!-- Title: With Include -->
<!-- Include: fragment.md -->
<!-- Label: keep -->

Body.
`)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "With Include", meta.Title)
	assert.Equal(t, []string{"keep"}, meta.Labels, "headers after the Include still apply")
	assert.Contains(t, body, "<!-- Include: fragment.md -->",
		"the Include directive has to reach the expander")
	assert.NotContains(t, body, "<!-- Title:")
	assert.NotContains(t, body, "<!-- Label:")
	assert.Contains(t, body, "Body.")
}

// TestIncludeAmongHeadersAfterBlankLine covers the two header-run fixes
// together: the gap must not end the run, and the Include below it must still
// reach the body.
func TestIncludeAmongHeadersAfterBlankLine(t *testing.T) {
	meta, body, err := extract(t, `<!-- Space: TEST -->
<!-- Title: Grouped Include -->

<!-- Include: fragment.md -->

Body.
`)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "Grouped Include", meta.Title)
	assert.Contains(t, body, "<!-- Include: fragment.md -->")
}

// TestUnknownHeaderIsRefused pins the fix for a misspelled header. The line
// sits inside the header run, so it is taken out of the body whatever happens
// next; logging the typo and publishing anyway meant a page silently lost its
// title and the run still exited zero.
func TestUnknownHeaderIsRefused(t *testing.T) {
	_, _, err := extract(t, `<!-- Space: TEST -->
<!-- Titel: Typo -->

Body.
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown header "Titel"`)
	assert.Contains(t, err.Error(), HeaderTitle,
		"the message has to name the header that was probably meant")
}

// TestUnknownHeaderBeforeAnyKnownOneIsRefused covers the same typo written as
// the document's very first line, where nothing has been read as a header yet.
func TestUnknownHeaderBeforeAnyKnownOneIsRefused(t *testing.T) {
	_, _, err := extract(t, `<!-- Titel: Typo -->
<!-- Space: TEST -->

Body.
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown header "Titel"`)
}

// TestCommentBelowHeadersIsNotAHeader guards the other side of the previous
// test: a comment that is not part of the header run is page content, and
// refusing those would fail documents that merely mention a colon.
func TestCommentBelowHeadersIsNotAHeader(t *testing.T) {
	meta, body, err := extract(t, `<!-- Space: TEST -->
<!-- Title: Commented -->

Body.

<!-- note: generated file, do not edit -->
`)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Contains(t, body, "<!-- note: generated file, do not edit -->")
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
