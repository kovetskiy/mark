package renderer

// AdmonitionType is a Confluence call-out macro: the box every admonition
// syntax mark understands is published as. Its String is the macro's ac:name.
//
// Three syntaxes produce one -- a colon blockquote ("> Info:"), a GitHub
// alert and an MkDocs "!!!" admonition -- and a word means the same macro in
// each of them: admonitionTypes.
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

// admonitionTypes maps every admonition word to its macro, in the colour
// GitHub gives the word, or MkDocs Material for a word GitHub does not have:
// blue for information, green for advice, yellow for a warning, red for
// danger. Confluence's note macro is the
// yellow one, so "note" is info and "warning" is note.
//
// Each syntax reads its own vocabulary from it: the colon syntax every word
// (syntax.AdmonitionWords), GitHub its five alert types, and MkDocs every word
// but "warn", which is not one of its types.
var admonitionTypes = map[string]AdmonitionType{
	"note":      AdmonitionInfo,
	"info":      AdmonitionInfo,
	"important": AdmonitionInfo,

	"tip":     AdmonitionTip,
	"hint":    AdmonitionTip,
	"success": AdmonitionTip,

	"warning":   AdmonitionNote,
	"warn":      AdmonitionNote,
	"attention": AdmonitionNote,

	"caution": AdmonitionWarning,
	"danger":  AdmonitionWarning,
	"error":   AdmonitionWarning,
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

// mkDocsAdmonitionType returns the macro of an MkDocs admonition class. A class
// MkDocs has that is not an admonition word -- abstract, question, quote and
// the rest -- is AdmonitionNone, and so is "warn", which MkDocs does not have.
func mkDocsAdmonitionType(class string) AdmonitionType {
	if class == "warn" {
		return AdmonitionNone
	}
	return admonitionType(class)
}
