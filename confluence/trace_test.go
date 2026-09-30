package confluence

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

// traced captures what the tracer writes at TRACE.
func traced(t *testing.T, format string, args ...any) string {
	t.Helper()

	var buffer bytes.Buffer

	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buffer)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

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
// nothing the start of it does not.
func TestTraceBoundsALargeDump(t *testing.T) {
	dump := "HTTP/1.1 502 Bad Gateway\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		strings.Repeat("x", 1<<20)

	out := traced(t, "%s", dump)

	assert.Less(t, len(out), traceDumpLimit+1024)
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
