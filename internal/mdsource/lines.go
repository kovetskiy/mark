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
	return LinesOf(derived, original)(line)
}

// LinesOf returns LineOf for derived and original as a function of the line,
// which reads the two texts on its first call and not again, so that finding
// the lines of many warnings costs what finding one does. The function is not
// for concurrent use.
func LinesOf(derived, original []byte) func(line int) int {
	var (
		read bool

		// lines is derived split into lines, and above how many lines that
		// read the same come before each.
		lines [][]byte
		above []int

		// in holds, for each text a line of derived reads, how many of its
		// lines read so, and the lines of original that do.
		in map[string]*alikeLines
	)

	return func(line int) int {
		if !read {
			read = true
			lines, above, in = readAlike(derived, original)
		}

		if line < 1 || line > len(lines) {
			return 0
		}

		alike := in[string(lines[line-1])]
		if alike.derived != len(alike.original) {
			return 0
		}

		return alike.original[above[line-1]]
	}
}

type alikeLines struct {
	derived  int
	original []int
}

func readAlike(derived, original []byte) ([][]byte, []int, map[string]*alikeLines) {
	lines := bytes.Split(derived, []byte("\n"))
	above := make([]int, len(lines))
	in := make(map[string]*alikeLines, len(lines))

	for i, line := range lines {
		alike := in[string(line)]
		if alike == nil {
			alike = &alikeLines{}
			in[string(line)] = alike
		}

		above[i] = alike.derived
		alike.derived++
	}

	for i, line := range bytes.Split(original, []byte("\n")) {
		if alike := in[string(line)]; alike != nil {
			alike.original = append(alike.original, i+1)
		}
	}

	return lines, above, in
}
