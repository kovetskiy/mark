package metadata

import (
	"fmt"
	"strings"
)

const (
	// IgnoreStart opens a region that is not published, and IgnoreEnd closes
	// it. The spelling follows the one agreed in issue #317.
	IgnoreStart = "ac:ignore"
	IgnoreEnd   = "ac:ignore end"
)

// StripIgnoredBlocks removes the regions a document marked as not for
// Confluence.
//
//	<!-- Include: ac:profile
//	     Name: Doe, John -->
//	<!-- ac:ignore -->
//	John Doe's profile
//	<!-- ac:ignore end -->
//
// The point is a document that reads well in both places. Some things render
// nicely in one and badly in the other -- a table of contents, a macro that
// only Confluence understands, a plain-text stand-in for it -- and until now
// the only way to have both was to keep two copies of the file.
//
// Ignored lines are removed rather than blanked. Leaving blank lines behind
// would be kinder to line numbers in later error messages, but a blank line is
// not nothing in Markdown: dropped into the middle of a list or an indented
// block it would split the one and end the other, so the region has to go
// entirely.
//
// An unclosed region is an error rather than a silent strip to the end of the
// file. A missing end marker is nearly always a typo, and quietly publishing
// half a page is a worse answer than refusing.
func StripIgnoredBlocks(data []byte) ([]byte, error) {
	text := string(data)
	if !strings.Contains(strings.ToLower(text), IgnoreStart) {
		return data, nil
	}

	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))

	ignoring := false
	openedAt := 0

	for i, line := range lines {
		switch ignoreMarker(line) {
		case markerStart:
			if ignoring {
				return nil, fmt.Errorf(
					"line %d: <!-- %s --> opened again before the one on line %d was closed",
					i+1, IgnoreStart, openedAt,
				)
			}
			ignoring = true
			openedAt = i + 1

		case markerEnd:
			if !ignoring {
				return nil, fmt.Errorf(
					"line %d: <!-- %s --> without a matching <!-- %s -->",
					i+1, IgnoreEnd, IgnoreStart,
				)
			}
			ignoring = false

		default:
			if !ignoring {
				kept = append(kept, line)
			}
		}
	}

	if ignoring {
		return nil, fmt.Errorf(
			"<!-- %s --> on line %d is never closed with <!-- %s -->",
			IgnoreStart, openedAt, IgnoreEnd,
		)
	}

	return []byte(strings.Join(kept, "\n")), nil
}

type marker int

const (
	markerNone marker = iota
	markerStart
	markerEnd
)

// ignoreMarker reports whether a line is one of the region markers.
//
// The end marker is checked first: it begins with the start marker's text, so
// testing the other way round would read every "ac:ignore end" as an opening.
func ignoreMarker(line string) marker {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, commentOpen) || !strings.HasSuffix(line, commentClose) {
		return markerNone
	}
	if len(line) < len(commentOpen)+len(commentClose) {
		return markerNone
	}

	content := strings.ToLower(strings.TrimSpace(
		line[len(commentOpen) : len(line)-len(commentClose)],
	))
	// Collapse the space between the words so that "ac:ignore  end" is the same
	// marker as "ac:ignore end".
	content = strings.Join(strings.Fields(content), " ")

	switch content {
	case IgnoreEnd:
		return markerEnd
	case IgnoreStart:
		return markerStart
	default:
		return markerNone
	}
}
