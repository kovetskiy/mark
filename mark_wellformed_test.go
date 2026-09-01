package mark

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publishConfig is a plain publish with no tracking, for tests about what
// reaches Confluence rather than about what mark remembers.
func publishConfig(url, files string) Config {
	return Config{
		BaseURL:  url,
		Username: "user",
		Password: "token",
		Files:    files,
		Features: []string{"mention"},
		Output:   io.Discard,
	}
}

// TestMalformedBodyIsRefusedBeforeUpload: storage format is XML, and Confluence
// answers a body that is not well-formed by rejecting the whole page with a
// BadRequestException that names nothing. Catching it here is the difference
// between a complaint the author can act on and one they cannot.
func TestMalformedBodyIsRefusedBeforeUpload(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	// An unbalanced tag the document wrote itself. Nothing downstream can
	// repair this one -- it is the author's to fix -- which is what makes it
	// the right shape for this test.
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Malformed -->

Some text with an <span>unclosed tag.
`)

	err := Run(publishConfig(server.URL, file))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not well-formed XML")
	assert.Contains(t, err.Error(), "unclosed tag")

	// The page itself is created during ancestry resolution, before the body is
	// compiled, so what the gate protects is the content: the page is left
	// empty rather than filled with markup Confluence would reject.
	assert.Empty(t, bodyOfPageTitled(t, server, "Malformed"),
		"no body should have been uploaded")
}

// bodyOfPageTitled returns what is actually stored on the page carrying a
// title, or "" if no page carries it.
func bodyOfPageTitled(t *testing.T, server *confluencetest.Server, title string) string {
	t.Helper()
	fresh := confluence.NewAPI(server.URL, "user", "token", false)
	found, err := fresh.FindPage("DOCS", title, "page")
	require.NoError(t, err)
	if found == nil {
		return ""
	}
	return server.Page(found.ID).Body
}

// TestWellFormedBodyPublishes is the control: the gate must not stand in the
// way of the markup mark itself emits.
func TestWellFormedBodyPublishes(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	// Deliberately busy: entities, a macro, a code block holding markup, and a
	// footnote -- the constructs whose output the check is most likely to
	// misread.
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Well Formed -->

A & B, "quoted", 3 < 4, and a non-breaking&nbsp;space.

| a | b |
|---|---|
| 1 | 2 |

`+"```go\nif a < b && c > d {}\n```"+`

A claim[^why].

[^why]: Because.
`)

	require.NoError(t, Run(publishConfig(server.URL, file)))
	assert.Equal(t, 1, countPagesTitled(t, server, "Well Formed"))
}

// TestMalformedBodyIsRefusedAcrossAFileSet: one bad document must not take the
// good ones with it, and must not be silently skipped either.
func TestMalformedBodyIsRefusedAcrossAFileSet(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a-good.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Good -->

Fine.
`)
	writeFile(t, dir, "b-bad.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Bad -->

An <span>unclosed tag.
`)

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.ContinueOnError = true

	err := Run(config)
	require.Error(t, err)

	assert.NotEmpty(t, bodyOfPageTitled(t, server, "Good"),
		"the well-formed document should still publish")
	assert.Empty(t, bodyOfPageTitled(t, server, "Bad"),
		"the malformed one should have uploaded no body")
}
