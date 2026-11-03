package mdsource

import (
	"fmt"
	"strings"
)

const (
	// ignoreStart opens a region that is not published, and ignoreEnd closes
	// it. The spelling follows the one agreed in issue #317.
	ignoreStart = "ac:ignore"
	ignoreEnd   = "ac:ignore end"
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
// only Confluence understands, a plain-text stand-in for it.
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
	content := string(data)
	if !strings.Contains(strings.ToLower(content), ignoreStart) {
		return data, nil
	}

	// A marker inside a code block is a code sample, not an instruction. Which
	// lines are code is a question for the parser -- guessing at fences by hand
	// gets indented blocks, tildes and nesting wrong -- so goldmark is asked,
	// even though the stripping itself has to stay textual: it runs before the
	// headers are read, and an ignored region is meant to take its headers with
	// it.
	code := codeLines(data)

	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))

	ignoring := false
	openedAt := 0

	for i, line := range lines {
		marker := markerNone
		if !code[i] {
			marker = ignoreMarker(line)
		}

		switch marker {
		case markerStart:
			if ignoring {
				return nil, fmt.Errorf(
					"line %d: <!-- %s --> opened again before the one on line %d was closed",
					i+1, ignoreStart, openedAt,
				)
			}
			ignoring = true
			openedAt = i + 1

		case markerEnd:
			if !ignoring {
				return nil, fmt.Errorf(
					"line %d: <!-- %s --> without a matching <!-- %s -->",
					i+1, ignoreEnd, ignoreStart,
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
			ignoreStart, openedAt, ignoreEnd,
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
	content, ok := CommentBody(line)
	if !ok {
		return markerNone
	}
	content = strings.ToLower(content)
	// Collapse the space between the words so that "ac:ignore  end" is the same
	// marker as "ac:ignore end".
	content = strings.Join(strings.Fields(content), " ")

	switch content {
	case ignoreEnd:
		return markerEnd
	case ignoreStart:
		return markerStart
	default:
		return markerNone
	}
}

// codeLines reports which lines of the document are inside code.
func codeLines(data []byte) map[int]bool {
	code := map[int]bool{}

	spans := CodeRegions(data)
	if len(spans) == 0 {
		return code
	}

	line := 0
	for offset := range data {
		if InCode(spans, offset) {
			code[line] = true
		}
		if data[offset] == '\n' {
			line++
		}
	}

	return code
}
