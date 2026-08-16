package metadata

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
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
	content := string(data)
	if !strings.Contains(strings.ToLower(content), IgnoreStart) {
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

// codeLines reports which lines of the document are inside a code block.
//
// Both kinds count: a fenced block and an indented one. The fence lines
// themselves are not included, which does not matter -- a fence is never a
// marker -- and neither is a code span, since a marker has to be alone on its
// line to be one at all.
func codeLines(data []byte) map[int]bool {
	code := map[int]bool{}

	doc := goldmark.New().Parser().Parse(text.NewReader(data))

	// Line numbers come from counting newlines before an offset, so the offsets
	// are collected first and turned into lines in one pass over the document.
	var spans [][2]int
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch node.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
		default:
			return ast.WalkContinue, nil
		}

		lines := node.Lines()
		if lines.Len() == 0 {
			return ast.WalkContinue, nil
		}
		spans = append(spans, [2]int{lines.At(0).Start, lines.At(lines.Len() - 1).Stop})

		return ast.WalkContinue, nil
	})

	if len(spans) == 0 {
		return code
	}

	line := 0
	for offset := range data {
		for _, span := range spans {
			if offset >= span[0] && offset < span[1] {
				code[line] = true
				break
			}
		}
		if data[offset] == '\n' {
			line++
		}
	}

	return code
}
