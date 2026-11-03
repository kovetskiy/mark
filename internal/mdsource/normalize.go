package mdsource

import "bytes"

// byteOrderMark is the UTF-8 encoding of U+FEFF, which some editors write in
// front of a file.
var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// Normalize puts a document's bytes into the shape every parser in mark
// expects: "\n" line endings, and no byte-order mark.
//
// A byte-order mark is not content, and left in front of the first header
// comment it hides the document's metadata. Windows editors write one
// routinely.
//
// Every reader of a document goes through this, not only publishing: a link is
// rewritten from the linked document's headers, which must be read from the
// same bytes.
func Normalize(data []byte) []byte {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	return bytes.TrimPrefix(data, byteOrderMark)
}
