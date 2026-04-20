package renderer

// GetLineCol returns the 1-based line and column for a given byte offset in the source.
func GetLineCol(source []byte, offset int) (line, col int) {
	line = 1
	col = 1
	if offset > len(source) {
		offset = len(source)
	}
	for i := 0; i < offset; i++ {
		if source[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
