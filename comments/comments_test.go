package comments

import (
	"html"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Helper function unit tests
// ---------------------------------------------------------------------------

func TestTruncateSelection(t *testing.T) {
	assert.Equal(t, "hello", truncateSelection("hello", 10))
	assert.Equal(t, "hello", truncateSelection("hello", 5))
	assert.Equal(t, "hell…", truncateSelection("hello", 4))
	assert.Equal(t, "", truncateSelection("", 5))
	// Multibyte runes count as single units.
	assert.Equal(t, "世界…", truncateSelection("世界 is the world", 2))
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		s1, s2 string
		want   int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "axc", 1}, // one substitution
		{"abc", "ab", 1},  // one deletion
		{"ab", "abc", 1},  // one insertion
		{"kitten", "sitting", 3},
		// Multibyte: é is one rune, so distance from "héllo" to "hello" is 1.
		{"héllo", "hello", 1},
	}
	for _, tt := range tests {
		t.Run(tt.s1+"/"+tt.s2, func(t *testing.T) {
			assert.Equal(t, tt.want, levenshteinDistance(tt.s1, tt.s2))
		})
	}
}

func TestContextBefore(t *testing.T) {
	// Basic cases.
	assert.Equal(t, "", contextBefore("hello", 0, 10))
	assert.Equal(t, "hello", contextBefore("hello", 5, 10))
	assert.Equal(t, "llo", contextBefore("hello", 5, 3))

	// "héllo" is 6 bytes (h=1, é=2, l=1, l=1, o=1).
	// maxBytes=4 → raw start=2, which lands mid-rune (é's continuation byte).
	// Should advance to byte 3 (first 'l').
	assert.Equal(t, "llo", contextBefore("héllo", 6, 4))
}

func TestContextAfter(t *testing.T) {
	// Basic cases.
	assert.Equal(t, "", contextAfter("hello", 5, 10))
	assert.Equal(t, "hello", contextAfter("hello", 0, 10))
	assert.Equal(t, "hel", contextAfter("hello", 0, 3))

	// "héllo" is 6 bytes. contextAfter(s, 0, 2) → raw end=2 (é's continuation
	// byte), which is not a rune start. Should back up to 1, returning just "h".
	assert.Equal(t, "h", contextAfter("héllo", 0, 2))
}

// makeComments builds an InlineComments value from alternating
// (selection, markerRef) pairs, all with location "inline".
func makeComments(pairs ...string) *confluence.InlineComments {
	c := &confluence.InlineComments{}
	for i := 0; i+1 < len(pairs); i += 2 {
		selection, ref := pairs[i], pairs[i+1]
		c.Results = append(c.Results, confluence.InlineCommentResult{
			Extensions: confluence.InlineCommentExtensions{
				Location: "inline",
				InlineProperties: confluence.InlineCommentProperties{
					OriginalSelection: selection,
					MarkerRef:         ref,
				},
			},
		})
	}
	return c
}

func TestMergeComments(t *testing.T) {
	body := "<p>Hello world</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-123">world</ac:inline-comment-marker></p>`
	comments := makeComments("world", "uuid-123")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-123">world</ac:inline-comment-marker></p>`, result)
}

func TestMergeComments_Escaping(t *testing.T) {
	body := "<p>Hello &amp; world</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-456">&amp;</ac:inline-comment-marker> world</p>`
	comments := makeComments("&", "uuid-456")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-456">&amp;</ac:inline-comment-marker> world</p>`, result)
}

func TestMergeComments_Disambiguation(t *testing.T) {
	body := "<p>Item one. Item two. Item one.</p>"
	// Comment is on the second "Item one."
	oldBody := `<p>Item one. Item two. <ac:inline-comment-marker ac:ref="uuid-1">Item one.</ac:inline-comment-marker></p>`
	comments := makeComments("Item one.", "uuid-1")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	// Context should correctly pick the second occurrence
	assert.Equal(t, `<p>Item one. Item two. <ac:inline-comment-marker ac:ref="uuid-1">Item one.</ac:inline-comment-marker></p>`, result)
}

// TestMergeComments_SelectionMissing verifies that a comment whose selection
// no longer appears in the new body is dropped without returning an error or panicking.
// A warning is logged so the user knows the comment was not relocated.
func TestMergeComments_SelectionMissing(t *testing.T) {
	body := "<p>Completely different content</p>"
	oldBody := `<p><ac:inline-comment-marker ac:ref="uuid-gone">old text</ac:inline-comment-marker></p>`
	comments := makeComments("old text", "uuid-gone")

	result, err := Merge(body, oldBody, comments)
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

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>foo <ac:inline-comment-marker ac:ref="uuid-B">bar baz</ac:inline-comment-marker></p>`, result)
}

// TestMergeComments_NilComments verifies that a nil comments pointer is
// handled gracefully and the new body is returned unchanged.
func TestMergeComments_NilComments(t *testing.T) {
	body := "<p>Hello world</p>"
	result, err := Merge(body, "", nil)
	assert.NoError(t, err)
	assert.Equal(t, body, result)
}

// TestMergeComments_HTMLEntities verifies that selections containing HTML
// entities (&lt;, &gt;) are matched correctly. The API returns raw (unescaped)
// text for OriginalSelection, which is matched against the body's text with its
// references resolved.
func TestMergeComments_HTMLEntities(t *testing.T) {
	body := `<p>Hello &lt;world&gt; it&#39;s me</p>`
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-ent">&lt;world&gt;</ac:inline-comment-marker> it&#39;s me</p>`
	// The API returns the raw (unescaped) selection text.
	comments := makeComments("<world>", "uuid-ent")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-ent">&lt;world&gt;</ac:inline-comment-marker> it&#39;s me</p>`, result)
}

// TestMergeComments_ApostropheEncoded verifies that a selection including an
// apostrophe is found when the new body spells it as the numeric reference
// &#39;, as mark's xmlesc template function does.
func TestMergeComments_ApostropheEncoded(t *testing.T) {
	body := `<p>Hello &lt;world&gt; it&#39;s me</p>`
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-apos-enc">&lt;world&gt; it&#39;s</ac:inline-comment-marker> me</p>`
	// The API returns the raw unescaped selection including a literal apostrophe.
	comments := makeComments("<world> it's", "uuid-apos-enc")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-apos-enc">&lt;world&gt; it&#39;s</ac:inline-comment-marker> me</p>`, result)
}

// TestMergeComments_ReferenceSpellings verifies that a selection matches
// whichever way the new body spells its characters: raw, as a named
// reference, or as a decimal or hexadecimal numeric one.
func TestMergeComments_ReferenceSpellings(t *testing.T) {
	for _, quoted := range []string{`"a&amp;b"`, `&quot;a&#38;b&#34;`, `&#x22;a&#x26;b&quot;`} {
		t.Run(quoted, func(t *testing.T) {
			body := `<p>say ` + quoted + ` now</p>`

			result, err := Merge(body, body, makeComments(`"a&b"`, "uuid-spell"))
			assert.NoError(t, err)
			assert.Equal(t, `<p>say <ac:inline-comment-marker ac:ref="uuid-spell">`+quoted+`</ac:inline-comment-marker> now</p>`, result)
		})
	}
}

// TestMergeComments_SelectionInsideReference verifies that a selection which
// would split a character reference, such as "amp" inside "&amp;", is not
// wrapped in a marker.
func TestMergeComments_SelectionInsideReference(t *testing.T) {
	body := `<p>this &amp; that</p>`

	result, err := Merge(body, body, makeComments("amp", "uuid-split"))
	assert.NoError(t, err)
	assert.Equal(t, body, result)
}

// TestMergeComments_QuotedSelectionInCompiledMarkdown verifies that a comment
// on text with double quotes survives a republish of the unchanged document:
// goldmark writes those quotes as &quot;, while the selection Confluence
// reports holds them as they are.
func TestMergeComments_QuotedSelectionInCompiledMarkdown(t *testing.T) {
	std, err := stdlib.New(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := markdown.CompileMarkdown(t.Context(), []byte("He said \"hello world\" today, it's fine.\n"), std, "page.md", markdown.Options{})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "<p>He said &quot;hello world&quot; today, it's fine.</p>\n", body)
	oldBody := `<p>He said <ac:inline-comment-marker ac:ref="uuid-quoted">"hello world"</ac:inline-comment-marker> today, it's fine.</p>`

	result, err := Merge(body, oldBody, makeComments(`"hello world"`, "uuid-quoted"))
	assert.NoError(t, err)
	assert.Equal(t, `<p>He said <ac:inline-comment-marker ac:ref="uuid-quoted">&quot;hello world&quot;</ac:inline-comment-marker> today, it's fine.</p>`+"\n", result)
	assert.NoError(t, markdown.CheckWellFormed(result))
}

// TestMergeComments_ApostropheSelection verifies that a selection containing a
// literal apostrophe is found when the new body also contains a literal
// apostrophe, as goldmark writes it.
func TestMergeComments_ApostropheSelection(t *testing.T) {
	body := `<p>Hello it's a test</p>`
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-apos">it's</ac:inline-comment-marker> a test</p>`
	// The API returns the raw (unescaped) selection text with a literal apostrophe.
	comments := makeComments("it's", "uuid-apos")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-apos">it's</ac:inline-comment-marker> a test</p>`, result)
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

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <strong><ac:inline-comment-marker ac:ref="uuid-nested">world</ac:inline-comment-marker></strong></p>`, result)
}

// TestMergeComments_EmptySelection verifies that a comment with an empty
// OriginalSelection is skipped without panicking and the body is returned
// unchanged.
func TestMergeComments_EmptySelection(t *testing.T) {
	body := "<p>Hello world</p>"
	comments := makeComments("", "uuid-empty")

	result, err := Merge(body, body, comments)
	assert.NoError(t, err)
	assert.Equal(t, body, result)
}

// TestMergeComments_DuplicateMarkerRef verifies that multiple comment results
// sharing the same MarkerRef (e.g. threaded replies) produce exactly one
// <ac:inline-comment-marker> insertion rather than nested duplicates.
func TestMergeComments_DuplicateMarkerRef(t *testing.T) {
	body := "<p>Hello world</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-dup">world</ac:inline-comment-marker></p>`
	// Two results with identical ref — simulates threaded replies.
	comments := makeComments("world", "uuid-dup", "world", "uuid-dup")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-dup">world</ac:inline-comment-marker></p>`, result)
}

// ---------------------------------------------------------------------------
// Additional Merge scenario tests
// ---------------------------------------------------------------------------

// TestMergeComments_MultipleComments verifies that two non-overlapping comments
// are both correctly re-embedded via back-to-front replacement.
func TestMergeComments_MultipleComments(t *testing.T) {
	body := "<p>Hello world and foo bar</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-1">world</ac:inline-comment-marker> and foo <ac:inline-comment-marker ac:ref="uuid-2">bar</ac:inline-comment-marker></p>`
	comments := makeComments("world", "uuid-1", "bar", "uuid-2")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-1">world</ac:inline-comment-marker> and foo <ac:inline-comment-marker ac:ref="uuid-2">bar</ac:inline-comment-marker></p>`, result)
}

// TestMergeComments_EmptyResults verifies that an InlineComments value with a
// non-nil but empty Results slice is handled gracefully.
func TestMergeComments_EmptyResults(t *testing.T) {
	body := "<p>Hello world</p>"
	result, err := Merge(body, body, &confluence.InlineComments{})
	assert.NoError(t, err)
	assert.Equal(t, body, result)
}

// TestMergeComments_NonInlineLocation verifies that page-level comments
// (location != "inline") are silently skipped and the body is unchanged.
func TestMergeComments_NonInlineLocation(t *testing.T) {
	body := "<p>Hello world</p>"
	comments := &confluence.InlineComments{
		Results: []confluence.InlineCommentResult{
			{
				Extensions: confluence.InlineCommentExtensions{
					Location: "page",
					InlineProperties: confluence.InlineCommentProperties{
						OriginalSelection: "Hello",
						MarkerRef:         "uuid-page",
					},
				},
			},
		},
	}
	result, err := Merge(body, body, comments)
	assert.NoError(t, err)
	assert.Equal(t, body, result)
}

// TestMergeComments_NoContext verifies that when a comment's MarkerRef has no
// corresponding marker in oldBody (no context available) the first occurrence
// of the selection in the new body is used.
func TestMergeComments_NoContext(t *testing.T) {
	body := "<p>foo bar foo</p>"
	oldBody := "<p>foo bar foo</p>" // no markers → no context
	comments := makeComments("foo", "uuid-noctx")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	// First occurrence of "foo" is at position 3.
	assert.Equal(t, `<p><ac:inline-comment-marker ac:ref="uuid-noctx">foo</ac:inline-comment-marker> bar foo</p>`, result)
}

// TestMergeComments_UTF8 verifies that selections and bodies containing
// multibyte UTF-8 characters are handled correctly.
func TestMergeComments_UTF8(t *testing.T) {
	body := "<p>こんにちは世界</p>"
	oldBody := `<p>こんにちは<ac:inline-comment-marker ac:ref="uuid-jp">世界</ac:inline-comment-marker></p>`
	comments := makeComments("世界", "uuid-jp")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>こんにちは<ac:inline-comment-marker ac:ref="uuid-jp">世界</ac:inline-comment-marker></p>`, result)
}

// TestMergeComments_SelectionWithQuotes verifies that a selection containing
// apostrophes or double-quotes is found correctly in the new body even though
// html.EscapeString would encode those characters. Only &, <, > should be
// escaped when searching.
func TestMergeComments_SelectionWithQuotes(t *testing.T) {
	body := `<p>It's a "test" page</p>`
	oldBody := `<p>It's a <ac:inline-comment-marker ac:ref="uuid-q">"test"</ac:inline-comment-marker> page</p>`
	comments := makeComments(`"test"`, "uuid-q")

	result, err := Merge(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>It's a <ac:inline-comment-marker ac:ref="uuid-q">"test"</ac:inline-comment-marker> page</p>`, result)
}

// TestMergeComments_DuplicateMarkerRefDropped verifies that when multiple
// comment results share the same MarkerRef and the selection cannot be found,
// only a single warning is emitted (not one per result).
func TestMergeComments_DuplicateMarkerRefDropped(t *testing.T) {
	body := "<p>Hello world</p>"
	// Duplicate refs, but selection "gone" is not present in body or oldBody.
	comments := makeComments("gone", "uuid-dup2", "gone", "uuid-dup2")

	result, err := Merge(body, body, comments)
	assert.NoError(t, err)
	assert.Equal(t, body, result) // body unchanged, single warning logged
}

// TestMergeComments_SelectionOnlyInMarkup verifies that a selection found
// only in markup -- an attribute value, a tag name, a CDATA section, a macro
// parameter, an XML comment -- is dropped rather than wrapped in a marker,
// which would leave XML Confluence refuses or a marker shown as literal text.
func TestMergeComments_SelectionOnlyInMarkup(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		selection string
	}{
		{
			name:      "macro name",
			body:      `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>a</p></ac:rich-text-body></ac:structured-macro>`,
			selection: "info",
		},
		{
			name:      "href",
			body:      `<p>go to <a href="https://example.com/setup">the guide</a></p>`,
			selection: "setup",
		},
		{
			name:      "attribute value holding a closing angle bracket",
			body:      `<p title="a > setup">guide</p>`,
			selection: "setup",
		},
		{
			name:      "tag name",
			body:      `<p><code>x</code></p>`,
			selection: "code",
		},
		{
			name:      "CDATA",
			body:      `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[func foo() { return <nil> }]]></ac:plain-text-body></ac:structured-macro>`,
			selection: "<nil>",
		},
		{
			name:      "macro parameter",
			body:      `<ac:structured-macro ac:name="info"><ac:parameter ac:name="title">Setup</ac:parameter><ac:rich-text-body><p>a</p></ac:rich-text-body></ac:structured-macro>`,
			selection: "Setup",
		},
		{
			name:      "default parameter",
			body:      `<ac:structured-macro ac:name="excerpt-include"><ac:default-parameter>Release Notes</ac:default-parameter></ac:structured-macro>`,
			selection: "Release Notes",
		},
		{
			name:      "placeholder",
			body:      `<ac:layout><ac:layout-section ac:type="single"><ac:layout-cell><ac:placeholder>Add notes here</ac:placeholder></ac:layout-cell></ac:layout-section></ac:layout>`,
			selection: "notes",
		},
		{
			name:      "task status",
			body:      "<ac:task-list>\n<ac:task>\n<ac:task-id>1</ac:task-id>\n<ac:task-status>incomplete</ac:task-status>\n<ac:task-body>a</ac:task-body>\n</ac:task>\n</ac:task-list>",
			selection: "complete",
		},
		{
			name:      "task id",
			body:      "<ac:task-list>\n<ac:task>\n<ac:task-id>1</ac:task-id>\n<ac:task-status>incomplete</ac:task-status>\n<ac:task-body>a</ac:task-body>\n</ac:task>\n</ac:task-list>",
			selection: "1",
		},
		{
			name:      "XML comment",
			body:      `<!-- setup --><p>a</p>`,
			selection: "setup",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldBody := `<p>Run <ac:inline-comment-marker ac:ref="uuid-m">` + html.EscapeString(tt.selection) + `</ac:inline-comment-marker> now</p>`

			result, err := Merge(tt.body, oldBody, makeComments(tt.selection, "uuid-m"))
			assert.NoError(t, err)
			assert.Equal(t, tt.body, result)
			assert.NoError(t, markdown.CheckWellFormed(result))
		})
	}
}

// TestMergeComments_SelectionInTextAndMarkup verifies that when a selection
// appears both in an attribute and in text, the marker goes around the text.
func TestMergeComments_SelectionInTextAndMarkup(t *testing.T) {
	body := `<p>Run <a href="https://example.com/setup">setup</a> now</p>`
	oldBody := `<p>Run <ac:inline-comment-marker ac:ref="uuid-t">setup</ac:inline-comment-marker> now</p>`

	result, err := Merge(body, oldBody, makeComments("setup", "uuid-t"))
	assert.NoError(t, err)
	assert.Equal(t, `<p>Run <a href="https://example.com/setup"><ac:inline-comment-marker ac:ref="uuid-t">setup</ac:inline-comment-marker></a> now</p>`, result)
	assert.NoError(t, markdown.CheckWellFormed(result))
}

// TestMergeReportsALostComment: a comment whose text is gone from the new body
// is said to be lost to the document's warnings, not only to the log.
func TestMergeReportsALostComment(t *testing.T) {
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-gone">world</ac:inline-comment-marker></p>`
	warnings := report.NewWarnings("docs/a.md")

	result, err := MergeWithWarnings(`<p>Goodbye</p>`, oldBody, makeComments("world", "uuid-gone"), warnings)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Goodbye</p>`, result)

	assert.Equal(t, []report.Warning{{
		Kind:    report.WarningInlineComment,
		File:    "docs/a.md",
		Message: `inline comment uuid-gone is lost: the text it was made on, "world", is not in the new body's page text`,
	}}, warnings.List())
}

const markerA = `<ac:inline-comment-marker ac:ref="uuid-a">`
const markerEnd = `</ac:inline-comment-marker>`

// TestMergeComments_SelectionAcrossInlineMarkup: a comment made on text that
// runs through inline markup is kept, with one marker for each run of text it
// covers and none of them across an element's boundary.
func TestMergeComments_SelectionAcrossInlineMarkup(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		selection string
		want      string
	}{
		{
			name:      "into bold",
			body:      `<p>hello <strong>world</strong> x</p>`,
			selection: "hello world",
			want:      `<p>` + markerA + `hello ` + markerEnd + `<strong>` + markerA + `world` + markerEnd + `</strong> x</p>`,
		},
		{
			name:      "out of bold, part of each run",
			body:      `<p>one <strong>two three</strong> four five</p>`,
			selection: "three four",
			want:      `<p>one <strong>two ` + markerA + `three` + markerEnd + `</strong>` + markerA + ` four` + markerEnd + ` five</p>`,
		},
		{
			name:      "through a link",
			body:      `<p>read <a href="https://example.com/guide">the guide</a> first</p>`,
			selection: "read the guide first",
			want:      `<p>` + markerA + `read ` + markerEnd + `<a href="https://example.com/guide">` + markerA + `the guide` + markerEnd + `</a>` + markerA + ` first` + markerEnd + `</p>`,
		},
		{
			name:      "through a code span",
			body:      `<p>run <code>make test</code> twice</p>`,
			selection: "run make test twice",
			want:      `<p>` + markerA + `run ` + markerEnd + `<code>` + markerA + `make test` + markerEnd + `</code>` + markerA + ` twice` + markerEnd + `</p>`,
		},
		{
			name:      "through nested markup",
			body:      `<p>a <em>b <strong>c</strong></em> d</p>`,
			selection: "b c d",
			want:      `<p>a <em>` + markerA + `b ` + markerEnd + `<strong>` + markerA + `c` + markerEnd + `</strong></em>` + markerA + ` d` + markerEnd + `</p>`,
		},
		{
			name:      "through a link with a rich body",
			body:      `<p>see <ac:link ac:anchor="notes"><ac:link-body>the notes</ac:link-body></ac:link> below</p>`,
			selection: "the notes below",
			want:      `<p>see <ac:link ac:anchor="notes"><ac:link-body>` + markerA + `the notes` + markerEnd + `</ac:link-body></ac:link>` + markerA + ` below` + markerEnd + `</p>`,
		},
		{
			name:      "past an emoticon",
			body:      `<p>well <ac:emoticon ac:name="smile"/> done</p>`,
			selection: "well done",
			want:      `<p>` + markerA + `well ` + markerEnd + `<ac:emoticon ac:name="smile"/>` + markerA + ` done` + markerEnd + `</p>`,
		},
		{
			name:      "space only between two elements",
			body:      `<p><strong>a</strong> <em>b</em></p>`,
			selection: "a b",
			want:      `<p><strong>` + markerA + `a` + markerEnd + `</strong> <em>` + markerA + `b` + markerEnd + `</em></p>`,
		},
		{
			name:      "references in both runs",
			body:      `<p>R&amp;D <strong>&lt;team&gt;</strong></p>`,
			selection: "R&D <team>",
			want:      `<p>` + markerA + `R&amp;D ` + markerEnd + `<strong>` + markerA + `&lt;team&gt;` + markerEnd + `</strong></p>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := report.NewWarnings("docs/a.md")

			result, err := MergeWithWarnings(tt.body, `<p>none</p>`, makeComments(tt.selection, "uuid-a"), warnings)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, result)
			assert.NoError(t, markdown.CheckWellFormed(result))
			assert.Empty(t, warnings.List())
		})
	}
}

// TestMergeComments_SelectionAcrossWhitespace: the selection is the text as
// the page shows it, where a line break in the source or a no-break space
// reads as one space.
func TestMergeComments_SelectionAcrossWhitespace(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		selection string
		want      string
	}{
		{
			name:      "soft line break",
			body:      "<p>foo\nbar baz</p>",
			selection: "foo bar",
			want:      "<p>" + markerA + "foo\nbar" + markerEnd + " baz</p>",
		},
		{
			name:      "no-break space reference",
			body:      `<p>10&nbsp;km away</p>`,
			selection: "10 km",
			want:      `<p>` + markerA + `10&nbsp;km` + markerEnd + ` away</p>`,
		},
		{
			name:      "no-break space in the selection",
			body:      `<p>10&#160;km away</p>`,
			selection: "10 km",
			want:      `<p>` + markerA + `10&#160;km` + markerEnd + ` away</p>`,
		},
		{
			name:      "space on both sides of an opening tag",
			body:      `<p>hello <strong> world</strong></p>`,
			selection: "hello world",
			want:      `<p>` + markerA + `hello ` + markerEnd + `<strong>` + markerA + ` world` + markerEnd + `</strong></p>`,
		},
		{
			name:      "indented source",
			body:      "<p>one\n   two</p>",
			selection: "one two",
			want:      "<p>" + markerA + "one\n   two" + markerEnd + "</p>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Merge(tt.body, `<p>none</p>`, makeComments(tt.selection, "uuid-a"))
			assert.NoError(t, err)
			assert.Equal(t, tt.want, result)
			assert.NoError(t, markdown.CheckWellFormed(result))
		})
	}
}

// TestMergeComments_SelectionAcrossBlocks: text in two blocks is not one
// stretch of text, and neither is text on either side of a macro, a CDATA
// section or a line break element, so a selection is never matched across
// them.
func TestMergeComments_SelectionAcrossBlocks(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "paragraphs", body: `<p>hello </p><p>world</p>`},
		{name: "table cells", body: `<table><tbody><tr><td>hello </td><td>world</td></tr></tbody></table>`},
		{name: "list items", body: `<ul><li>hello </li><li>world</li></ul>`},
		{name: "line break element", body: `<p>hello <br/>world</p>`},
		{name: "XML comment", body: `<p>hello <!-- note -->world</p>`},
		{
			name: "macro parameter",
			body: `<p>hello <ac:structured-macro ac:name="status"><ac:parameter ac:name="title">world</ac:parameter></ac:structured-macro></p>`,
		},
		{
			name: "plain link body",
			body: `<p>hello <ac:link><ri:page ri:content-title="Guide"/><ac:plain-text-link-body><![CDATA[world]]></ac:plain-text-link-body></ac:link></p>`,
		},
		{
			name: "macro between",
			body: `<p>hello <ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">top</ac:parameter></ac:structured-macro>world</p>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := report.NewWarnings("docs/a.md")

			result, err := MergeWithWarnings(tt.body, `<p>none</p>`, makeComments("hello world", "uuid-a"), warnings)
			assert.NoError(t, err)
			assert.Equal(t, tt.body, result)
			assert.Len(t, warnings.List(), 1)
		})
	}
}

// TestMergeComments_SplitMarkerKeepsItsPlace: the markers of one comment in
// the old body are read as one stretch, so the comment goes back where it was
// when the same text appears twice.
func TestMergeComments_SplitMarkerKeepsItsPlace(t *testing.T) {
	body := `<p>hello <strong>world</strong> first</p><p>hello <strong>world</strong> second</p>`
	want := `<p>hello <strong>world</strong> first</p><p>` + markerA + `hello ` + markerEnd + `<strong>` + markerA + `world` + markerEnd + `</strong> second</p>`

	result, err := Merge(body, want, makeComments("hello world", "uuid-a"))
	assert.NoError(t, err)
	assert.Equal(t, want, result)

	// What mark wrote is what it reads on the next run.
	again, err := Merge(body, result, makeComments("hello world", "uuid-a"))
	assert.NoError(t, err)
	assert.Equal(t, want, again)
}

// TestMergeComments_OverlapAcrossInlineMarkup: a comment whose text overlaps
// one that runs through inline markup is still set aside, not nested in it.
func TestMergeComments_OverlapAcrossInlineMarkup(t *testing.T) {
	body := `<p>hello <strong>world</strong> x</p>`
	warnings := report.NewWarnings("docs/a.md")

	result, err := MergeWithWarnings(body, `<p>none</p>`, makeComments("hello world", "uuid-a", "world x", "uuid-b"), warnings)
	assert.NoError(t, err)
	assert.Equal(t,
		`<p>hello <strong><ac:inline-comment-marker ac:ref="uuid-b">world`+markerEnd+`</strong><ac:inline-comment-marker ac:ref="uuid-b"> x`+markerEnd+`</p>`,
		result)
	assert.NoError(t, markdown.CheckWellFormed(result))
	assert.Len(t, warnings.List(), 1)
}

// TestMergeComments_InlineMarkupInCompiledMarkdown: a comment on a sentence
// with emphasis, a link, a code span and a line break in its source survives
// a republish of the unchanged document.
func TestMergeComments_InlineMarkupInCompiledMarkdown(t *testing.T) {
	std, err := stdlib.New(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	source := "Run **make test** before\nyou open [a pull request](https://example.com/pulls) with `git push`.\n"
	body, _, err := markdown.CompileMarkdown(t.Context(), []byte(source), std, "page.md", markdown.Options{})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t,
		"<p>Run <strong>make test</strong> before\nyou open <a href=\"https://example.com/pulls\">a pull request</a> with <code>git push</code>.</p>\n",
		body)
	warnings := report.NewWarnings("page.md")

	result, err := MergeWithWarnings(body, `<p>none</p>`,
		makeComments("make test before you open a pull request with git", "uuid-a"), warnings)
	assert.NoError(t, err)
	assert.Equal(t,
		"<p>Run <strong>"+markerA+"make test"+markerEnd+"</strong>"+markerA+" before\nyou open "+markerEnd+
			"<a href=\"https://example.com/pulls\">"+markerA+"a pull request"+markerEnd+"</a>"+markerA+" with "+markerEnd+
			"<code>"+markerA+"git"+markerEnd+" push</code>.</p>\n",
		result)
	assert.NoError(t, markdown.CheckWellFormed(result))
	assert.Empty(t, warnings.List())
}
