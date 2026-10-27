// Package comments carries inline comments over to a page's new body for
// --preserve-comments.
package comments

import (
	"fmt"
	stdhtml "html"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

var markerRegex = regexp.MustCompile(`(?s)<ac:inline-comment-marker ac:ref="([^"]+)">(.*?)</ac:inline-comment-marker>`)

// htmlTextReplacer is the replacer behind htmlEscapeText.
var htmlTextReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// htmlEscapeText escapes only the characters that Confluence storage HTML
// always encodes in text nodes (&, <, >). Unlike html.EscapeString it does NOT
// escape single-quotes or double-quotes, because those are frequently left
// unescaped inside text nodes by the Confluence editor and by mark's own
// renderer, so escaping them would prevent the selection-search from finding
// a valid match.
func htmlEscapeText(s string) string {
	return htmlTextReplacer.Replace(s)
}

// truncateSelection returns a truncated preview of s for use in log messages,
// capped at maxRunes runes, with an ellipsis appended when trimmed.
func truncateSelection(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

// contextBefore returns up to maxBytes of s ending at byteEnd, trimmed
// forward to the nearest valid UTF-8 rune start so the slice is never
// split across a multi-byte sequence.
func contextBefore(s string, byteEnd, maxBytes int) string {
	start := byteEnd - maxBytes
	if start < 0 {
		start = 0
	}
	for start < byteEnd && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:byteEnd]
}

// contextAfter returns up to maxBytes of s starting at byteStart, trimmed
// back to the nearest valid UTF-8 rune start so the slice is never split
// across a multi-byte sequence.
func contextAfter(s string, byteStart, maxBytes int) string {
	end := byteStart + maxBytes
	if end >= len(s) {
		return s[byteStart:]
	}
	for end > byteStart && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[byteStart:end]
}

func levenshteinDistance(s1, s2 string) int {
	r1 := []rune(s1)
	r2 := []rune(s2)

	if len(r1) == 0 {
		return len(r2)
	}
	if len(r2) == 0 {
		return len(r1)
	}

	// Use two rolling rows instead of a full matrix to reduce allocations
	// from O(m×n) to O(n). Swap r1/r2 so r2 is the shorter string, keeping
	// the row width (len(r2)+1) as small as possible.
	if len(r1) < len(r2) {
		r1, r2 = r2, r1
	}

	prev := make([]int, len(r2)+1)
	curr := make([]int, len(r2)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(r1); i++ {
		curr[0] = i
		for j := 1; j <= len(r2); j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			curr[j] = min(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(r2)]
}

type commentContext struct {
	before string
	after  string
}

// maxCandidates is how many occurrences of each selection Merge
// evaluates with Levenshtein distance; further occurrences are ignored to bound
// CPU cost on pages where a selection is short or very common.
const maxCandidates = 100

// contextWindowBytes is the number of bytes of surrounding text captured as
// context around each inline-comment marker. It is used both when extracting
// context from oldBody and when scoring candidates in newBody.
const contextWindowBytes = 100

// Merge re-embeds inline comment markers from the Confluence API into
// newBody (the updated storage HTML about to be uploaded). It extracts context
// from each existing marker in oldBody and uses Levenshtein distance to
// relocate each marker to the best-matching position in newBody, so comment
// threads survive page edits even when the surrounding text has shifted.
func Merge(newBody string, oldBody string, comments *confluence.InlineComments) (string, error) {
	if comments == nil {
		return newBody, nil
	}
	// 1. Extract context for each comment from oldBody
	contexts := make(map[string]commentContext)
	matches := markerRegex.FindAllStringSubmatchIndex(oldBody, -1)
	for _, match := range matches {
		ref := oldBody[match[2]:match[3]]
		// context around the tag
		before := contextBefore(oldBody, match[0], contextWindowBytes)
		after := contextAfter(oldBody, match[1], contextWindowBytes)
		contexts[ref] = commentContext{
			before: before,
			after:  after,
		}
	}

	type replacement struct {
		start     int
		end       int
		ref       string
		selection string
	}
	var replacements []replacement
	seenRefs := make(map[string]bool)

	for _, comment := range comments.Results {
		if comment.Extensions.Location != "inline" {
			log.Debug().
				Str("location", comment.Extensions.Location).
				Str("ref", comment.Extensions.InlineProperties.MarkerRef).
				Msg("comment ignored during inline marker merge: not an inline comment")
			continue
		}

		ref := comment.Extensions.InlineProperties.MarkerRef
		selection := comment.Extensions.InlineProperties.OriginalSelection

		if seenRefs[ref] {
			// Multiple results share the same MarkerRef (e.g. threaded replies).
			// The marker only needs to be inserted once; skip duplicates.
			continue
		}
		// Mark ref as seen immediately so subsequent results for the same ref
		// (threaded replies) are always deduplicated, even if this one is dropped.
		seenRefs[ref] = true

		if selection == "" {
			log.Warn().
				Str("ref", ref).
				Msg("inline comment skipped: original selection is empty; comment will be lost")
			continue
		}

		ctx, hasCtx := contexts[ref]

		// Build the list of forms to search for in newBody. The escaped form
		// is tried first (normal XML text nodes). The raw form is appended as a
		// fallback for text inside CDATA-backed macro bodies (e.g. ac:code),
		// where < and > are stored unescaped inside <![CDATA[...]]>.
		escapedSelection := htmlEscapeText(selection)
		searchForms := []string{escapedSelection}
		if selection != escapedSelection {
			searchForms = append(searchForms, selection)
		}

		var bestStart = -1
		var bestEnd = -1
		var minDistance = 1000000

		// Iterate over search forms; stop as soon as we have a definitive best.
		candidates := 0
		stopSearch := false
		for _, form := range searchForms {
			if stopSearch {
				break
			}
			currentPos := 0
			for {
				index := strings.Index(newBody[currentPos:], form)
				if index == -1 {
					break
				}
				start := currentPos + index
				end := start + len(form)

				// Skip candidates that start or end in the middle of a multi-byte
				// UTF-8 rune; such a match would produce invalid UTF-8 output.
				if !utf8.RuneStart(newBody[start]) || (end < len(newBody) && !utf8.RuneStart(newBody[end])) {
					currentPos = start + 1
					continue
				}

				candidates++
				if candidates > maxCandidates {
					stopSearch = true
					break
				}

				if !hasCtx {
					// No context available; use the first occurrence.
					bestStart = start
					bestEnd = end
					stopSearch = true
					break
				}

				newBefore := contextBefore(newBody, start, contextWindowBytes)
				newAfter := contextAfter(newBody, end, contextWindowBytes)

				// Fast path: exact context match is the best possible result.
				if newBefore == ctx.before && newAfter == ctx.after {
					bestStart = start
					bestEnd = end
					stopSearch = true
					break
				}

				// Lower-bound pruning: Levenshtein distance is at least the
				// absolute difference in rune counts. Use rune counts (not byte
				// lengths) to match the unit levenshteinDistance operates on,
				// avoiding false skips for multibyte UTF-8 content.
				lbBefore := utf8.RuneCountInString(ctx.before) - utf8.RuneCountInString(newBefore)
				if lbBefore < 0 {
					lbBefore = -lbBefore
				}
				lbAfter := utf8.RuneCountInString(ctx.after) - utf8.RuneCountInString(newAfter)
				if lbAfter < 0 {
					lbAfter = -lbAfter
				}
				if lbBefore+lbAfter >= minDistance {
					currentPos = start + 1
					continue
				}

				distance := levenshteinDistance(ctx.before, newBefore) + levenshteinDistance(ctx.after, newAfter)

				if distance < minDistance {
					minDistance = distance
					bestStart = start
					bestEnd = end
				}

				currentPos = start + 1
			}
		}

		if bestStart != -1 {
			replacements = append(replacements, replacement{
				start:     bestStart,
				end:       bestEnd,
				ref:       ref,
				selection: selection,
			})
		} else {
			log.Warn().
				Str("ref", ref).
				Str("selection_preview", truncateSelection(selection, 50)).
				Msg("inline comment dropped: selected text not found in new body; comment will be lost")
		}
	}

	// Sort replacements from back to front to avoid offset issues.
	// Use a stable sort with ref as a tie-breaker so the ordering is
	// deterministic when two markers resolve to the same start offset.
	slices.SortStableFunc(replacements, func(a, b replacement) int {
		if a.start != b.start {
			return b.start - a.start
		}
		if a.ref < b.ref {
			return -1
		}
		if a.ref > b.ref {
			return 1
		}
		return 0
	})

	// Apply replacements back-to-front. Track the minimum start of any
	// applied replacement so that overlapping candidates (whose end exceeds
	// that boundary) are dropped rather than producing nested or malformed
	// <ac:inline-comment-marker> tags.
	minAppliedStart := len(newBody)
	for _, r := range replacements {
		if r.end > minAppliedStart {
			// This replacement overlaps with an already-applied one.
			// Drop it and warn so the user knows the comment was skipped.
			log.Warn().
				Str("ref", r.ref).
				Str("selection_preview", truncateSelection(r.selection, 50)).
				Int("start", r.start).
				Int("end", r.end).
				Int("conflicting_start", minAppliedStart).
				Msg("inline comment marker dropped: selection overlaps an already-placed marker")
			continue
		}
		minAppliedStart = r.start
		selection := newBody[r.start:r.end]
		withComment := fmt.Sprintf(
			`<ac:inline-comment-marker ac:ref="%s">%s</ac:inline-comment-marker>`,
			stdhtml.EscapeString(r.ref),
			selection,
		)
		newBody = newBody[:r.start] + withComment + newBody[r.end:]
	}

	return newBody, nil
}
