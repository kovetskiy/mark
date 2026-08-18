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

// The shapes a run's outcome can be written in.
const (
	// FormatURL prints the address of each published page and nothing else,
	// which is what mark has always printed.
	FormatURL = "url"

	// FormatJSON prints one object describing the whole run.
	FormatJSON = "json"

	// FormatGitHub prints GitHub Actions workflow commands, which appear
	// against the file they name in a pull request.
	FormatGitHub = "github"
)

// ParseFormat reads the value given to --output-format.
func ParseFormat(value string) (string, error) {
	switch format := strings.ToLower(strings.TrimSpace(value)); format {
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

// What became of one document.
const (
	StatusPublished = "published"
	StatusUnchanged = "unchanged"
	StatusSkipped   = "skipped"
	StatusFailed    = "failed"
)

// Page is one document's outcome.
type Page struct {
	File   string `json:"file"`
	Status string `json:"status"`
	Space  string `json:"space,omitempty"`
	Title  string `json:"title,omitempty"`
	PageID string `json:"pageId,omitempty"`
	URL    string `json:"url,omitempty"`

	// Reason says why a page was skipped or how it failed, in the words a
	// person would want to read.
	Reason string `json:"reason,omitempty"`
}

// Orphan is a tracked page whose document is gone, and what was done about it.
type Orphan struct {
	File   string `json:"file"`
	PageID string `json:"pageId,omitempty"`
	Title  string `json:"title,omitempty"`
	Action string `json:"action"`
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

// Write says what happened, in the requested shape.
//
// The url form writes as each page publishes rather than at the end, so it is
// not written again here: repeating it would double every line of the output
// mark has always produced.
func (r *Report) Write(w io.Writer, format string) error {
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
		switch page.Status {
		case StatusFailed:
			if err := command(w, "error", page.File, page.Reason); err != nil {
				return err
			}

		case StatusSkipped:
			if err := command(w, "warning", page.File, page.Reason); err != nil {
				return err
			}

		case StatusPublished:
			if err := command(w, "notice", page.File,
				fmt.Sprintf("published %q to %s", page.Title, page.URL)); err != nil {
				return err
			}
		}
	}

	for _, orphan := range r.Orphans {
		message := fmt.Sprintf("page %q has no source file", orphan.Title)
		if orphan.Action != "report" {
			message = fmt.Sprintf("page %q was %sd: its source file is gone", orphan.Title, orphan.Action)
		}

		if err := command(w, "warning", orphan.File, message); err != nil {
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
