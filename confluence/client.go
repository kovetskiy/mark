package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	// basic is set when mark logs in with a username; bearer is the Personal
	// Access Token otherwise. At most one of them is set.
	basic  *basicAuth
	bearer string

	// trace is the prefix of this client's request and response dumps at
	// TRACE, or nil when they are not written.
	trace *tracer
}

type basicAuth struct {
	username string
	password string
}

// newClient roots a client at base.
func newClient(base string, httpClient *http.Client, username, password string, trace string) *client {
	c := &client{http: httpClient}

	c.base, c.baseErr = url.Parse(base)
	if c.baseErr != nil {
		c.base = &url.URL{}
	}

	if username != "" {
		c.basic = &basicAuth{username: username, password: password}
	} else {
		c.bearer = password
	}

	if trace != "" {
		c.trace = &tracer{trace}
	}

	return c
}

// requestOption adjusts one request, and that request only.
type requestOption func(*http.Request)

// withHeader sets a header on the request, over any do sets itself.
func withHeader(key, value string) requestOption {
	return func(req *http.Request) {
		req.Header.Set(key, value)
	}
}

// reply is what do hands back of an answer: the parts of an http.Response
// mark looks at, with the body already read in full and closed, so there is
// nothing left for a caller to close.
type reply struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte

	// Request is the request that got this answer -- after redirects, the
	// last one.
	Request *http.Request
}

// do sends one request and hands back the response, its body read in full, so
// that a caller can check the status and read the body whatever happened to
// it here.
//
// The behaviours below are the ones gopencils had and mark's error handling
// is built around; they are kept on purpose.
//
//   - Path: each element of path is escaped as one segment and joined to the
//     base path with "/". An empty last element leaves a trailing slash, as
//     "content/" always has. The base's own query is dropped.
//   - Query: encoded with url.Values.Encode, so keys are sorted and spaces
//     become "+". A nil or empty query sends none.
//   - Auth: basic auth when a username was given, otherwise "Authorization:
//     Bearer" with the token, and nothing when that is empty too.
//   - Headers: none by default, beyond what net/http adds. A JSON body is
//     announced as application/json; anything else a request needs, such as
//     a multipart Content-Type, comes as an option and overrides that.
//   - Body: an io.Reader is sent as it is; any other non-nil value is sent as
//     its JSON encoding. Either way it is buffered, so the retry transport can
//     replay it.
//   - Trace: at TRACE the request and the response are dumped, bodies
//     included, through the same tracer and prefixes as before.
//   - No response: a request that got no answer returns a nil response and
//     the error. newTransportError reads the nil as "never completed".
//   - Status 400 and above: the body is not decoded and the error is nil, so
//     newErrorStatusNotOK can quote it.
//   - Status 204: nothing is decoded.
//   - Any other status: the body is decoded into out when out is non-nil,
//     whatever its Content-Type says. A body that is not JSON, or is empty,
//     returns the decode error *together with* the response: that is how an
//     SSO login page answering 200 is told apart from a network failure, by
//     newTransportError and by moveByAction's login-page check alike. Only
//     the first JSON value is read; anything after it is ignored.
//   - A body that cannot be read in full is the same: the response and the
//     read error.
func (c *client) do(
	ctx context.Context,
	method string,
	path []string,
	query url.Values,
	body, out any,
	options ...requestOption,
) (*reply, error) {
	if c.baseErr != nil {
		return nil, fmt.Errorf("invalid base URL: %w", c.baseErr)
	}

	payload, contentType, err := encodeBody(body)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path, query), reader)
	if err != nil {
		return nil, err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if c.basic != nil {
		req.SetBasicAuth(c.basic.username, c.basic.password)
	} else if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}

	for _, option := range options {
		option(req)
	}

	if c.trace != nil {
		dump, err := httputil.DumpRequest(req, true)
		if err != nil {
			c.trace.Printf("dump request failed: %s", err)
		} else {
			c.trace.Printf("%s", dump)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if c.trace != nil {
		// DumpResponse reads the body it dumps, and the real one is spent.
		resp.Body = io.NopCloser(bytes.NewReader(data))
		dump, dumpErr := httputil.DumpResponse(resp, true)
		if dumpErr != nil {
			c.trace.Printf("dump response failed: %s", dumpErr)
		} else {
			c.trace.Printf("%s", dump)
		}
	}

	answer := &reply{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header,
		Body:       data,
		Request:    resp.Request,
	}

	if err != nil {
		return answer, err
	}

	if resp.StatusCode >= http.StatusBadRequest || resp.StatusCode == http.StatusNoContent || out == nil {
		return answer, nil
	}

	return answer, json.NewDecoder(bytes.NewReader(data)).Decode(out)
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

// encodeBody turns a request body into bytes and the Content-Type that names
// them; see do.
func encodeBody(body any) ([]byte, string, error) {
	switch body := body.(type) {
	case nil:
		return nil, "", nil
	case io.Reader:
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, "", fmt.Errorf("unable to read request body: %w", err)
		}
		return data, "", nil
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", fmt.Errorf("unable to encode request body: %w", err)
		}
		return data, "application/json", nil
	}
}
