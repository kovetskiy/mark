package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
)

// makeComments builds an InlineComments value from alternating
// (selection, markerRef) pairs, all with location "inline".
func makeComments(pairs ...string) *confluence.InlineComments {
	c := &confluence.InlineComments{}
	for i := 0; i+1 < len(pairs); i += 2 {
		selection, ref := pairs[i], pairs[i+1]
		c.Results = append(c.Results, struct {
			Extensions struct {
				Location         string `json:"location"`
				InlineProperties struct {
					OriginalSelection string `json:"originalSelection"`
					MarkerRef         string `json:"markerRef"`
				} `json:"inlineProperties"`
			} `json:"extensions"`
		}{})
		c.Results[len(c.Results)-1].Extensions.Location = "inline"
		c.Results[len(c.Results)-1].Extensions.InlineProperties.OriginalSelection = selection
		c.Results[len(c.Results)-1].Extensions.InlineProperties.MarkerRef = ref
	}
	return c
}

func TestMergeComments(t *testing.T) {
	body := "<p>Hello world</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-123">world</ac:inline-comment-marker></p>`
	comments := makeComments("world", "uuid-123")

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-123">world</ac:inline-comment-marker></p>`, result)
}

func TestMergeComments_Escaping(t *testing.T) {
	body := "<p>Hello &amp; world</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-456">&amp;</ac:inline-comment-marker> world</p>`
	comments := makeComments("&", "uuid-456")

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-456">&amp;</ac:inline-comment-marker> world</p>`, result)
}

func TestMergeComments_Disambiguation(t *testing.T) {
	body := "<p>Item one. Item two. Item one.</p>"
	// Comment is on the second "Item one."
	oldBody := `<p>Item one. Item two. <ac:inline-comment-marker ac:ref="uuid-1">Item one.</ac:inline-comment-marker></p>`
	comments := makeComments("Item one.", "uuid-1")

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	// Context should correctly pick the second occurrence
	assert.Equal(t, `<p>Item one. Item two. <ac:inline-comment-marker ac:ref="uuid-1">Item one.</ac:inline-comment-marker></p>`, result)
}

// TestMergeComments_SelectionMissing verifies that a comment whose selection
// no longer appears in the new body is silently dropped without panicking.
func TestMergeComments_SelectionMissing(t *testing.T) {
	body := "<p>Completely different content</p>"
	oldBody := `<p><ac:inline-comment-marker ac:ref="uuid-gone">old text</ac:inline-comment-marker></p>`
	comments := makeComments("old text", "uuid-gone")

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	// Comment is dropped; body is returned unchanged.
	assert.Equal(t, body, result)
}

// TestMergeComments_OverlappingSelections verifies that when two comments
// reference overlapping text regions the later one (by position) is kept and
// the earlier overlapping one is dropped rather than corrupting the body.
func TestMergeComments_OverlappingSelections(t *testing.T) {
	body := "<p>foo bar baz</p>"
	// Neither comment has a marker in oldBody, so no positional context is
	// available; the algorithm falls back to a plain string search.
	oldBody := "<p>foo bar baz</p>"
	// "foo bar" starts at 3, ends at 10; "bar baz" starts at 7, ends at 14.
	// They overlap on "bar".  The later match (uuid-B at position 7) wins.
	comments := makeComments("foo bar", "uuid-A", "bar baz", "uuid-B")

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>foo <ac:inline-comment-marker ac:ref="uuid-B">bar baz</ac:inline-comment-marker></p>`, result)
}

// TestMergeComments_NilComments verifies that a nil comments pointer is
// handled gracefully and the new body is returned unchanged.
func TestMergeComments_NilComments(t *testing.T) {
	body := "<p>Hello world</p>"
	result, err := MergeComments(body, "", nil)
	assert.NoError(t, err)
	assert.Equal(t, body, result)
}

// TestMergeComments_HTMLEntities verifies that selections containing HTML
// entities beyond &amp; (&lt;, &gt;, &#39;) are matched correctly.
func TestMergeComments_HTMLEntities(t *testing.T) {
	body := `<p>Hello &lt;world&gt; it&#39;s me</p>`
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-ent">&lt;world&gt;</ac:inline-comment-marker> it&#39;s me</p>`
	// The API returns the raw (unescaped) selection text.
	comments := makeComments("<world>", "uuid-ent")

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-ent">&lt;world&gt;</ac:inline-comment-marker> it&#39;s me</p>`, result)
}

// TestMergeComments_NestedTags verifies that a marker whose stored content
// contains nested inline tags (e.g. <strong>) is still recognised by
// markerRegex and the comment is correctly relocated into the new body.
func TestMergeComments_NestedTags(t *testing.T) {
	// The new body contains plain bold text (no marker yet).
	body := "<p>Hello <strong>world</strong></p>"
	// The old body already has the marker wrapping the bold tag.
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-nested"><strong>world</strong></ac:inline-comment-marker></p>`
	// The API returns the raw selected text without markup.
	comments := makeComments("world", "uuid-nested")

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <strong><ac:inline-comment-marker ac:ref="uuid-nested">world</ac:inline-comment-marker></strong></p>`, result)
}

// TestMergeComments_EmptySelection verifies that a comment with an empty
// OriginalSelection is skipped without panicking and the body is returned
// unchanged.
func TestMergeComments_EmptySelection(t *testing.T) {
	body := "<p>Hello world</p>"
	comments := makeComments("", "uuid-empty")

	result, err := MergeComments(body, body, comments)
	assert.NoError(t, err)
	assert.Equal(t, body, result)
}
