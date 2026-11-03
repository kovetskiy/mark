package mdsource

import "strings"

const (
	commentOpen  = "<!--"
	commentClose = "-->"
)

// CommentBody returns the trimmed text of a line that is one HTML comment and
// nothing else, as header and ignore markers are written.
func CommentBody(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, commentOpen) || !strings.HasSuffix(line, commentClose) {
		return "", false
	}

	// In "<!-->" the prefix and suffix overlap, and slicing between them would
	// panic (issue #686).
	if len(line) < len(commentOpen)+len(commentClose) {
		return "", false
	}

	return strings.TrimSpace(line[len(commentOpen) : len(line)-len(commentClose)]), true
}
