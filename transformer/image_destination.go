package transformer

import (
	"net/url"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
)

// These mark an image whose destination or title already holds the value
// itself, rather than Markdown source that still has to be read. An <img> tag
// is decoded by the HTML parser that finds it, and a destination pointed at an
// uploaded attachment is a URL mark built; reading either as Markdown on top of
// that would decode it twice, so a title written as "&amp;amp;" would lose the
// level of escaping it was given on purpose.
const (
	plainDestinationAttribute = "data-mark-plain-destination"
	plainTitleAttribute       = "data-mark-plain-title"
)

// markPlain records that n's destination and title need no further decoding.
func markPlain(n *ast.Image) {
	SetAttributeText(n, plainDestinationAttribute, "")
	SetAttributeText(n, plainTitleAttribute, "")
}

// HasPlainTitle reports whether n's title is a plain value rather than
// Markdown source.
func HasPlainTitle(n *ast.Image) bool {
	_, ok := n.Attribute(plainTitleAttribute)

	return ok
}

// ImageDestination reads an image's destination the way CommonMark defines it:
// backslash escapes and entity references are resolved, so "a\_b.png" names
// a_b.png and "?a=1&amp;b=2" is a query of two parameters. Percent-encoding is
// left as it is; this is still a URL.
func ImageDestination(n *ast.Image, source []byte) string {
	if _, ok := n.Attribute(plainDestinationAttribute); ok {
		return n.Destination.Str(source)
	}

	return unescapeDestination(n.Destination.Bytes(source))
}

// LinkDestination reads a link's destination the way ImageDestination reads an
// image's, so that "other\_page.md" and "a&amp;b.md" name other_page.md and
// a&b.md, and a link and an image written the same way name the same file.
// Percent-encoding is left as it is here too; LocalImagePaths is what reads
// past it.
func LinkDestination(n *ast.Link, source []byte) string {
	return unescapeDestination(n.Destination.Bytes(source))
}

// UnescapeDestination reads a destination written as Markdown source, such as
// a macro's Attachment value, the way ImageDestination reads an image's.
func UnescapeDestination(destination string) string {
	return unescapeDestination([]byte(destination))
}

// unescapeDestination resolves the backslash escapes and the entity and
// numeric references CommonMark allows in a link destination, with the decoder
// goldmark's parser binds to one.
func unescapeDestination(raw []byte) string {
	return string(markdownDecoder.Decode(raw))
}

// LocalImagePaths lists the paths a local file for destination may be found
// at, in the order they are tried: as written first, which is the only lookup
// there used to be, so a file whose name really contains a "%" keeps resolving
// to itself; then percent-decoded, as a browser would read the URL, so that
// "my%20file.png" names "my file.png" just as "<my file.png>" does.
// A destination with a scheme is not a local path and yields nothing.
func LocalImagePaths(destination string) []string {
	if destination == "" || strings.Contains(destination, "://") {
		return nil
	}

	decoded, err := url.PathUnescape(destination)
	if err != nil || decoded == destination {
		return []string{destination}
	}

	return []string{destination, decoded}
}
