// Package comments carries inline comments over to a page's new body for
// --preserve-comments.
package comments

import (
	"encoding/xml"
	"fmt"
	stdhtml "html"
	"iter"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
)

var markerRegex = regexp.MustCompile(`(?s)<ac:inline-comment-marker ac:ref="([^"]+)">(.*?)</ac:inline-comment-marker>`)

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

// textSpan is a run of page text in a storage body, as the byte offsets
// [start, end).
type textSpan struct {
	start int
	end   int
	// stretch numbers the stretch of text the run belongs to: runs that only
	// inlineElements separate, which a reader sees as one.
	stretch int
}

// inlineElements are the tags a stretch of text runs through. Any other
// markup ends the stretch, so that a selection is never matched across two
// blocks, a macro or a line break.
var inlineElements = map[string]bool{
	"a": true, "abbr": true, "b": true, "big": true, "cite": true, "code": true,
	"del": true, "dfn": true, "em": true, "i": true, "ins": true, "kbd": true,
	"mark": true, "q": true, "s": true, "samp": true, "small": true, "span": true,
	"strike": true, "strong": true, "sub": true, "sup": true, "tt": true, "u": true,
	"var": true,

	"ac:link": true, "ac:link-body": true, "ac:emoticon": true,
	"ri:attachment": true, "ri:blog-post": true, "ri:content-entity": true,
	"ri:page": true, "ri:shortcut": true, "ri:space": true, "ri:url": true,
	"ri:user": true,
}

// settingElements hold text that is not page text: a macro's parameters, a
// task's id and status, and a placeholder's editor-only prompt. A marker
// inside one corrupts it.
var settingElements = map[string]bool{
	"ac:parameter":         true,
	"ac:default-parameter": true,
	"ac:task-id":           true,
	"ac:task-uuid":         true,
	"ac:task-status":       true,
	"ac:placeholder":       true,
}

// textSpans returns the runs of body a reader sees as page text: everything
// outside tags, comments, CDATA sections and processing instructions, and
// outside settingElements. A marker placed anywhere else would break the
// markup or show up as literal text.
func textSpans(body string) []textSpan {
	var spans []textSpan
	inSetting := 0
	textStart := 0
	stretch := 0
	for i := 0; i < len(body); {
		if body[i] != '<' {
			i++
			continue
		}
		if inSetting == 0 && i > textStart {
			spans = append(spans, textSpan{start: textStart, end: i, stretch: stretch})
		}
		end := markupEnd(body, i)
		name, closing, selfClosing := tagName(body[i:end])
		if !inlineElements[name] {
			stretch++
		}
		switch {
		case !settingElements[name] || selfClosing:
		case closing:
			inSetting = max(inSetting-1, 0)
		default:
			inSetting++
		}
		i = end
		textStart = end
	}
	if inSetting == 0 && len(body) > textStart {
		spans = append(spans, textSpan{start: textStart, end: len(body), stretch: stretch})
	}
	return spans
}

// markupEnd returns the offset just past the markup that starts with the "<"
// at body[start], or len(body) if it is never closed.
func markupEnd(body string, start int) int {
	closeAfter := func(prefix, terminator string) int {
		if i := strings.Index(body[start+len(prefix):], terminator); i != -1 {
			return start + len(prefix) + i + len(terminator)
		}
		return len(body)
	}
	rest := body[start:]
	switch {
	case strings.HasPrefix(rest, "<!--"):
		return closeAfter("<!--", "-->")
	case strings.HasPrefix(rest, "<![CDATA["):
		return closeAfter("<![CDATA[", "]]>")
	case strings.HasPrefix(rest, "<?"):
		return closeAfter("<?", "?>")
	}
	// A tag, whose quoted attribute values may hold a ">".
	var quote byte
	for i := start + 1; i < len(body); i++ {
		switch c := body[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i + 1
		}
	}
	return len(body)
}

// tagName returns the element name of a tag such as "<p>", "</p>" or "<br/>",
// and whether it closes or is self-closing.
func tagName(tag string) (name string, closing, selfClosing bool) {
	tag = strings.TrimPrefix(tag, "<")
	if rest, ok := strings.CutPrefix(tag, "/"); ok {
		tag, closing = rest, true
	}
	selfClosing = strings.HasSuffix(tag, "/>")
	end := strings.IndexAny(tag, " \t\r\n/>")
	if end == -1 {
		end = len(tag)
	}
	return tag[:end], closing, selfClosing
}

// pageText is a stretch of page text as a reader sees it, which is how a
// selection spells it: character references resolved -- goldmark writes a
// double quote as &quot;, mark's templates as &#34;, and Confluence and raw
// HTML often leave it as it is -- and each run of white space, a line break or
// a no-break space included, as one space.
type pageText struct {
	text string
	// from[i] and until[i] are the body offsets [from, until) of the
	// character, reference or white space text[i] was read from.
	from  []int
	until []int
}

// readPageText reads the stretch of body made of spans.
func readPageText(body string, spans []textSpan) pageText {
	var text strings.Builder
	var from, until []int
	// spaceIn is the span the space ending the text so far came from, or -1
	// if the text does not end in one.
	spaceIn := -1
	for n, span := range spans {
		for i := span.start; i < span.end; {
			_, size := utf8.DecodeRuneInString(body[i:span.end])
			decoded := body[i : i+size]
			if body[i] == '&' {
				if ref, value, ok := characterReference(body[i:span.end]); ok {
					size, decoded = len(ref), value
				}
			}
			switch {
			case strings.TrimFunc(decoded, unicode.IsSpace) != "":
				for range len(decoded) {
					from = append(from, i)
					until = append(until, i+size)
				}
				text.WriteString(decoded)
				spaceIn = -1
			case spaceIn == n:
				until[len(until)-1] = i + size
			case spaceIn == -1:
				from = append(from, i)
				until = append(until, i+size)
				text.WriteByte(' ')
				spaceIn = n
			default:
				// White space following white space in an earlier run adds
				// nothing to what the reader sees.
			}
			i += size
		}
	}
	return pageText{text: text.String(), from: from, until: until}
}

// pageTexts reads each stretch of page text in body.
func pageTexts(body string) []pageText {
	var texts []pageText
	spans := textSpans(body)
	for len(spans) > 0 {
		n := 1
		for n < len(spans) && spans[n].stretch == spans[0].stretch {
			n++
		}
		texts = append(texts, readPageText(body, spans[:n]))
		spans = spans[n:]
	}
	return texts
}

// collapseSpace returns s with each run of white space as one space.
func collapseSpace(s string) string {
	var collapsed strings.Builder
	inSpace := false
	for _, r := range s {
		if !unicode.IsSpace(r) {
			collapsed.WriteRune(r)
		} else if !inSpace {
			collapsed.WriteByte(' ')
		}
		inSpace = unicode.IsSpace(r)
	}
	return collapsed.String()
}

// xmlEntities are the references XML itself defines.
var xmlEntities = map[string]string{"amp": "&", "lt": "<", "gt": ">", "quot": `"`, "apos": "'"}

// characterReference reads the reference at the start of s, which begins with
// "&", and returns it with the text it stands for. A named reference may be
// any HTML one, as Confluence accepts them.
func characterReference(s string) (ref, value string, ok bool) {
	const longestName = 32
	semi := strings.IndexByte(s[:min(len(s), longestName+2)], ';')
	if semi < 2 || semi > longestName+1 {
		return "", "", false
	}
	ref, name := s[:semi+1], s[1:semi]
	if number, isNumeric := strings.CutPrefix(name, "#"); isNumeric {
		base := 10
		if hex, isHex := strings.CutPrefix(strings.ToLower(number), "x"); isHex {
			number, base = hex, 16
		}
		code, err := strconv.ParseInt(number, base, 32)
		if err != nil || code < 0 || code > utf8.MaxRune {
			return "", "", false
		}
		r := rune(code)
		if !utf8.ValidRune(r) {
			return "", "", false
		}
		return ref, string(r), true
	}
	if value, ok = xmlEntities[name]; !ok {
		value, ok = xml.HTMLEntity[name]
	}
	return ref, value, ok
}

// occurrences yields the body offsets [start, end) of each occurrence of
// selection in texts, in order. An occurrence must take whole characters and
// references: one that would split a reference is no place for a marker. It
// may run through inline tags, which markRuns then keeps out of the markers.
func occurrences(texts []pageText, selection string) iter.Seq2[int, int] {
	selection = collapseSpace(selection)
	return func(yield func(int, int) bool) {
		if selection == "" {
			return
		}
		for _, t := range texts {
			for pos := 0; ; pos++ {
				index := strings.Index(t.text[pos:], selection)
				if index == -1 {
					break
				}
				pos += index
				end := pos + len(selection)
				startsUnit := pos == 0 || t.from[pos] != t.from[pos-1]
				endsUnit := end == len(t.text) || t.from[end] != t.from[end-1]
				if startsUnit && endsUnit && !yield(t.from[pos], t.until[end-1]) {
					return
				}
			}
		}
	}
}

// markRuns wraps each run of text in commented, a piece of body that starts
// and ends in text, in a marker of its own for ref. Confluence writes a
// comment that runs through inline markup the same way, and a marker that
// took a tag in could close an element it did not open. A run of nothing but
// white space is left bare beside runs that have text, since it may sit where
// an element takes no markup.
func markRuns(commented, ref string) string {
	type run struct {
		text   string
		isText bool
	}
	var runs []run
	hasText := false
	for i := 0; i < len(commented); {
		end := len(commented)
		if commented[i] == '<' {
			end = markupEnd(commented, i)
			runs = append(runs, run{text: commented[i:end]})
		} else {
			if next := strings.IndexByte(commented[i:], '<'); next != -1 {
				end = i + next
			}
			runs = append(runs, run{text: commented[i:end], isText: true})
			hasText = hasText || strings.TrimSpace(commented[i:end]) != ""
		}
		i = end
	}

	var marked strings.Builder
	for _, r := range runs {
		if !r.isText || (hasText && strings.TrimSpace(r.text) == "") {
			marked.WriteString(r.text)
			continue
		}
		fmt.Fprintf(&marked,
			`<ac:inline-comment-marker ac:ref="%s">%s</ac:inline-comment-marker>`,
			stdhtml.EscapeString(ref), r.text)
	}
	return marked.String()
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
//
// A comment that cannot be placed is lost, and only logged; MergeWithWarnings
// also records it.
func Merge(newBody string, oldBody string, comments *confluence.InlineComments) (string, error) {
	return MergeWithWarnings(newBody, oldBody, comments, nil)
}

// MergeWithWarnings is Merge, adding each comment that cannot be placed to
// warnings as well as logging it. Nil warnings only logs.
func MergeWithWarnings(
	newBody string, oldBody string, comments *confluence.InlineComments, warnings *report.Warnings,
) (string, error) {
	if comments == nil || len(comments.Results) == 0 {
		return newBody, nil
	}
	// 1. Extract context for each comment from oldBody
	contexts := make(map[string]commentContext)
	matches := markerRegex.FindAllStringSubmatchIndex(oldBody, -1)
	for _, match := range matches {
		ref := oldBody[match[2]:match[3]]
		// A comment that runs through inline markup has several markers:
		// its context is what precedes the first and follows the last.
		ctx, seen := contexts[ref]
		if !seen {
			ctx.before = contextBefore(oldBody, match[0], contextWindowBytes)
		}
		ctx.after = contextAfter(oldBody, match[1], contextWindowBytes)
		contexts[ref] = ctx
	}

	type replacement struct {
		start     int
		end       int
		ref       string
		selection string
	}
	var replacements []replacement
	seenRefs := make(map[string]bool)
	texts := pageTexts(newBody)

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
			warnings.Add(report.WarningInlineComment,
				"inline comment %s is lost: the text it was made on is empty", ref)
			continue
		}

		ctx, hasCtx := contexts[ref]

		var bestStart = -1
		var bestEnd = -1
		var minDistance = 1000000

		// The selection is plain text, so it is looked for in the page text
		// with its references resolved: a match in markup would break the XML
		// or show as literal text.
		candidates := 0
		for start, end := range occurrences(texts, selection) {
			// Skip candidates that start or end in the middle of a multi-byte
			// UTF-8 rune; such a match would produce invalid UTF-8 output.
			if !utf8.RuneStart(newBody[start]) || (end < len(newBody) && !utf8.RuneStart(newBody[end])) {
				continue
			}

			candidates++
			if candidates > maxCandidates {
				break
			}

			if !hasCtx {
				// No context available; use the first occurrence.
				bestStart = start
				bestEnd = end
				break
			}

			newBefore := contextBefore(newBody, start, contextWindowBytes)
			newAfter := contextAfter(newBody, end, contextWindowBytes)

			// Fast path: exact context match is the best possible result.
			if newBefore == ctx.before && newAfter == ctx.after {
				bestStart = start
				bestEnd = end
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
				continue
			}

			distance := levenshteinDistance(ctx.before, newBefore) + levenshteinDistance(ctx.after, newAfter)

			if distance < minDistance {
				minDistance = distance
				bestStart = start
				bestEnd = end
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
			warnings.Add(report.WarningInlineComment,
				"inline comment %s is lost: the text it was made on, %q, is not in the new body's page text",
				ref, truncateSelection(selection, 50))
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
			warnings.Add(report.WarningInlineComment,
				"inline comment %s is lost: the text it was made on, %q, overlaps another comment's",
				r.ref, truncateSelection(r.selection, 50))
			continue
		}
		minAppliedStart = r.start
		newBody = newBody[:r.start] + markRuns(newBody[r.start:r.end], r.ref) + newBody[r.end:]
	}

	return newBody, nil
}
