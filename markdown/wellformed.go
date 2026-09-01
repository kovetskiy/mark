package mark

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// wellFormedPrologue supplies the two namespace prefixes a compiled body uses
// but never declares -- the page it becomes is what declares them. It is
// deliberately free of newlines so that the line numbers the parser reports are
// the body's own.
const wellFormedPrologue = `<root xmlns:ac="ac" xmlns:ri="ri">`

// CheckWellFormed reports whether a compiled body is XML that Confluence will
// accept.
//
// Storage format is an XML dialect, and Confluence answers a body that is not
// well-formed with BadRequestException: it rejects the whole page rather than
// the element at fault, and says nothing about which element that was. Every
// way of producing invalid markup therefore surfaces to the author as one
// opaque server error, whatever caused it -- an unescaped ampersand in a macro
// parameter, a void <br> written by hand, an HTML comment containing a double
// hyphen.
//
// Running the same parse here turns all of that into one local complaint that
// names the line, before anything is uploaded.
func CheckWellFormed(body string) error {
	decoder := xml.NewDecoder(strings.NewReader(wellFormedPrologue + body + `</root>`))
	decoder.Strict = true

	// Confluence accepts the named HTML entities, and mark's own templates emit
	// them -- &#160; between a footnote and its backlink, &nbsp; from a
	// document. A bare encoding/xml decoder knows only the five XML ones and
	// would report every one of those as an error.
	decoder.Entity = xml.HTMLEntity

	for {
		_, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err == nil {
			continue
		}

		var syntax *xml.SyntaxError
		if !errors.As(err, &syntax) {
			return err
		}

		return fmt.Errorf(
			"the rendered page is not well-formed XML, which Confluence rejects "+
				"in its entirety: line %d: %s%s",
			syntax.Line, syntax.Msg, quoteLine(body, syntax.Line),
		)
	}
}

// quoteLine returns the offending line of the rendered body, so the complaint
// points at something recognisable rather than at a line number in output the
// author never sees.
func quoteLine(body string, line int) string {
	lines := strings.Split(body, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}

	text := strings.TrimSpace(lines[line-1])
	const limit = 160
	if len(text) > limit {
		text = text[:limit] + "..."
	}
	if text == "" {
		return ""
	}

	return "\n  " + text
}
