// Package plantuml encodes PlantUML markup for the "PlantUML Diagrams for
// Confluence" add-on by Stratus Add-ons Ltd, whose plantumlcloud macro carries
// the whole diagram inside the macro itself: no attachment is created and no
// PlantUML server is contacted, the renderer draws from the "data" parameter.
//
// The value is the markup percent-encoded the way JavaScript's
// encodeURIComponent does it, then deflate-compressed with no zlib wrapper,
// then base64-encoded:
//
//	btoa(bytesToString(pako.deflateRaw(encodeURIComponent(markup))))
//
// is the recipe the vendor documents. The renderer inflates the data and runs
// decodeURIComponent over the result, which makes the percent encoding part
// of the contract rather than a detail: a literal "%20" in markup that skips
// it is silently read back as a space.
package plantuml

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
)

// Macro is the parameters of a plantumlcloud macro: everything the add-on
// needs to draw the diagram, which is the whole diagram -- unlike a d2 or a
// mermaid drawing, none of it is an attachment.
type Macro struct {
	// Filename is the "filename" parameter: the name the diagram is shown
	// under, and the .svg it would be exported as.
	Filename string

	// Data is the "data" parameter: the encoded markup the renderer draws
	// from.
	Data string
}

// Process builds the macro for the markup. The name is title when the info
// string gave one and the markup's checksum otherwise -- the fallback a d2 or
// mermaid diagram's attachment name takes for the same situation, so a
// diagram with no name of its own is still named after what it shows rather
// than after the add-on's "Untitled Diagram".
func Process(title string, markup []byte) (Macro, error) {
	data, err := EncodeData(markup)
	if err != nil {
		return Macro{}, err
	}

	name := title
	if name == "" {
		checksum, err := attachment.GetChecksum(bytes.NewReader(markup))
		if err != nil {
			return Macro{}, fmt.Errorf("unable to checksum markup: %w", err)
		}

		name = checksum
	}

	return Macro{
		// A "/" in a title is a path, and a filename parameter is not one:
		// d2 and mermaid flatten the same thing in an attachment's name.
		Filename: attachment.FlattenFilename(name) + ".svg",
		Data:     data,
	}, nil
}

// EncodeData returns the value of the plantumlcloud macro's "data" parameter
// for the given markup.
func EncodeData(markup []byte) (string, error) {
	var deflated bytes.Buffer

	deflater, err := flate.NewWriter(&deflated, flate.DefaultCompression)
	if err != nil {
		return "", fmt.Errorf("unable to deflate markup: %w", err)
	}

	if _, err := deflater.Write([]byte(percentEncode(string(markup)))); err != nil {
		return "", fmt.Errorf("unable to deflate markup: %w", err)
	}

	if err := deflater.Close(); err != nil {
		return "", fmt.Errorf("unable to deflate markup: %w", err)
	}

	return base64.StdEncoding.EncodeToString(deflated.Bytes()), nil
}

// upperhex is the alphabet percent escapes are written in. Decoders accept
// either case, but the add-on's own encoder writes uppercase, and its
// published example round-trips through this one byte for byte.
const upperhex = "0123456789ABCDEF"

// percentEncode escapes markup the way JavaScript's encodeURIComponent does:
// every byte but A-Za-z0-9-_.!~*'() becomes %XX. A character outside that set
// is escaped byte by byte over its UTF-8 spelling, and decoding the escapes in
// order reassembles it, so no character-level work is needed.
//
// Go has no direct equivalent. url.QueryEscape turns a space into "+" and
// escapes !'()* besides, and url.PathEscape leaves reserved delimiters alone,
// so both would hand the renderer bytes its decodeURIComponent reads
// differently -- or rejects.
func percentEncode(markup string) string {
	var escaped strings.Builder
	escaped.Grow(len(markup))

	for i := 0; i < len(markup); i++ {
		c := markup[i]
		if unreserved(c) {
			escaped.WriteByte(c)
			continue
		}

		escaped.WriteByte('%')
		escaped.WriteByte(upperhex[c>>4])
		escaped.WriteByte(upperhex[c&0x0f])
	}

	return escaped.String()
}

// unreserved reports whether c is a byte encodeURIComponent leaves alone.
func unreserved(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}

	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	}

	return false
}
