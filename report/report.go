// Package report collects what a run did, so that it can be said in whichever
// way the thing reading it needs to hear.
//
// The log is for people. This is for everything else: a script deciding what
// changed, or a CI system that wants a failure attached to the line of the file
// that caused it.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Format is a shape a run's outcome can be written in.
type Format string

// The shapes a run's outcome can be written in.
const (
	// FormatURL prints the address of each published page and nothing else,
	// which is what mark has always printed.
	FormatURL Format = "url"

	// FormatJSON prints one object describing the whole run.
	FormatJSON Format = "json"

	// FormatGitHub prints GitHub Actions workflow commands, which appear
	// against the file they name in a pull request.
	FormatGitHub Format = "github"
)

// ParseFormat reads the value given to --output-format.
func ParseFormat(value string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(value))) {
	case "", FormatURL:
		return FormatURL, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatGitHub:
		return FormatGitHub, nil
	default:
		return "", fmt.Errorf(
			"unknown --output-format value %q: expected %s, %s or %s",
			value, FormatURL, FormatJSON, FormatGitHub,
		)
	}
}

// Status is what became of one document.
type Status string

// What became of one document.
const (
	StatusPublished Status = "published"
	StatusUnchanged Status = "unchanged"
	StatusSkipped   Status = "skipped"
	StatusFailed    Status = "failed"

	// What a dry run that was also given --changes-only says a real run would
	// do. A page it would leave alone is StatusUnchanged, as it is on a real run.
	StatusWouldCreate Status = "would-create"
	StatusWouldUpdate Status = "would-update"
)

// ReasonNotSynchronized is the Reason of a page skipped because its document
// opted out of being published. The github format does not annotate it: the
// author asked for it, so there is nothing in the pull request to look at.
const ReasonNotSynchronized = "the document is not synchronized"

// Page is one document's outcome.
type Page struct {
	File string `json:"file"`
	// Source is the file the document was made from, when it is not File
	// itself; see SetSources.
	Source string `json:"source,omitempty"`
	Status Status `json:"status"`
	Space  string `json:"space,omitempty"`
	Title  string `json:"title,omitempty"`
	PageID string `json:"pageId,omitempty"`
	URL    string `json:"url,omitempty"`

	// Reason says why a page was skipped or how it failed, in the words a
	// person would want to read. On a page a dry run says would be updated, it
	// names what a real run would do besides rewriting the body, such as moving
	// the page.
	Reason string `json:"reason,omitempty"`

	// Warnings are what was wrong with a document that was published anyway --
	// a link that does not resolve, when the run was told to warn about those
	// rather than fail on them, or a header mark does not read.
	//
	// Kept apart from Reason because they do not say what became of the page:
	// a document can carry them and still have published perfectly well, which
	// is the whole reason for warning instead of failing.
	Warnings []string `json:"warnings,omitempty"`

	// WarningDetails are the same warnings, in the same order, each with its
	// kind and the file it is about. When set, they are what the github
	// format annotates.
	WarningDetails []Warning `json:"warningDetails,omitempty"`
}

// Orphan is a tracked page whose document is gone, and what was done about it.
type Orphan struct {
	File string `json:"file"`
	// Source is the file the document was made from; see SetSources.
	Source string `json:"source,omitempty"`
	PageID string `json:"pageId,omitempty"`
	Title  string `json:"title,omitempty"`

	// Action is the --on-orphan action taken, one of the OnOrphan values;
	// OnOrphanReport means the page was left where it is.
	Action OnOrphan `json:"action"`
}

// OnOrphan is what to do about a page whose source file is gone.
type OnOrphan string

// What to do about a page whose source file is gone.
const (
	// OnOrphanReport says so and does nothing. It is the default.
	OnOrphanReport OnOrphan = "report"

	// OnOrphanArchive archives the page. Cloud only.
	OnOrphanArchive OnOrphan = "archive"

	// OnOrphanDelete moves the page to the trash, where a person can still
	// change their mind.
	OnOrphanDelete OnOrphan = "delete"
)

// ParseOnOrphan reads the value given to --on-orphan.
func ParseOnOrphan(value string) (OnOrphan, error) {
	switch OnOrphan(strings.ToLower(strings.TrimSpace(value))) {
	case "", OnOrphanReport:
		return OnOrphanReport, nil
	case OnOrphanArchive:
		return OnOrphanArchive, nil
	case OnOrphanDelete:
		return OnOrphanDelete, nil
	default:
		return "", fmt.Errorf(
			"unknown --on-orphan value %q: expected %s, %s or %s",
			value, OnOrphanReport, OnOrphanArchive, OnOrphanDelete,
		)
	}
}

// Report is everything a run has to say.
type Report struct {
	mu sync.Mutex

	Pages   []Page   `json:"pages"`
	Orphans []Orphan `json:"orphans,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

// New returns an empty report.
func New() *Report {
	return &Report{}
}

// AddPage records what became of a document.
func (r *Report) AddPage(page Page) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// A document published twice -- once waiting on a page this run created,
	// then again once it existed -- is one document, and the later word is the
	// true one.
	for i := range r.Pages {
		if r.Pages[i].File == page.File {
			r.Pages[i] = page

			return
		}
	}

	r.Pages = append(r.Pages, page)
}

// StatusOf reports what the run last recorded for a document, or "" if it
// recorded nothing about it.
func (r *Report) StatusOf(file string) Status {
	if r == nil {
		return ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.Pages {
		if r.Pages[i].File == file {
			return r.Pages[i].Status
		}
	}

	return ""
}

// AddOrphan records a tracked page whose document has gone.
func (r *Report) AddOrphan(orphan Orphan) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.Orphans = append(r.Orphans, orphan)
}

// AddError records something that went wrong and was not about one document.
func (r *Report) AddError(message string) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.Errors = append(r.Errors, message)
}

// SetSources sets the Source of each page and orphan recorded so far to what
// source returns for its File, which stays as it is. A result that is empty or
// the File itself sets no source. A nil source changes nothing.
func (r *Report) SetSources(source func(file string) string) {
	if r == nil || source == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.Pages {
		r.Pages[i].Source = SourceOf(r.Pages[i].File, source)
	}

	for i := range r.Orphans {
		r.Orphans[i].Source = SourceOf(r.Orphans[i].File, source)
	}
}

// SourceOf is what source names for file, or empty when that is file itself
// or source is nil, so that a Source is set only on a document made from
// another file. It is the rule SetSources applies, for a caller naming the
// source of something the report does not hold.
func SourceOf(file string, source func(file string) string) string {
	if source == nil {
		return ""
	}

	if named := source(file); named != file {
		return named
	}

	return ""
}

// annotated is the file a github annotation is made against: the source, when
// there is one, since that is the file a pull request changes.
func annotated(file, source string) string {
	if source != "" {
		return source
	}

	return file
}

// Write says what happened, in the requested shape.
//
// The url form writes as each page publishes rather than at the end, so it is
// not written again here: repeating it would double every line of the output
// mark has always produced.
func (r *Report) Write(w io.Writer, format Format) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	switch format {
	case FormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")

		return encoder.Encode(r)

	case FormatGitHub:
		return r.writeGitHub(w)

	default:
		return nil
	}
}

func (r *Report) writeGitHub(w io.Writer) error {
	for _, page := range r.Pages {
		file := annotated(page.File, page.Source)

		switch page.Status {
		case StatusFailed:
			if err := command(w, "error", file, page.Reason); err != nil {
				return err
			}

		case StatusSkipped:
			if page.Reason == ReasonNotSynchronized {
				break
			}

			if err := command(w, "warning", file, page.Reason); err != nil {
				return err
			}

		case StatusPublished:
			if err := command(w, "notice", file,
				fmt.Sprintf("published %q to %s", page.Title, page.URL)); err != nil {
				return err
			}

		case StatusWouldCreate, StatusWouldUpdate:
			if err := command(w, "notice", file,
				fmt.Sprintf("would %s %q", strings.TrimPrefix(string(page.Status), "would-"), page.Title)); err != nil {
				return err
			}
		}

		// Whatever became of the document. A warning is worth showing against
		// a page that published and against one that was left unchanged, which
		// has no line of its own at all -- the point of it is to be seen in the
		// diff by somebody who is not reading the build log.
		details := page.WarningDetails
		if len(details) == 0 {
			for _, message := range page.Warnings {
				details = append(details, Warning{Message: message})
			}
		}

		for _, warning := range details {
			// A warning about the document itself goes where its other
			// annotations do, the source included; one about another file is
			// annotated against that file.
			at := file
			if warning.File != "" && warning.File != page.File {
				at = warning.File
			}

			if err := command(w, "warning", at, warning.Message); err != nil {
				return err
			}
		}
	}

	for _, orphan := range r.Orphans {
		message := fmt.Sprintf("page %q has no source file", orphan.Title)
		if orphan.Action != OnOrphanReport {
			message = fmt.Sprintf("page %q was %sd: its source file is gone", orphan.Title, orphan.Action)
		}

		if err := command(w, "warning", annotated(orphan.File, orphan.Source), message); err != nil {
			return err
		}
	}

	for _, message := range r.Errors {
		if err := command(w, "error", "", message); err != nil {
			return err
		}
	}

	return nil
}

// command writes one GitHub Actions workflow command.
//
// The file is given as a property so that the annotation appears against that
// file in a pull request, which is the whole reason for this format.
func command(w io.Writer, level, file, message string) error {
	var properties string
	if file != "" {
		properties = " file=" + escapeProperty(file)
	}

	_, err := fmt.Fprintf(w, "::%s%s::%s\n", level, properties, escapeMessage(message))

	return err
}

// escapeMessage encodes the characters that would otherwise end the command or
// start a new one.
func escapeMessage(s string) string {
	return strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
	).Replace(s)
}

// escapeProperty encodes what escapeMessage does and the two characters that
// separate properties from each other and from the message.
func escapeProperty(s string) string {
	return strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
		":", "%3A",
		",", "%2C",
	).Replace(s)
}
