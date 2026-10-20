package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// client sends requests to one root of a Confluence: /rest/api, /api/v2, or
// the site itself. It is the whole of the HTTP layer between API and
// net/http, and deliberately small: every request mark makes is a path, a
// query, perhaps a body, and perhaps a JSON answer.
//
// A client holds no state that changes after construction, so one request
// cannot leave anything behind for the next.
type client struct {
	// base is the root every path is joined to. baseErr is why the base URL
	// did not parse, reported by every request rather than by NewAPI, whose
	// signature has no error to return.
	base    *url.URL
	baseErr error

	http *http.Client

	// username and password are sent as basic auth when username is set;
	// otherwise password is a Personal Access Token, sent as a bearer token
	// when it is set.
	username string
	password string

	// trace is the prefix of this client's request and response dumps at
	// TRACE, or nil when they are not written.
	trace *tracer
}

// newClient roots a client at base.
func newClient(base string, httpClient *http.Client, username, password string, trace string) *client {
	c := &client{http: httpClient, username: username, password: password}

	c.base, c.baseErr = url.Parse(base)
	if c.baseErr != nil {
		c.base = &url.URL{}
	}

	if trace != "" {
		c.trace = &tracer{trace}
	}

	return c
}

// reply is what do hands back of an answer: the parts of an http.Response
// mark looks at, with the body already closed, so there is nothing left for a
// caller to close.
type reply struct {
	StatusCode int
	Status     string

	// Body is the start of the body: for a failure, what a StatusError
	// quotes and the byte that says there was more. A success keeps none,
	// since nothing reads it, except at TRACE, where either keeps up to
	// traceDumpLimit bytes for the dump.
	Body []byte

	// Request is the request that got this answer -- after redirects, the
	// last one.
	Request *http.Request

	// Empty is set when the body had nothing in it, and so, for a success,
	// out was left as it was.
	Empty bool
}

// do sends one request, decodes a successful answer into out, and turns
// every other answer into an error that says what it was. mark's error
// handling is built around the behaviours below.
//
//   - Path: each element of path is escaped as one segment and joined to the
//     base path with "/". An empty last element leaves a trailing slash, as
//     "content/" always has. The base's own query is dropped.
//   - Query: encoded with url.Values.Encode, so keys are sorted and spaces
//     become "+". A nil or empty query sends none.
//   - Auth: basic auth when a username was given, otherwise "Authorization:
//     Bearer" with the token, and nothing when that is empty too.
//   - Headers: none by default, beyond what net/http adds. A JSON body is
//     announced as application/json, and an attachment form as its multipart
//     Content-Type together with the token that lets an upload past
//     Confluence's XSRF check. Anything else a request needs comes in header,
//     whose keys must be in canonical form, and overrides those.
//   - Body: a *form is sent as its multipart encoding, an io.Reader as it is,
//     and any other non-nil value as its JSON encoding. Either way it is
//     buffered, so the retry transport can replay it.
//   - Trace: at TRACE the request is dumped as its headers and as much of
//     the start of its body as a trace shows, and the response as its
//     headers and as much of the start of its body as do kept, which a large
//     body is not held whole for.
//   - No response: a request that got no answer returns a nil response and
//     the error.
//   - Any status outside 2xx: the body is not decoded, and the error is a
//     *StatusError carrying the status, the URL and the start of the body,
//     which errors.Is matches against ErrNotFound for a 404 and ErrConflict
//     for a 409. Only the first maxErrorBody+1 bytes of the body are read,
//     which is all the error quotes, and a body cut short still makes a
//     StatusError, with what was read.
//   - Status 204, or any 2xx with an empty body: success, and out is left as
//     it was. The reply says it was Empty; get and send, which are asked for
//     a value, take that for an error.
//   - Login page: a 2xx that is a login page rather than Confluence's answer
//     returns an error errors.Is matches against ErrLoginPage, with the
//     response; see loginEvidence for how one is recognised. A Confluence
//     behind SSO answers an API call with one, and the error says so rather
//     than failing to parse it.
//   - Any other 2xx: decoded into out, as it streams in, when its
//     Content-Type is JSON or there is none; one that names anything else
//     is an error, and so is a body that is not one JSON value and nothing
//     after it -- though not one whose connection fails once the value has
//     come whole. With out nil nothing is decoded or looked at. Either way
//     the error comes with the response, as does one reading a body that
//     cannot be read in full.
//   - Cancellation: any error a request ends with while ctx is done -- no
//     response, a body cut short, or one that does not decode -- is marked
//     so that IsCancellation recognises it, and errors.Is finds ctx.Err() in
//     it. Only for a GET or a HEAD, though. Any other method is not sent once
//     ctx is done, and once sent is not cut short by it: it runs to its
//     answer, bounded by the transport's timeouts. The wait before sending
//     one again after a 429 is cut short, and ends it as a cancellation.
func (c *client) do(
	ctx context.Context,
	header http.Header,
	method string,
	path []string,
	query url.Values,
	body, out any,
) (*reply, error) {
	if c == nil {
		return nil, errNoClient
	}

	if c.baseErr != nil {
		return nil, fmt.Errorf("invalid base URL: %w", c.baseErr)
	}

	payload, bodyHeader, err := encodeBody(body)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	// A write is not started once the run is stopped, and not cut short once
	// it is sent: Confluence may already have applied it, and a run that then
	// read the answer as a failure would not record what the write did --
	// under --no-overwrite, a page version it never wrote down reads as an
	// edit made in Confluence on every later run. From here on ctx is the
	// request's own, so the checks below that ask whether the run was stopped
	// ask of a write whether it was, which it never is.
	//
	// Only the exchange is shielded, though. The wait before the retry
	// transport sends a write again -- after a 429, which Confluence answers
	// before acting -- is on the caller's context still, so a stopped run
	// does not sit out a throttled tenant's Retry-After for every write.
	caller := ctx
	if !readOnly(method) {
		if err := ctx.Err(); err != nil {
			return nil, cancellation(ctx, err)
		}

		ctx = context.WithValue(context.WithoutCancel(ctx), backoffContextKey{}, caller)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path, query), reader)
	if err != nil {
		return nil, err
	}

	maps.Copy(req.Header, bodyHeader)

	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	} else if c.password != "" {
		req.Header.Set("Authorization", "Bearer "+c.password)
	}

	maps.Copy(req.Header, header)

	if c.trace != nil {
		c.traceRequest(req, payload)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// A write given up on in its backoff ends with the caller's error.
		if stopped := caller.Err(); stopped != nil && errors.Is(err, stopped) {
			return nil, cancellation(caller, err)
		}

		return nil, cancellation(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	answer := &reply{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Request:    resp.Request,
	}

	// Only the start of the body is kept, for an error to quote and a trace
	// to show; a success is decoded as it streams in, and without a trace
	// nothing of it is kept.
	limit := 0
	if !successful(resp.StatusCode) {
		limit = maxErrorBody + 1
	}
	if c.trace != nil {
		limit = traceDumpLimit
	}
	received := &capture{reader: resp.Body, limit: limit}

	err = c.answer(ctx, req, resp, received, out)
	answer.Body = received.prefix
	answer.Empty = received.total == 0

	if c.trace != nil {
		c.traceResponse(resp, received)
	}

	if !successful(resp.StatusCode) {
		// A failure status is an answer even when its body could not be read
		// to the end: callers act on the status, and the bytes that did
		// arrive are only ever quoted. Unless the run was stopped, which is
		// what a body cut short then means.
		if err != nil && ctx.Err() != nil {
			return answer, cancellation(ctx, err)
		}

		return answer, newStatusError(answer)
	}

	if err == nil {
		return answer, nil
	}

	// A login page names its answer whatever becomes of it next: one that
	// arrived as the run was stopped is still worded, as a cancellation.
	var login *loginPageError
	isLogin := errors.As(err, &login)
	if isLogin {
		login.answer = answer
	}

	if ctx.Err() != nil {
		return answer, cancellation(ctx, err)
	}

	if isLogin {
		return answer, err
	}

	return answer, &bodyError{answer: answer, err: err}
}

// answer reads as much of resp's body as it takes to answer req, through
// body: the start of a failure, the whole of a success decoded into out, or
// the start of anything else. The error is the read's for a failure status,
// which do turns into a *StatusError, and otherwise the reason the answer is
// not one.
func (c *client) answer(ctx context.Context, req *http.Request, resp *http.Response, body *capture, out any) error {
	if !successful(resp.StatusCode) {
		// No more than a StatusError quotes, plus the one byte that tells it
		// the body went on. What answers with an error is not always
		// Confluence: a proxy's HTML error page can run to megabytes, which
		// would otherwise all be held in memory only to be cut down. The rest
		// is not drained; closing the body drops the connection.
		return body.readPrefix(maxErrorBody + 1)
	}

	// Nothing to decode, and nothing to read: success, with out as it was.
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	mediaType := contentMediaType(resp.Header)

	if evidence, strong := loginEvidence(req, resp, mediaType); evidence != "" {
		_ = body.readPrefix(body.limit)

		return &loginPageError{requested: req.URL.Redacted(), evidence: evidence, strong: strong}
	}

	if resp.ContentLength == 0 {
		return nil
	}

	// Nothing asked for, nothing to object to -- beyond a body that cannot be
	// read at all.
	if out == nil {
		return body.readPrefix(1)
	}

	// A target that takes JSON under any name for it is decoded whatever the
	// Content-Type says; what is not JSON still fails to decode.
	lenient, anyType := out.(*whateverContentType)
	if anyType {
		out = lenient.out
	}

	if mediaType != "" && !isJSONMediaType(mediaType) && !anyType {
		err := body.readPrefix(max(body.limit, 1))
		// An empty body is success whatever it says it is.
		if body.total == 0 {
			return err
		}

		return fmt.Errorf("its Content-Type is %s, not JSON", mediaType)
	}

	decoder := json.NewDecoder(body)
	if err := decoder.Decode(out); err != nil {
		// An empty body is success, with out as it was. One with nothing but
		// white space in it is not JSON.
		if errors.Is(err, io.EOF) && body.total == 0 {
			return nil
		}

		return err
	}

	// One JSON value is the whole of an answer. Anything after it is a body
	// that is not one, however well its start parsed.
	_, err := decoder.Token()
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil && ctx.Err() != nil:
		return err
	case body.readErr != nil:
		// Not something after the value, but the connection failing after
		// it -- a proxy closing it before the end of a chunked body. The
		// value came whole, and the request, a write as much as a read, did
		// what it was asked: failing it as a body that is not JSON would
		// fail a write that happened, and put the blame on an SSO page.
		return nil //nolint:nilerr // the value is whole; a read failing after it is no fault in it.
	default:
		return errors.New("it goes on after the JSON value")
	}
}

// whateverContentType is a decode target for an answer that is JSON under
// whatever Content-Type it is sent with: do decodes into out as it would under
// application/json, and a body that is not JSON is the same error as ever. A
// login page is still recognised first.
//
// For movepage.action, which is an XWork action rather than a REST endpoint:
// it answers JSON when asked for it, but under no promise of what it calls
// it, and its refusal is only in that JSON.
type whateverContentType struct {
	out any
}

// capture is a response body read through, keeping the first limit bytes of
// it and counting the rest, so that only that much of a body is ever held,
// however large it is.
type capture struct {
	reader io.Reader
	limit  int

	prefix []byte
	total  int64

	// done is set once the body has been read to its end.
	done bool

	// readErr is what reading the body failed with, other than its end.
	readErr error
}

func (c *capture) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if room := c.limit - len(c.prefix); room > 0 {
		c.prefix = append(c.prefix, p[:min(n, room)]...)
	}
	c.total += int64(n)
	switch {
	case errors.Is(err, io.EOF):
		c.done = true
	case err != nil:
		c.readErr = err
	}

	return n, err
}

// readPrefix reads up to n bytes, to be quoted or traced.
func (c *capture) readPrefix(n int) error {
	_, err := io.Copy(io.Discard, io.LimitReader(c, int64(n)))

	return err
}

// traceRequest dumps the headers of req and the start of its payload: no
// more of it than a trace shows is copied into the dump.
func (c *client) traceRequest(req *http.Request, payload []byte) {
	head, err := httputil.DumpRequest(req, false)
	if err != nil {
		c.trace.Printf("dump request failed: %s", err)

		return
	}

	shown := payload[:min(len(payload), traceDumpLimit)]
	c.trace.write(boundTraceDumpMore(append(head, shown...), int64(len(payload)-len(shown))))
}

// traceResponse dumps the headers of resp and the start of its body, which
// is all of the body there is to dump: a success has been decoded as it
// arrived, rather than held so that it could be traced.
func (c *client) traceResponse(resp *http.Response, body *capture) {
	head, err := httputil.DumpResponse(resp, false)
	if err != nil {
		c.trace.Printf("dump response failed: %s", err)

		return
	}

	dump := append(head, body.prefix...)
	rest := body.total - int64(len(body.prefix))

	if body.done {
		c.trace.write(boundTraceDumpMore(dump, rest))
	} else {
		c.trace.write(boundTraceDump(dump) + " (the rest of the body was not read)")
	}
}

// get asks c for path and decodes the answer as a T: do for the request most
// calls make. An empty answer is an error, not a zero T.
func get[T any](ctx context.Context, c *client, path []string, query url.Values) (T, error) {
	var out T
	answer, err := c.do(ctx, nil, http.MethodGet, path, query, nil, &out)

	return out, valueIn(answer, &out, err)
}

// listing is one page of a collection, as v1 and v2 both answer a listing:
// the results, and the links that say where it came from and whether more
// follow.
type listing[T any] struct {
	Results []T `json:"results"`

	Links struct {
		// Next is the next page, empty on the last one -- or, on some
		// deployments, on every one.
		Next string `json:"next"`

		// Context and Base are the site's context path and URL.
		Context string `json:"context"`
		Base    string `json:"base"`
	} `json:"_links"`
}

// send sends body to path with method and decodes the answer as a T. An
// empty answer is an error, not a zero T.
func send[T any](ctx context.Context, c *client, method string, path []string, body any) (T, error) {
	var out T
	answer, err := c.do(ctx, nil, method, path, nil, body, &out)

	return out, valueIn(answer, &out, err)
}

// valueIn is err, or, for a success that is no value, an *emptyAnswerError.
// do takes an empty 2xx for success, which it is for a call that wants nothing
// back. One that wants a T does not get one, and a zero T in its place -- a
// page whose ID is "" -- is taken for an answer: put in the page cache, and
// requests then sent to "content/".
//
// Nor is a body with nothing in it the only way to answer with no value:
// "null" and "{}" decode, into a T as empty as no body at all. So a T that is
// named by an ID -- a page, a folder -- must come with one.
func valueIn[T any](answer *reply, out *T, err error) error {
	if err != nil || answer == nil {
		return err
	}

	if answer.Empty {
		return &emptyAnswerError{answer: answer}
	}

	if named, ok := any(out).(identified); ok && named.answeredID() == "" {
		return &emptyAnswerError{answer: answer, noID: true}
	}

	return nil
}

// identified is a value Confluence names by an ID, which an answer that is one
// always carries.
type identified interface {
	answeredID() string
}

func (p *PageInfo) answeredID() string   { return p.ID }
func (f *FolderInfo) answeredID() string { return f.ID }
func (c *contentV2) answeredID() string  { return c.ID }

// cancelledError is an error a request ended with while its context was
// done. It reads as err, which net/http already words as a cancellation, and
// unwraps to both err and the context's own error, so errors.Is finds
// context.Canceled or context.DeadlineExceeded in it whatever err was -- a
// body cut short, or that body failing to decode.
type cancelledError struct {
	err, ctxErr error
}

func (e *cancelledError) Error() string { return e.err.Error() }

func (e *cancelledError) Unwrap() []error { return []error{e.err, e.ctxErr} }

// cancellation marks err as a cancellation when ctx is done, and returns it
// unchanged otherwise. A nil err stays nil: an exchange that completed is an
// answer, however soon after it the context ended.
func cancellation(ctx context.Context, err error) error {
	ctxErr := ctx.Err()
	if err == nil || ctxErr == nil {
		return err
	}

	return &cancelledError{err: err, ctxErr: ctxErr}
}

// IsCancellation reports whether err is the caller giving up rather than
// Confluence answering: a request, or a wait between requests, that ended
// because its context was done. Nothing that remembers an answer for later
// -- a cache, a flag that an endpoint is missing, the platform -- may
// remember one of these.
//
// It asks how the error was made, not what it wraps. net/http's "timeout
// awaiting response headers" and a dial's "i/o timeout" both satisfy
// errors.Is(err, context.DeadlineExceeded), and neither is the run being
// stopped. And it asks the context the request carried, not whichever one
// the caller holds by the time the error is looked at.
func IsCancellation(err error) bool {
	var cancelled *cancelledError

	return errors.As(err, &cancelled)
}

// ErrLoginPage reports that a request was answered with a login page instead
// of by Confluence: an SSO proxy, Seraph turning the credentials away, or an
// identity provider the request was redirected to. errors.Is finds it in the
// error do returns for such an answer, which also says where the request
// ended up.
var ErrLoginPage = errors.New("a login page answered in place of Confluence")

// loginPageError is the error for a 2xx do recognised as a login page.
type loginPageError struct {
	answer *reply

	// requested is the URL asked, redacted; the answer's own request is the
	// one a redirect ended on.
	requested string

	// evidence says how the answer was recognised, for the message; see
	// loginEvidence.
	evidence string

	// strong is whether evidence is more than an HTML Content-Type, the
	// weakest of it: an action that redirects to the page it acted on lands
	// on HTML too; see moveByAction.
	strong bool
}

func (e *loginPageError) Error() string {
	if e.answer == nil {
		return fmt.Sprintf("%s answered with a login page (%s)", e.requested, e.evidence)
	}

	landed := requestTarget(e.answer)
	if landed != e.requested {
		return fmt.Sprintf(
			"%s was redirected to %s, which answered %s with a login page (%s)",
			e.requested, landed, e.answer.Status, e.evidence,
		)
	}

	return fmt.Sprintf("%s answered %s with a login page (%s)", landed, e.answer.Status, e.evidence)
}

func (e *loginPageError) Is(target error) bool { return target == ErrLoginPage }

// loginEvidence says why a 2xx answer to req is a login page rather than
// Confluence's own answer, or "" when it is not one, and whether that is
// strong evidence: anything but an HTML Content-Type alone. mediaType is the
// answer's, as contentMediaType reads it.
//
// Confluence's API answers in JSON, so an answer in HTML is a page for a
// person: an SSO proxy's login form, or the page some proxy or redirect put
// in front of the API. Beyond that, whatever the answer says it is, a request
// that ended on Seraph's /login.action was sent to log in; and one that was
// redirected to another host, or that Seraph says it did not authenticate,
// was turned away unless it answered in JSON.
func loginEvidence(req *http.Request, resp *http.Response, mediaType string) (evidence string, strong bool) {
	final := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL
	}

	if strings.HasSuffix(final.Path, "/login.action") {
		return "redirected to /login.action", true
	}

	if !isJSONMediaType(mediaType) {
		if !strings.EqualFold(final.Hostname(), req.URL.Hostname()) {
			return "redirected to " + final.Hostname(), true
		}
		if reason := resp.Header.Get("X-Seraph-LoginReason"); reason != "" && reason != "OK" {
			return "X-Seraph-LoginReason: " + reason, true
		}
		if resp.Header.Get("X-AUSERNAME") == "anonymous" {
			return "X-AUSERNAME: anonymous", true
		}
	}

	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return "an HTML Content-Type", false
	}

	return "", false
}

// contentMediaType is the media type of a Content-Type header, lowercased and
// without parameters, or "" when there is none.
func contentMediaType(header http.Header) string {
	value := header.Get("Content-Type")
	if value == "" {
		return ""
	}

	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		mediaType, _, _ = strings.Cut(value, ";")
	}

	return strings.ToLower(strings.TrimSpace(mediaType))
}

// isJSONMediaType reports whether a media type is JSON: application/json, or
// a structured +json type.
func isJSONMediaType(mediaType string) bool {
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// successful reports whether a status is a 2xx, the only answers do does not
// turn into a *StatusError. Go's client follows redirects on its own, so a 3xx
// that reaches do is one it could not follow, and no answer either.
func successful(status int) bool {
	return status >= 200 && status <= 299
}

// errNoClient is what a request made through an API that NewAPI did not
// build answers, rather than a nil pointer dereference.
var errNoClient = errors.New("no Confluence client to send the request with: the API was not made by NewAPI")

// hostname is the host of the base, or "" for no client at all.
func (c *client) hostname() string {
	if c == nil {
		return ""
	}

	return c.base.Hostname()
}

// url joins path and query onto the base.
func (c *client) url(path []string, query url.Values) string {
	escaped := make([]string, len(path))
	for i, segment := range path {
		escaped[i] = url.PathEscape(segment)
	}

	u := *c.base
	u.Path = joinPath(c.base.Path, strings.Join(path, "/"))
	u.RawPath = joinPath(c.base.EscapedPath(), strings.Join(escaped, "/"))
	u.RawQuery = query.Encode()

	return u.String()
}

func joinPath(base, rest string) string {
	if base == "" {
		return rest
	}

	return base + "/" + rest
}

// encodeBody turns a request body into bytes and the headers that go with
// them; see do.
func encodeBody(body any) ([]byte, http.Header, error) {
	switch body := body.(type) {
	case nil:
		return nil, nil, nil
	case *form:
		return body.buffer.Bytes(), http.Header{
			"Content-Type":      {body.writer.FormDataContentType()},
			"X-Atlassian-Token": {"no-check"},
		}, nil
	case io.Reader:
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to read request body: %w", err)
		}
		return data, nil, nil
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to encode request body: %w", err)
		}
		return data, http.Header{"Content-Type": {"application/json"}}, nil
	}
}
