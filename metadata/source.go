package metadata

import "bytes"

// byteOrderMark is the UTF-8 encoding of U+FEFF, which some editors write in
// front of a file.
var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// NormalizeSource puts a document's bytes into the shape every parser here
// expects: "\n" line endings, and no byte-order mark.
//
// A byte-order mark is not content, and leaving it in front of the first
// header comment makes the file look to every parser here like one with no
// metadata at all. Windows editors write one routinely.
//
// Every reader of a document goes through this, not only the one publishing
// it: a link to a document is rewritten from that document's headers, and
// reading them from different bytes than publishing does found no headers
// behind a BOM and left the link pointing at a .md file.
func NormalizeSource(data []byte) []byte {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	return bytes.TrimPrefix(data, byteOrderMark)
}
