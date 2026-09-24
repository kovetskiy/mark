package renderer

// AdmonitionType is a Confluence call-out macro: the box every admonition
// syntax mark understands is published as. Its String is the macro's ac:name.
//
// Three syntaxes produce one -- a legacy "Info:" blockquote, a GitHub alert and
// an MkDocs "!!!" admonition -- and each has its own vocabulary, looked up in
// the per-syntax tables below. The tables do not agree on what a word means:
// `!!! note` is the note macro, but `> [!NOTE]` is the info macro, and a GitHub
// "warning" is Confluence's note where an MkDocs one is Confluence's warning.
// That divergence is published behaviour, kept as it is; unifying it would
// change the pages people already have.
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

// mkDocsAdmonitionTypes maps an MkDocs admonition class to its macro. Any other
// class -- MkDocs has a dozen -- is AdmonitionNone.
var mkDocsAdmonitionTypes = map[string]AdmonitionType{
	"info":    AdmonitionInfo,
	"note":    AdmonitionNote,
	"warning": AdmonitionWarning,
	"tip":     AdmonitionTip,
}

// ghAlertTypes maps a GitHub alert type, as the GH alerts transformer records
// it, to its macro. Confluence has no "important" or "caution", so the five
// alerts are folded onto its four macros, and the names shift on the way:
// note is info, warning is note, caution is warning. An unknown type is info
// (see ghAlertType).
var ghAlertTypes = map[string]AdmonitionType{
	"note":      AdmonitionInfo,
	"tip":       AdmonitionTip,
	"important": AdmonitionInfo,
	"warning":   AdmonitionNote,
	"caution":   AdmonitionWarning,
}

// ghAlertType looks an alert type up in ghAlertTypes, falling back to info.
func ghAlertType(alertType string) AdmonitionType {
	if t, ok := ghAlertTypes[alertType]; ok {
		return t
	}
	return AdmonitionInfo
}

// BlockQuoteType is the type of a legacy "Info:" blockquote.
//
// Deprecated: use AdmonitionType, which the blockquote and MkDocs renderers
// now share.
type BlockQuoteType = AdmonitionType

// Deprecated: use the Admonition-prefixed constants.
const (
	Info = AdmonitionInfo
	Note = AdmonitionNote
	Warn = AdmonitionWarning
	Tip  = AdmonitionTip
	None = AdmonitionNone
)

// MkDocsAdmonitionType is the type of an MkDocs "!!!" admonition.
//
// Deprecated: use AdmonitionType, which the blockquote and MkDocs renderers
// now share.
type MkDocsAdmonitionType = AdmonitionType

// Deprecated: use the Admonition-prefixed constants.
const (
	AInfo = AdmonitionInfo
	ANote = AdmonitionNote
	AWarn = AdmonitionWarning
	ATip  = AdmonitionTip
	ANone = AdmonitionNone
)
