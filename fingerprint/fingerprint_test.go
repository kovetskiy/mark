package fingerprint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestContentFingerprintOfAPlainPageIsTheBodyHash pins what keeps an upgrade
// quiet: a page with neither header -- or with the appearance every page gets
// by default -- is fingerprinted exactly as it was before the headers counted,
// so the fingerprints already stamped on it still match.
func TestContentFingerprintOfAPlainPageIsTheBodyHash(t *testing.T) {
	const body = "<p>body</p>"

	assert.Equal(t, SHA1(body), Content(body, "", "full-width", ""))
	assert.Equal(t, SHA1(body), Content(body, "full-width", "full-width", ""))
	assert.NotEqual(t, SHA1(body), Content(body, "fixed", "full-width", ""))
	assert.NotEqual(t, SHA1(body), Content(body, "full-width", "full-width", "🚀"))
	assert.NotEqual(t, Content(body, "fixed", "full-width", "🚀"), Content(body, "default", "full-width", "🚀"))

	// --content-appearance fixed gives every page without the header a fixed
	// appearance: that is the default, and changes nothing about the hash.
	assert.Equal(t, SHA1(body), Content(body, "fixed", "fixed", ""))
	assert.NotEqual(t, SHA1(body), Content(body, "full-width", "fixed", ""))
}

// TestFormatVersionMessage covers the round trip --changes-only depends on:
// whatever VersionMessage writes must be readable by
// FromVersionMessage on the next run, or the page updates every time.
func TestFormatVersionMessage(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"

	t.Run("hash leads so truncation cannot eat it", func(t *testing.T) {
		// Confluence bounds the version message. With the tag appended, a long
		// operator message pushed it past the limit and silently disabled
		// change detection; leading with the tag costs the prose instead.
		long := strings.Repeat("a very long commit subject ", 40)
		got := VersionMessage(long, hash)

		assert.True(t, strings.HasPrefix(got, "[v"+hash+"]"),
			"the fingerprint must come first")

		for _, limit := range []int{255, 128, 64, 43} {
			truncated := got
			if len(truncated) > limit {
				truncated = truncated[:limit]
			}
			matches := FromVersionMessage(truncated)
			assert.Equal(t, hash, matches, "hash must survive truncation at %d", limit)
		}
	})

	t.Run("empty operator message leaves no stray separator", func(t *testing.T) {
		assert.Equal(t, "[v"+hash+"]", VersionMessage("", hash))
	})

	t.Run("operator message is preserved", func(t *testing.T) {
		got := VersionMessage("published by CI", hash)
		assert.Equal(t, "[v"+hash+"] published by CI", got)
	})

	t.Run("round trips through the reader", func(t *testing.T) {
		for _, msg := range []string{"", "published by CI", "[brackets] and (parens)"} {
			assert.Equal(t, hash, FromVersionMessage(VersionMessage(msg, hash)),
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
	assert.Equal(t, hash, FromVersionMessage(legacy),
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
		assert.Empty(t, FromVersionMessage(msg),
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
	assert.Equal(t, real, FromVersionMessage(legacy),
		"the trailing fingerprint is the real one, not the quoted copy")

	// The layout mark writes now: its own tag leads, so it wins outright.
	current := fmt.Sprintf("[v%s] re-publish of [v%s]", real, quoted)
	assert.Equal(t, real, FromVersionMessage(current))

	// A tag that is neither leading nor trailing is not a fingerprint at all.
	buried := fmt.Sprintf("see [v%s] for context", quoted)
	assert.Empty(t, FromVersionMessage(buried))
}
