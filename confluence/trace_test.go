package confluence

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturedTrace sends everything logged at TRACE, for the rest of the test,
// to the buffer it returns.
func capturedTrace(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buffer bytes.Buffer

	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buffer)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	return &buffer
}

// traced captures what the tracer writes at TRACE.
func traced(t *testing.T, format string, args ...any) string {
	t.Helper()

	buffer := capturedTrace(t)

	(&tracer{"rest:"}).Printf(format, args...)

	return buffer.String()
}

// TestTraceDoesNotWriteTheCredential: the trace dumps the whole request, and
// the request carries the token. "--log-level TRACE" is the natural thing to
// ask a bug reporter for, so whatever it writes ends up in issue threads.
func TestTraceDoesNotWriteTheCredential(t *testing.T) {
	dump := "GET /rest/api/space/DOCS HTTP/1.1\r\n" +
		"Host: example.atlassian.net\r\n" +
		"Authorization: Basic dXNlcjp0b2tlbg==\r\n" +
		"Cookie: JSESSIONID=8FA1B2C3\r\n" +
		"Accept: application/json\r\n\r\n"

	out := traced(t, "%s", dump)

	assert.NotContains(t, out, "dXNlcjp0b2tlbg==")
	assert.NotContains(t, out, "8FA1B2C3")

	// Which headers were sent, and that one carried credentials at all, is what
	// somebody reading a trace is trying to establish.
	assert.Contains(t, out, "Authorization: <redacted>")
	assert.Contains(t, out, "Cookie: <redacted>")
	assert.Contains(t, out, "Accept: application/json")
	assert.Contains(t, out, "Host: example.atlassian.net")
}

// TestTraceRedactsASetCookieResponse: the response dump carries the session
// back the other way.
func TestTraceRedactsASetCookieResponse(t *testing.T) {
	dump := "HTTP/1.1 200 OK\r\n" +
		"Set-Cookie: JSESSIONID=SECRETSESSION; Path=/\r\n\r\n{}"

	out := traced(t, "%s", dump)

	assert.NotContains(t, out, "SECRETSESSION")
	assert.Contains(t, out, "Set-Cookie: <redacted>")
}

// TestTraceLeavesABodyPercentAlone: a dump is arbitrary bytes, not a format
// string.
func TestTraceLeavesABodyPercentAlone(t *testing.T) {
	out := traced(t, "%s", "HTTP/1.1 200 OK\r\n\r\n{\"title\":\"100% done\"}")

	assert.Contains(t, out, "100% done")
	assert.NotContains(t, out, "%!")
}

func TestRedactHeadersLeavesABodyLineAlone(t *testing.T) {
	// A JSON body naming a header is not a header.
	body := "HTTP/1.1 200 OK\r\n\r\n{\"authorization\": \"kept\"}"

	assert.Contains(t, redactHeaders(body), `"authorization": "kept"`)
}

// TestTraceBoundsALargeDump: a body is traced as one line, and a line of
// megabytes cost a CI runner half an hour to ingest while telling the reader
// nothing the start of it does not. This goes through resty's debug log, which
// is what writes the dump, with a large body in both directions: each is cut
// to its start, and the response still makes it into the line after a request
// body that alone would have filled it.
func TestTraceBoundsALargeDump(t *testing.T) {
	large := strings.Repeat("x", 1<<20)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "JSESSIONID=SECRETSESSION; Path=/")
		_, _ = fmt.Fprintf(w, `{"title":"start of the response %s"}`, large)
	}))
	t.Cleanup(server.Close)

	out := capturedTrace(t)

	client := newRestClient(server.Client(), server.URL, "user", "token", "rest:")

	var result map[string]any
	response, err := client.R().
		SetResult(&result).
		SetBody(map[string]string{"value": "start of the request " + large}).
		Put("/content/1")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode())
	require.Len(t, result["title"], len("start of the response ")+len(large),
		"the bound is on the trace, not on what the caller reads")

	line := out.String()

	assert.Less(t, len(line), 2*traceDumpLimit+8<<10)
	assert.Contains(t, line, "start of the request")
	assert.Contains(t, line, "RESPONSE")
	assert.Contains(t, line, "200 OK")
	assert.Contains(t, line, "start of the response")
	assert.Equal(t, 2, strings.Count(line, "more bytes not traced"))

	assert.NotContains(t, line, "SECRETSESSION")
	assert.Contains(t, line, "Authorization: <redacted>")
}

// TestTraceBoundsALine: whatever else resty logs through the tracer is held
// to a bound of its own, so no single line can run away.
func TestTraceBoundsALine(t *testing.T) {
	dump := "HTTP/1.1 502 Bad Gateway\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		strings.Repeat("x", 1<<20)

	out := traced(t, "%s", dump)

	assert.Less(t, len(out), traceLineLimit+1024)
	assert.Contains(t, out, "HTTP/1.1 502 Bad Gateway")
	assert.Contains(t, out, "more bytes not traced")
}

// TestTraceCutsOnACharacterBoundary: the cut must not split a character, or
// the line carries a broken one.
func TestTraceCutsOnACharacterBoundary(t *testing.T) {
	dump := strings.Repeat("a", traceDumpLimit-1) + strings.Repeat("é", 10)

	bounded := boundTraceDump(dump)

	assert.True(t, utf8.ValidString(bounded))
	assert.True(t, strings.HasPrefix(bounded, strings.Repeat("a", traceDumpLimit-1)+"... ("))
}

// TestTraceKeepsASmallDumpWhole: the bound is for runaway bodies, not for the
// ordinary JSON a trace is read for.
func TestTraceKeepsASmallDumpWhole(t *testing.T) {
	dump := "HTTP/1.1 200 OK\r\n\r\n{\"id\":\"1\"}"

	assert.Equal(t, dump, boundTraceDump(dump))
}
