package confluence

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrNotFound reports that Confluence answered 404. It is a sentinel so a
// caller can distinguish "the thing is not there" -- which is often an ordinary
// state rather than a failure -- from every other way a request can go wrong.
// A *StatusError for a 404 matches it with errors.Is.
var ErrNotFound = errors.New("404 (Not Found)")

// ErrConflict reports that Confluence answered 409: for a page update, that
// the version sent is not the one after the page's current version. A
// *StatusError for a 409 matches it with errors.Is.
var ErrConflict = errors.New("409 (Conflict)")

// StatusError is a request Confluence answered with a status outside 2xx.
//
// Every request goes through client.do, which returns one of these for any
// such answer, so a caller asks errors.Is(err, ErrNotFound) or errors.As for
// the status instead of reading the response itself.
type StatusError struct {
	// StatusCode and Status are the response's, as net/http reports them:
	// 404 and "404 Not Found".
	StatusCode int
	Status     string

	// URL is the request the status answered -- after redirects, the last
	// one -- with any credentials in it redacted.
	URL string

	// Body is the start of the response body, at most maxErrorBody bytes of
	// it, and Truncated says whether there was more.
	Body      []byte
	Truncated bool
}

// Error words the status the way mark always has: what answered, and for
// most statuses what it said. The URL is part of every one of these. mark
// makes several calls per page, and a status on its own does not say which of
// them refused.
func (e *StatusError) Error() string {
	const prefix = "the Confluence API returned unexpected status: "

	switch e.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Sprintf(prefix+"401 (Unauthorized) for %s", e.URL)
	case http.StatusNotFound:
		return fmt.Sprintf(prefix+"%s for %s", ErrNotFound, e.URL)
	case http.StatusConflict:
		// The body is kept: Confluence says there which kind of conflict it
		// was.
		return fmt.Sprintf(prefix+"%s for %s, output: %q", ErrConflict, e.URL, e.Body)
	}

	truncated := ""
	if e.Truncated {
		truncated = " (truncated)"
	}

	return fmt.Sprintf(prefix+"%s for %s, output: %q%s", e.Status, e.URL, e.Body, truncated)
}

// Is matches ErrNotFound for a 404 and ErrConflict for a 409.
func (e *StatusError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrConflict:
		return e.StatusCode == http.StatusConflict
	default:
		return false
	}
}

// newStatusError builds the error for an answer outside 2xx. body is what was
// read of it, which readBody bounds at maxErrorBody+1 bytes.
func newStatusError(answer *reply) *StatusError {
	body := answer.Body
	truncated := len(body) > maxErrorBody
	if truncated {
		body = body[:maxErrorBody]
	}

	return &StatusError{
		StatusCode: answer.StatusCode,
		Status:     answer.Status,
		URL:        requestTarget(answer),
		Body:       body,
		Truncated:  truncated,
	}
}

// statusOf is the status a request failed with, or 0 when it did not fail
// with one.
func statusOf(err error) int {
	var status *StatusError
	if errors.As(err, &status) {
		return status.StatusCode
	}

	return 0
}

// maxErrorBody caps how much of a failed response's body a StatusError
// quotes. Confluence's own JSON errors are a few hundred bytes; what answers
// with an error is not always Confluence, and a proxy's HTML error page or a
// Server stack trace can run to megabytes.
const maxErrorBody = 4096

// requestTarget names the URL a request was made to, with any credentials in
// it redacted.
func requestTarget(response *reply) string {
	if response == nil || response.Request == nil || response.Request.URL == nil {
		return "the Confluence API"
	}

	return response.Request.URL.Redacted()
}
