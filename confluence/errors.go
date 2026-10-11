package confluence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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

	// Message is what Confluence said was wrong, read out of its JSON error
	// body, or "" when the body is not one of those.
	Message string
}

// Error words the status the way mark always has: what answered, and for
// most statuses what it said. The URL is part of every one of these. mark
// makes several calls per page, and a status on its own does not say which of
// them refused.
func (e *StatusError) Error() string {
	const prefix = "the Confluence API returned unexpected status: "

	// Confluence's own words, where it gave some, say more than any status
	// and in less space than the body they came in.
	if e.Message != "" {
		status := e.Status
		switch e.StatusCode {
		case http.StatusUnauthorized:
			status = "401 (Unauthorized)"
		case http.StatusNotFound:
			status = ErrNotFound.Error()
		case http.StatusConflict:
			status = ErrConflict.Error()
		}

		return fmt.Sprintf(prefix+"%s for %s: %s", status, e.URL, e.Message)
	}

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

	status := &StatusError{
		StatusCode: answer.StatusCode,
		Status:     answer.Status,
		URL:        requestTarget(answer),
		Body:       body,
		Truncated:  truncated,
	}
	if !truncated {
		status.Message = confluenceMessage(body)
	}

	return status
}

// confluenceMessage reads the message out of an error body in one of the
// shapes Confluence answers a failure with, or returns "".
//
// v1 answers {"statusCode":404,"message":"...","reason":"Not Found"}, with
// more beside it on some releases, and v2 a list of problems,
// {"errors":[{"status":404,"code":"NOT_FOUND","title":"...","detail":"..."}]}.
// Anything else -- a proxy's page, a stack trace -- is quoted as it is
// instead.
func confluenceMessage(body []byte) string {
	var answer struct {
		Message string `json:"message"`
		Errors  []struct {
			Code   string `json:"code"`
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}

	var messages []string
	if message := strings.TrimSpace(answer.Message); message != "" {
		messages = append(messages, message)
	}

	for _, problem := range answer.Errors {
		title, detail := strings.TrimSpace(problem.Title), strings.TrimSpace(problem.Detail)
		switch {
		case title != "" && detail != "" && title != detail:
			messages = append(messages, title+": "+detail)
		case detail != "":
			messages = append(messages, detail)
		case title != "":
			messages = append(messages, title)
		case problem.Code != "":
			messages = append(messages, problem.Code)
		}
	}

	return strings.Join(messages, "; ")
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

// endpointMissing reports whether a request failed with one of the statuses
// that say the endpoint is not served here rather than anything about what
// was asked of it -- 404, 405 or 501 -- along with the status it failed with,
// or 0 when it did not fail with one.
func endpointMissing(err error) (status int, ok bool) {
	status = statusOf(err)
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return status, true
	default:
		return status, false
	}
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

// requestError puts an error a request ended with in terms of the operation
// it was part of, or returns nil for none.
//
// A StatusError already says what answered and how, and is returned as it
// is. A login page, which is what a Confluence behind SSO answers an API call
// with, is put in terms of the operation and of where to look; so is a 2xx
// whose body does not parse, which used to reach the user as `invalid
// character '<' looking for beginning of value`: no URL, no status, and no
// page name. Anything else -- no answer at all, or a cancelled one -- has
// nothing to add beyond what the request was trying to do.
func requestError(operation string, err error) error {
	if err == nil {
		return nil
	}

	var status *StatusError
	if errors.As(err, &status) {
		return err
	}

	// A cancelled request is that, whatever else is in the error: a body cut
	// short by its context does not decode, and blaming an SSO page for it
	// would send someone looking for a proxy that is not there.
	if IsCancellation(err) {
		return fmt.Errorf("unable to %s: %w", operation, err)
	}

	if errors.Is(err, ErrLoginPage) {
		return fmt.Errorf(
			"unable to %s: %w; SSO or a proxy in front of Confluence is the usual cause, "+
				"and mark has to reach the API itself",
			operation, err,
		)
	}

	var body *bodyError
	if errors.As(err, &body) {
		return fmt.Errorf(
			"unable to %s: %s answered %s with a body that is not JSON "+
				"(an SSO login page or a proxy interstitial in front of Confluence is the "+
				"usual cause): %w",
			operation, requestTarget(body.answer), body.answer.Status, body.err,
		)
	}

	return fmt.Errorf("unable to %s: %w", operation, err)
}

// bodyError is a 2xx whose body is not the one JSON value do expected: one
// that does not parse, goes on after its value, names another Content-Type,
// or could not be read to its end.
type bodyError struct {
	answer *reply
	err    error
}

func (e *bodyError) Error() string { return e.err.Error() }

func (e *bodyError) Unwrap() error { return e.err }
