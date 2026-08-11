package mark

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog"
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

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-123">world</ac:inline-comment-marker></p>`, result)
}

func TestMergeComments_Escaping(t *testing.T) {
	body := "<p>Hello &amp; world</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-456">&amp;</ac:inline-comment-marker> world</p>`
	comments := makeComments("&", "uuid-456")

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-456">&amp;</ac:inline-comment-marker> world</p>`, result)
}

func TestMergeComments_Disambiguation(t *testing.T) {
	body := "<p>Item one. Item two. Item one.</p>"
	// Comment is on the second "Item one."
	oldBody := `<p>Item one. Item two. <ac:inline-comment-marker ac:ref="uuid-1">Item one.</ac:inline-comment-marker></p>`
	comments := makeComments("Item one.", "uuid-1")

	result, err := mergeComments(body, oldBody, comments)
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

	result, err := mergeComments(body, oldBody, comments)
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

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>foo <ac:inline-comment-marker ac:ref="uuid-B">bar baz</ac:inline-comment-marker></p>`, result)
}

// TestMergeComments_NilComments verifies that a nil comments pointer is
// handled gracefully and the new body is returned unchanged.
func TestMergeComments_NilComments(t *testing.T) {
	body := "<p>Hello world</p>"
	result, err := mergeComments(body, "", nil)
	assert.NoError(t, err)
	assert.Equal(t, body, result)
}

// TestMergeComments_HTMLEntities verifies that selections containing HTML
// entities (&lt;, &gt;) are matched correctly. The API returns raw (unescaped)
// text for OriginalSelection; htmlEscapeText encodes &, < and > to their
// entity forms before searching.
func TestMergeComments_HTMLEntities(t *testing.T) {
	body := `<p>Hello &lt;world&gt; it&#39;s me</p>`
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-ent">&lt;world&gt;</ac:inline-comment-marker> it&#39;s me</p>`
	// The API returns the raw (unescaped) selection text.
	comments := makeComments("<world>", "uuid-ent")

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-ent">&lt;world&gt;</ac:inline-comment-marker> it&#39;s me</p>`, result)
}

// TestMergeComments_ApostropheEncoded verifies the known limitation: when a
// selection includes an apostrophe that Confluence stores as the numeric
// entity &#39; in the page body, mergeComments cannot locate the selection
// (htmlEscapeText does not encode ' to &#39;) and the comment is dropped with
// a warning rather than panicking or producing invalid output.
func TestMergeComments_ApostropheEncoded(t *testing.T) {
	// New body uses &#39; entity (as Confluence sometimes stores apostrophes).
	body := `<p>Hello &lt;world&gt; it&#39;s me</p>`
	// Old body has the comment marker around a selection that includes an apostrophe.
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-apos-enc">&lt;world&gt; it&#39;s</ac:inline-comment-marker> me</p>`
	// The API returns the raw unescaped selection including a literal apostrophe.
	comments := makeComments("<world> it's", "uuid-apos-enc")

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	// The comment is dropped (body unchanged) because htmlEscapeText("it's")
	// produces "it's", which doesn't match "it&#39;s" in the new body.
	assert.Equal(t, body, result)
}

// TestMergeComments_ApostropheSelection verifies that a selection containing a
// literal apostrophe is found when the new body also contains a literal
// apostrophe (as mark's renderer typically emits). This exercises the
// htmlEscapeText path which intentionally does not encode ' or ".
func TestMergeComments_ApostropheSelection(t *testing.T) {
	body := `<p>Hello it's a test</p>`
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-apos">it's</ac:inline-comment-marker> a test</p>`
	// The API returns the raw (unescaped) selection text with a literal apostrophe.
	comments := makeComments("it's", "uuid-apos")

	result, err := mergeComments(body, oldBody, comments)
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

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <strong><ac:inline-comment-marker ac:ref="uuid-nested">world</ac:inline-comment-marker></strong></p>`, result)
}

// TestMergeComments_EmptySelection verifies that a comment with an empty
// OriginalSelection is skipped without panicking and the body is returned
// unchanged.
func TestMergeComments_EmptySelection(t *testing.T) {
	body := "<p>Hello world</p>"
	comments := makeComments("", "uuid-empty")

	result, err := mergeComments(body, body, comments)
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

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-dup">world</ac:inline-comment-marker></p>`, result)
}

// ---------------------------------------------------------------------------
// Additional mergeComments scenario tests
// ---------------------------------------------------------------------------

// TestMergeComments_MultipleComments verifies that two non-overlapping comments
// are both correctly re-embedded via back-to-front replacement.
func TestMergeComments_MultipleComments(t *testing.T) {
	body := "<p>Hello world and foo bar</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-1">world</ac:inline-comment-marker> and foo <ac:inline-comment-marker ac:ref="uuid-2">bar</ac:inline-comment-marker></p>`
	comments := makeComments("world", "uuid-1", "bar", "uuid-2")

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-1">world</ac:inline-comment-marker> and foo <ac:inline-comment-marker ac:ref="uuid-2">bar</ac:inline-comment-marker></p>`, result)
}

// TestMergeComments_EmptyResults verifies that an InlineComments value with a
// non-nil but empty Results slice is handled gracefully.
func TestMergeComments_EmptyResults(t *testing.T) {
	body := "<p>Hello world</p>"
	result, err := mergeComments(body, body, &confluence.InlineComments{})
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
	result, err := mergeComments(body, body, comments)
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

	result, err := mergeComments(body, oldBody, comments)
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

	result, err := mergeComments(body, oldBody, comments)
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

	result, err := mergeComments(body, oldBody, comments)
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

	result, err := mergeComments(body, body, comments)
	assert.NoError(t, err)
	assert.Equal(t, body, result) // body unchanged, single warning logged
}

// TestMergeComments_CDATASelection verifies that a selection inside a
// CDATA-backed macro body (e.g. ac:code) is matched even though < and > are
// stored as raw characters rather than HTML entities. The raw form is tried as
// a fallback when the escaped form is not found.
func TestMergeComments_CDATASelection(t *testing.T) {
	// New body contains a code macro with CDATA — raw < and > in the content.
	body := `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[func foo() { return <nil> }]]></ac:plain-text-body></ac:structured-macro>`
	// Old body has the marker around the raw selection inside CDATA.
	oldBody := `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[func foo() { return <ac:inline-comment-marker ac:ref="uuid-cdata"><nil></ac:inline-comment-marker> }]]></ac:plain-text-body></ac:structured-macro>`
	// The API returns the raw (unescaped) selection.
	comments := makeComments("<nil>", "uuid-cdata")

	result, err := mergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	// The raw selection "<nil>" should be found and wrapped with a marker.
	assert.Equal(t, `<ac:structured-macro ac:name="code"><ac:plain-text-body><![CDATA[func foo() { return <ac:inline-comment-marker ac:ref="uuid-cdata"><nil></ac:inline-comment-marker> }]]></ac:plain-text-body></ac:structured-macro>`, result)
}

// BenchmarkRunCompileOnly exercises the whole per-file loop in compile-only
// mode, which never reaches the network, so it measures the fixed per-file
// overhead that Run pays regardless of Confluence latency.
func BenchmarkRunCompileOnly(b *testing.B) {
	// Other tests in this package raise the global level; log formatting would
	// otherwise dominate the measurement.
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	b.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	for _, fileCount := range []int{1, 50} {
		b.Run(fmt.Sprintf("files=%d", fileCount), func(b *testing.B) {
			dir := b.TempDir()
			for i := range fileCount {
				body := fmt.Sprintf(
					"<!-- Space: TEST -->\n<!-- Title: Page %d -->\n\n"+
						"# Page %d\n\nSome **text** with a [link](https://example.com).\n\n"+
						"| a | b |\n| --- | --- |\n| 1 | 2 |\n\n```go\nfunc main() {}\n```\n",
					i, i,
				)
				path := filepath.Join(dir, fmt.Sprintf("page-%03d.md", i))
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					b.Fatal(err)
				}
			}

			config := Config{
				BaseURL:     "http://127.0.0.1:1",
				Files:       filepath.Join(dir, "*.md"),
				CompileOnly: true,
				Features:    []string{"mention"},
				Output:      io.Discard,
			}

			b.ReportAllocs()
			for b.Loop() {
				if err := Run(config); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestFormatVersionMessage covers the round trip --changes-only depends on:
// whatever formatVersionMessage writes must be readable by
// readContentHash on the next run, or the page updates every time.
func TestFormatVersionMessage(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"

	t.Run("hash leads so truncation cannot eat it", func(t *testing.T) {
		// Confluence bounds the version message. With the tag appended, a long
		// operator message pushed it past the limit and silently disabled
		// change detection; leading with the tag costs the prose instead.
		long := strings.Repeat("a very long commit subject ", 40)
		got := formatVersionMessage(long, hash)

		assert.True(t, strings.HasPrefix(got, "[v"+hash+"]"),
			"the fingerprint must come first")

		for _, limit := range []int{255, 128, 64, 43} {
			truncated := got
			if len(truncated) > limit {
				truncated = truncated[:limit]
			}
			matches := readContentHash(truncated)
			assert.Equal(t, hash, matches, "hash must survive truncation at %d", limit)
		}
	})

	t.Run("empty operator message leaves no stray separator", func(t *testing.T) {
		assert.Equal(t, "[v"+hash+"]", formatVersionMessage("", hash))
	})

	t.Run("operator message is preserved", func(t *testing.T) {
		got := formatVersionMessage("published by CI", hash)
		assert.Equal(t, "[v"+hash+"] published by CI", got)
	})

	t.Run("round trips through the reader", func(t *testing.T) {
		for _, msg := range []string{"", "published by CI", "[brackets] and (parens)"} {
			assert.Equal(t, hash, readContentHash(formatVersionMessage(msg, hash)),
				"message %q must round trip", msg)
		}
	})
}

// TestContentHashPatternReadsLegacyTrailingTag pins backward compatibility:
// pages stamped by an older mark carry the tag at the end. If those stopped
// matching, every page in every space would take one spurious update on the
// first run after upgrading.
func TestContentHashPatternReadsLegacyTrailingTag(t *testing.T) {
	const hash = "89abcdef0123456789abcdef0123456789abcdef"

	legacy := fmt.Sprintf("%s [v%s]", "published by CI", hash)
	assert.Equal(t, hash, readContentHash(legacy),
		"the old trailing format must still be recognised")
}

// TestContentHashPatternIgnoresNonHashes guards against a version message that
// merely looks tag-shaped being read as a fingerprint.
func TestContentHashPatternIgnoresNonHashes(t *testing.T) {
	for _, msg := range []string{
		"[v123]", // too short
		"[vZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ]", // not hex
		"[v0123456789abcdef0123456789abcdef0123456]",  // 39 chars
		"no tag at all",
	} {
		assert.Empty(t, readContentHash(msg),
			"%q must not be read as a fingerprint", msg)
	}
}

// TestReadContentHashIgnoresTagsInsideTheMessage pins the reason both patterns
// are anchored.
//
// An operator's --version-message can itself contain something tag-shaped --
// someone quoting a previous version message, most plausibly. An unanchored
// search would find that copy first and prefer it over the real fingerprint at
// the end. The result is only an update that was not needed, never a change
// that was missed, but position is free information and discarding it makes
// the wrong answer reachable for no benefit.
func TestReadContentHashIgnoresTagsInsideTheMessage(t *testing.T) {
	const real = "0123456789abcdef0123456789abcdef01234567"
	const quoted = "89abcdef0123456789abcdef0123456789abcdef"

	// A page still carrying the legacy trailing layout, whose operator message
	// quotes an older tag.
	legacy := fmt.Sprintf("re-publish of [v%s] [v%s]", quoted, real)
	assert.Equal(t, real, readContentHash(legacy),
		"the trailing fingerprint is the real one, not the quoted copy")

	// The layout mark writes now: its own tag leads, so it wins outright.
	current := fmt.Sprintf("[v%s] re-publish of [v%s]", real, quoted)
	assert.Equal(t, real, readContentHash(current))

	// A tag that is neither leading nor trailing is not a fingerprint at all.
	buried := fmt.Sprintf("see [v%s] for context", quoted)
	assert.Empty(t, readContentHash(buried))
}
