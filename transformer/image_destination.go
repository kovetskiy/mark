package transformer

import (
	"net/url"
	"strings"
)

// UnescapeDestination reads a destination written as Markdown source, such as
// a macro's Attachment value, the way goldmark reads a link's or an image's:
// backslash escapes and entity references are resolved, so "a\_b.png" names
// a_b.png, and percent-encoding is left as it is.
//
// A destination in the tree is read with Destination.Value(source) instead:
// goldmark binds the decoder to it, and a value mark built itself carries the
// decoder that says whether it is still Markdown (SourceValue) or not
// (PlainValue).
func UnescapeDestination(destination string) string {
	return string(DecodeMarkdown([]byte(destination)))
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
