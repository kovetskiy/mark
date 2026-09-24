package confluence

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/rs/zerolog"
	"resty.dev/v3"
)

// newRestClient builds the resty client for one API root, /rest/api or
// /api/v2.
//
// Every client is built on the one http.Client newHTTPClient returns, so the
// two share retryTransport, the phase timeouts and the cookie jar. Resty's own
// retry stays off -- a retry count of zero, which is also its default, stated
// here so that nobody turns it on without reading this: its default conditions
// replay any method on a 5xx, where retryTransport is method-aware and never
// replays a POST that may already have created a page.
//
// The middlewares and codecs installed here keep the requests and the
// handling of responses what they were before resty; see each of them.
func newRestClient(httpClient *http.Client, baseURL, username, password, prefix string) *resty.Client {
	client := resty.NewWithClient(httpClient).
		SetBaseURL(baseURL).
		SetRetryCount(0).
		// Always installed, not only at TRACE: resty's default logger writes
		// to stderr, and it warns there about credentials sent over plain
		// HTTP. Everything resty has to say goes to TRACE instead.
		SetLogger(&tracer{prefix}).
		SetRequestMiddlewares(
			prepareRequest,
			resty.MiddlewareRequestCreate,
			replayableBody,
		).
		AddResponseMiddleware(closeUnreadBody).
		AddContentTypeEncoder("json", encodeJSON).
		AddContentTypeDecoder("json", decodeJSON)

	// A Personal Access Token arrives as the password with no username. Resty
	// copies the client's credentials onto each request it builds and never
	// writes a response's headers back, so the token can live on the client.
	// An empty token sends no Authorization header at all.
	if username != "" {
		client.SetBasicAuth(username, password)
	} else {
		client.SetAuthToken(password)
	}

	// The whole request and response, bodies included, as the trace always
	// had them. tracer redacts the credentials on the way out.
	if zerolog.GlobalLevel() == zerolog.TraceLevel {
		client.SetDebug(true)
	}

	return client
}

// prepareRequest runs before resty builds the http.Request, and gives it the
// shape every call in this package was written against.
//
//   - The path is escaped, as it was when it went into url.URL.Path. Resty
//     parses a request's path as a URL instead, so a property key, a space key
//     or an id carrying a '?', a '#' or a '%' would otherwise be cut short or
//     misread rather than sent as the path segment it is.
//   - A body goes out as application/json, without resty's charset suffix.
//   - A response is decoded as JSON whatever Content-Type it declares. A proxy
//     or an SSO shim in front of Confluence can relabel a JSON body as
//     text/html; resty would then leave the result empty and report success,
//     and an HTML login page answering 200 would decode as nothing instead of
//     failing loudly through newTransportError.
func prepareRequest(_ *resty.Client, request *resty.Request) error {
	request.URL = (&url.URL{Path: request.URL}).EscapedPath()

	if request.Body != nil && request.Header.Get("Content-Type") == "" {
		request.SetHeader("Content-Type", "application/json")
	}

	request.SetResponseForceContentType("application/json")

	return nil
}

// replayableBody buffers a request body that net/http could not rewind.
//
// Resty streams a multipart body through a pipe, which leaves the request with
// no Content-Length and no GetBody -- and retryTransport, rightly, does not
// replay what it cannot rewind, so an attachment upload that drew a 429 would
// have failed where it used to be retried. The attachment is already in memory
// by the time it gets here, as it always was, so holding the encoded form too
// costs what the hand-built form it replaces did.
func replayableBody(_ *resty.Client, request *resty.Request) error {
	raw := request.RawRequest
	if raw == nil || raw.Body == nil || raw.Body == http.NoBody || raw.GetBody != nil {
		return nil
	}

	body, err := io.ReadAll(raw.Body)
	_ = raw.Body.Close()
	if err != nil {
		return err
	}

	raw.Body = io.NopCloser(bytes.NewReader(body))
	raw.ContentLength = int64(len(body))
	raw.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	return nil
}

// closeUnreadBody closes every response body resty did not consume, keeping
// the first maxErrorBody+1 bytes of it for newErrorStatusNotOK.
//
// Resty decodes a 2xx body into the result and closes it, and leaves any other
// body open for the caller. Several callers here read a status as an answer --
// a 404 meaning "not there", a 403 meaning "Cloud" -- and never touch the
// body, which would leave the connection behind it unusable and never
// released. Reading only a bounded prefix also means an error page of any size
// costs the few kilobytes that are quoted, not all of it.
func closeUnreadBody(_ *resty.Client, response *resty.Response) error {
	if response.IsRead || response.Body == nil {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBody+1))
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))

	return err
}

// encodeJSON encodes a request body exactly as json.Marshal does. Resty's own
// encoder goes through json.Encoder, which appends a newline.
func encodeJSON(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}

	_, err = w.Write(body)

	return err
}

// decodeJSON decodes one JSON value from a response body.
//
// Resty's own decoder reads values until EOF and treats an empty body as
// success. A 2xx that carries no JSON at all is not something any call here
// expects, and it is reported, as it always was, rather than handed back as a
// zero value that looks like a real answer.
func decodeJSON(r io.Reader, v any) error {
	if err := json.NewDecoder(r).Decode(v); err != nil {
		return err
	}

	// Whatever follows the value is read off, within reason, so that the
	// connection can go back to the pool.
	_, _ = io.Copy(io.Discard, io.LimitReader(r, maxErrorBody))

	return nil
}
