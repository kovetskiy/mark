package markdown

import (
	"github.com/kovetskiy/mark/v17/report"
	"github.com/kovetskiy/mark/v17/stdlib"
)

// sanitizeXMLChars is stdlib.SanitizeMarkup, under a name the functions with
// a stdlib parameter can still call.
func sanitizeXMLChars(body string) (string, int) {
	return stdlib.SanitizeMarkup(body)
}

// warnIllegalXMLChars tells the author, once per document, that characters
// were replaced -- whether by the pass over the compiled body or on the way
// through the stdlib escaping, which is why the source is looked at as well.
// Replacing them silently would leave a U+FFFD on the page with nothing to say
// where it came from.
func warnIllegalXMLChars(source []byte, replaced int, warnings *report.Warnings) {
	if replaced == 0 {
		if _, n := stdlib.ReplaceIllegalXMLChars(string(source)); n == 0 {
			return
		}
	}

	warnings.Add(report.WarningInvalidCharacters,
		"replaced characters XML cannot carry, such as the control codes in "+
			"terminal output, with U+FFFD; Confluence rejects a page containing them",
	)
}
