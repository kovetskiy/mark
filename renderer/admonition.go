package renderer

import "strings"

// AdmonitionType is a Confluence call-out macro: the box every admonition
// syntax mark understands is published as. Its String is the macro's ac:name.
//
// Three syntaxes produce one -- a colon blockquote ("> Info:"), a GitHub
// alert and an MkDocs "!!!" admonition -- and a word means the same macro in
// each of them: admonitionTypes, and mkDocsOnlyTypes for the words only MkDocs
// reads.
type AdmonitionType int

const (
	AdmonitionInfo AdmonitionType = iota
	AdmonitionNote
	AdmonitionWarning
	AdmonitionTip
	// AdmonitionNone is not a macro: the source is published as a plain
	// blockquote.
	AdmonitionNone
)

// admonitionMacros maps each type to the Confluence macro it is published as.
var admonitionMacros = [...]string{
	AdmonitionInfo:    "info",
	AdmonitionNote:    "note",
	AdmonitionWarning: "warning",
	AdmonitionTip:     "tip",
	AdmonitionNone:    "none",
}

func (t AdmonitionType) String() string {
	// The name is interpolated into ac:name, so a value outside the table
	// answers with the fixed "none" rather than panicking.
	if t < 0 || int(t) >= len(admonitionMacros) {
		return admonitionMacros[AdmonitionNone]
	}
	return admonitionMacros[t]
}

// admonitionTypes maps the admonition words the syntaxes share to their
// macros, in the colour GitHub gives the word, or MkDocs Material for a word
// GitHub does not have: blue for information, green for advice, yellow for a
// warning, red for danger. Confluence's note macro is the yellow one, so
// "note" is info and "warning" is note.
//
// The colon syntax reads every word here (syntax.AdmonitionWords), GitHub its
// five alert types, and MkDocs every word but "warn", which is not one of its
// types, and mkDocsOnlyTypes besides.
var admonitionTypes = map[string]AdmonitionType{
	"note":      AdmonitionInfo,
	"info":      AdmonitionInfo,
	"important": AdmonitionInfo,

	"tip":  AdmonitionTip,
	"hint": AdmonitionTip,

	"warning":   AdmonitionNote,
	"warn":      AdmonitionNote,
	"attention": AdmonitionNote,

	"caution": AdmonitionWarning,
	"danger":  AdmonitionWarning,
}

// mkDocsOnlyTypes are the MkDocs Material types, and their aliases, that are
// not markers in the colon syntax: too many quoted lines open with "Error:",
// "Done:" or "Bug:" that are not meant as a box. Material's question, example
// and quote types have no box: Confluence has no purple one for example, and
// a quote is a quotation.
var mkDocsOnlyTypes = map[string]AdmonitionType{
	"abstract": AdmonitionInfo,
	"summary":  AdmonitionInfo,
	"tldr":     AdmonitionInfo,
	"todo":     AdmonitionInfo,

	"success": AdmonitionTip,
	"check":   AdmonitionTip,
	"done":    AdmonitionTip,

	"error":   AdmonitionWarning,
	"failure": AdmonitionWarning,
	"fail":    AdmonitionWarning,
	"missing": AdmonitionWarning,
	"bug":     AdmonitionWarning,
}

// admonitionType looks word up in admonitionTypes, answering AdmonitionNone
// for a word that is not there.
func admonitionType(word string) AdmonitionType {
	if t, ok := admonitionTypes[word]; ok {
		return t
	}
	return AdmonitionNone
}

// ghAlertType returns the macro of a GitHub alert type, as the GH alerts
// transformer records it: one of note, tip, important, warning and caution.
// Anything else is info.
func ghAlertType(alertType string) AdmonitionType {
	if t := admonitionType(alertType); t != AdmonitionNone {
		return t
	}
	return AdmonitionInfo
}

// mkDocsAdmonitionType returns the macro of an MkDocs admonition class, in any
// case, as Python-Markdown lower-cases it. Any other class -- question,
// example, quote, or "warn", which MkDocs does not have -- is AdmonitionNone.
func mkDocsAdmonitionType(class string) AdmonitionType {
	class = strings.ToLower(class)
	if t, ok := mkDocsOnlyTypes[class]; ok {
		return t
	}
	if class == "warn" {
		return AdmonitionNone
	}
	return admonitionType(class)
}
