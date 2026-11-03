package metadata

import "github.com/kovetskiy/mark/v16/internal/mdsource"

// NormalizeSource gives a document "\n" line endings and drops a byte-order
// mark, as every reader of a document in mark does before parsing it.
func NormalizeSource(data []byte) []byte {
	return mdsource.Normalize(data)
}

// StripIgnoredBlocks removes the regions between <!-- ac:ignore --> and
// <!-- ac:ignore end -->, leaving markers inside code alone. Publishing does
// this before compiling; markdown.CompileMarkdown does not.
func StripIgnoredBlocks(data []byte) ([]byte, error) {
	return mdsource.StripIgnoredBlocks(data)
}
