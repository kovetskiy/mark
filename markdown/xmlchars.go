package mark

import (
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/rs/zerolog/log"
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
func warnIllegalXMLChars(path string, source []byte, replaced int) {
	if replaced == 0 {
		if _, n := stdlib.ReplaceIllegalXMLChars(string(source)); n == 0 {
			return
		}
	}

	log.Warn().Msgf(
		"%s: replaced characters XML cannot carry, such as the control codes in "+
			"terminal output, with U+FFFD; Confluence rejects a page containing them",
		path,
	)
}
