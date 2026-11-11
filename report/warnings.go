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

	// WarningSourceOnlyLink is a relative link to a file only the source tree
	// holds, under --links-source-root-check-only: it passes the check and is
	// left as written, a path that leads nowhere in Confluence.
	WarningSourceOnlyLink WarningKind = "source-only-link"

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

	// WarningPlacement is a page not placed where its document asks: found
	// outside its declared parents, outside --parents-from-path-root, under a
	// parent title several documents declare, given an Order its parent
	// cannot be found for, or left behind in the space it used to publish to.
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

	// WarningSharedPage is a page the page manifest records for two
	// documents at once, so renaming either moves a page the other claims.
	// Each of the two is warned.
	WarningSharedPage WarningKind = "shared-page"

	// WarningMissingPage is a page or folder the page manifest recorded, for the
	// document or one of its parents, that no longer exists.
	WarningMissingPage WarningKind = "missing-page"

	// WarningRename is a document taken for another renamed because their
	// texts are alike rather than the same, or one not taken for any because
	// more than one is about as alike; see TrackingConfig.RenameSimilarity.
	WarningRename WarningKind = "rename"
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

	mu     sync.Mutex
	list   []Warning
	lineOf func(int) int

	// seen counts each warning added since the document was last read, so
	// that reading it again records only what goes beyond the earlier read.
	seen map[Warning]int
	// listed counts each warning in list.
	listed map[Warning]int
}

// NewWarnings collects the warnings about the document at file.
func NewWarnings(file string) *Warnings {
	return &Warnings{file: file}
}

// Reread starts another read of the document. A warning this read adds no
// more times than an earlier one did is only logged, so that a document
// published twice in a run is not reported with each of its warnings twice.
func (w *Warnings) Reread() {
	if w == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.seen = nil
}

// MapLines has the lines given to AddAt, which count in the text read, mapped
// by lineOf to the lines of the file, for text the file had lines taken out
// of. A nil lineOf leaves them as they are.
func (w *Warnings) MapLines(lineOf func(line int) int) {
	if w == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.lineOf = lineOf
}

// Add logs a warning and records it against the document.
func (w *Warnings) Add(kind WarningKind, format string, args ...any) {
	w.AddAt(0, kind, format, args...)
}

// AddAt is Add for a warning about one line of the document, counting from 1;
// 0 names no line.
func (w *Warnings) AddAt(line int, kind WarningKind, format string, args ...any) {
	warning := Warning{Kind: kind, Line: line, Message: fmt.Sprintf(format, args...)}
	if w == nil {
		logWarning(warning)

		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	warning.File = w.file
	if line > 0 && w.lineOf != nil {
		warning.Line = w.lineOf(line)
	}

	logWarning(warning)

	if w.seen == nil {
		w.seen = map[Warning]int{}
	}
	if w.listed == nil {
		w.listed = map[Warning]int{}
	}

	w.seen[warning]++
	if w.seen[warning] > w.listed[warning] {
		w.list = append(w.list, warning)
		w.listed[warning]++
	}
}

func logWarning(warning Warning) {
	switch {
	case warning.File != "" && warning.Line > 0:
		log.Warn().Msgf("%s:%d: %s", warning.File, warning.Line, warning.Message)
	case warning.File != "":
		log.Warn().Msgf("%s: %s", warning.File, warning.Message)
	default:
		log.Warn().Msg(warning.Message)
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
