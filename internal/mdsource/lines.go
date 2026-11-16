package mdsource

import "bytes"

// LineOf returns the line of original that line of derived was copied from,
// both counting from 1, where derived is original with lines taken out of it
// or put into it, as a document is once its headers are read and its includes
// expanded.
//
// It is the line of original that reads the same and has as many lines that
// read the same above it. Where the two texts hold a different number of
// lines reading so, which of them the line is cannot be told, and LineOf
// returns 0.
func LineOf(derived, original []byte, line int) int {
	lines := bytes.Split(derived, []byte("\n"))
	if line < 1 || line > len(lines) {
		return 0
	}

	text := lines[line-1]
	above, total := 0, 0

	for i, other := range lines {
		if bytes.Equal(other, text) {
			total++

			if i < line-1 {
				above++
			}
		}
	}

	found, alike := 0, 0

	for i, other := range bytes.Split(original, []byte("\n")) {
		if !bytes.Equal(other, text) {
			continue
		}

		if alike == above {
			found = i + 1
		}

		alike++
	}

	if alike != total {
		return 0
	}

	return found
}
