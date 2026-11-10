package report

import (
	"fmt"
	"slices"
	"sync"

	"github.com/rs/zerolog/log"
)

// WarningKind says what a warning is about, so that a caller can act on one
// kind without reading the message.
type WarningKind string

// What a warning can be about.
const (
	// WarningUnresolvedLink is a link that does not resolve, on a run told to
	// warn about those rather than fail on them.
	WarningUnresolvedLink WarningKind = "unresolved-link"

	// WarningUnknownKey is a front matter key mark does not read. It may
	// belong to another tool, or be a setting of mark's misspelled.
	WarningUnknownKey WarningKind = "unknown-key"

	// WarningIgnoredValue is a value its front matter key cannot hold, such
	// as a number among labels, dropped while the rest of the key was read.
	WarningIgnoredValue WarningKind = "ignored-value"

	// WarningStrandedHeader is a header written below the document's opening
	// comments, published as page text rather than applied.
	WarningStrandedHeader WarningKind = "stranded-header"

	// WarningMetadataIgnored is a document's metadata set aside whole,
	// because the page it publishes to is named by id.
	WarningMetadataIgnored WarningKind = "metadata-ignored"

	// WarningInclude is an include directive in a fragment that resolves to a
	// different file than it would from the document's directory.
	WarningInclude WarningKind = "include"

	// WarningPlacement is a page found by title outside the parents its
	// document declares: left where it is, or moved under them.
	WarningPlacement WarningKind = "placement"

	// WarningAttachment is a file that was not uploaded, or one uploaded and
	// never referenced.
	WarningAttachment WarningKind = "attachment"

	// WarningInvalidCharacters is text XML cannot carry, replaced with U+FFFD.
	WarningInvalidCharacters WarningKind = "invalid-characters"

	// WarningColumnWidths is a Column-Widths comment that was not applied.
	WarningColumnWidths WarningKind = "column-widths"

	// WarningInlineComment is an inline comment that could not be kept on the
	// page's new body.
	WarningInlineComment WarningKind = "inline-comment"

	// WarningMissingPage is a page the page manifest recorded for a document
	// that no longer exists, so a new one is created.
	WarningMissingPage WarningKind = "missing-page"
)

// Warning is one thing wrong with a document that was published anyway.
type Warning struct {
	Kind WarningKind `json:"kind"`

	// File is the document the warning is about, as mark read it.
	File string `json:"file,omitempty"`

	// Source is the file File was made from, when a library caller names
	// one; see Report.SetSources.
	Source string `json:"source,omitempty"`

	// Line is the line of File the warning is about, counting from 1, or 0
	// when it is about no one line or the line is not known.
	Line int `json:"line,omitempty"`

	Message string `json:"message"`
}

// Warnings collects one document's warnings while it is published, and logs
// each as it is added. A nil *Warnings only logs, which is what a caller with
// no report gets.
type Warnings struct {
	file string

	mu   sync.Mutex
	list []Warning
}

// NewWarnings collects the warnings about the document at file.
func NewWarnings(file string) *Warnings {
	return &Warnings{file: file}
}

// Add logs a warning and records it against the document. One already
// recorded is only logged, so that a document published twice in a run is
// not reported with each of its warnings twice.
func (w *Warnings) Add(kind WarningKind, format string, args ...any) {
	w.AddAt(0, kind, format, args...)
}

// AddAt is Add for a warning about one line of the document, counting from 1;
// 0 names no line.
func (w *Warnings) AddAt(line int, kind WarningKind, format string, args ...any) {
	warning := Warning{Kind: kind, Line: line, Message: fmt.Sprintf(format, args...)}
	if w != nil {
		warning.File = w.file
	}

	switch {
	case warning.File != "" && line > 0:
		log.Warn().Msgf("%s:%d: %s", warning.File, line, warning.Message)
	case warning.File != "":
		log.Warn().Msgf("%s: %s", warning.File, warning.Message)
	default:
		log.Warn().Msg(warning.Message)
	}

	if w == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if !slices.Contains(w.list, warning) {
		w.list = append(w.list, warning)
	}
}

// List returns the warnings added so far, in the order they were added.
func (w *Warnings) List() []Warning {
	if w == nil {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.list)
}
