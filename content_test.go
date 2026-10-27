package mark

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	err := Run(context.Background(), publishConfig(server.URL, file))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not well-formed XML")
	assert.Contains(t, err.Error(), "unclosed tag")

	// The page itself is created during ancestry resolution, before the body is
	// compiled, so what the gate protects is the content: the page is left
	// empty rather than filled with markup Confluence would reject.
	assert.Empty(t, bodyOfPageTitled(t, server, "Malformed"),
		"no body should have been uploaded")
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

	require.NoError(t, Run(context.Background(), publishConfig(server.URL, file)))
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

	err := Run(context.Background(), config)
	require.Error(t, err)

	assert.NotEmpty(t, bodyOfPageTitled(t, server, "Good"),
		"the well-formed document should still publish")
	assert.Empty(t, bodyOfPageTitled(t, server, "Bad"),
		"the malformed one should have uploaded no body")
}

// TestCompileOnlyRefusesAMalformedBody: validating documents in CI without
// Confluence credentials is the whole of what --compile-only is for, and the
// gate ran only on the path that uploads -- so the mode that exists to catch
// this exited zero and the failure was found at publish time instead.
func TestCompileOnlyRefusesAMalformedBody(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Title: Malformed -->

Text with an <span>unclosed tag.
`)

	err := Run(context.Background(), Config{
		Files: file, CompileOnly: true,
		Features: []string{"mention"}, Output: io.Discard,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not well-formed XML")
}

// TestCompileOnlyAcceptsAWellFormedBody is the control: the check must not
// stand in the way of the markup mark itself emits.
func TestCompileOnlyAcceptsAWellFormedBody(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Title: Fine -->

A & B, "quoted", 3 < 4, and a table:

| a | b |
|---|---|
| 1 | 2 |
`)

	require.NoError(t, Run(context.Background(), Config{
		Files: file, CompileOnly: true,
		Features: []string{"mention"}, Output: io.Discard,
	}))
}

// writeWithBOM writes a document behind a UTF-8 byte-order mark, the way a
// Windows editor set to "UTF-8 with BOM" does.
func writeWithBOM(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, content...), 0o600))
	return path
}

// storedBody returns what is actually stored on the page carrying a title.
func storedBody(t *testing.T, server *confluencetest.Server, title string) string {
	t.Helper()
	found, err := confluence.NewAPI(server.URL, "user", "token", false).
		FindPage(context.Background(), "DOCS", title, "page")
	require.NoError(t, err)
	if found == nil {
		return ""
	}
	return server.Page(found.ID).Body
}

// TestByteOrderMarkDoesNotHideMetadata: a BOM in front of the first header
// comment made every parser here see a file with no metadata at all, and the
// run failed with "doesn't contain metadata" -- pointing away from the headers,
// which are present and correct. "UTF-8 with BOM" is a one-click setting in VS
// Code, and PowerShell redirection produces one without being asked.
func TestByteOrderMarkDoesNotHideMetadata(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	path := writeWithBOM(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: With BOM -->\n\nBody text.\n")

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: path, Features: []string{"mention"}, Output: io.Discard,
	}))

	assert.Equal(t, 1, countPagesTitled(t, server, "With BOM"))
	assert.NotContains(t, storedBody(t, server, "With BOM"), "\ufeff",
		"the mark itself must not reach the page")
}

// TestByteOrderMarkDoesNotHideFrontMatter covers the other metadata form. The
// BOM sits in front of the opening "---", so it stops being a front-matter
// fence and becomes a horizontal rule with the YAML published beneath it as
// body text.
func TestByteOrderMarkDoesNotHideFrontMatter(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	path := writeWithBOM(t, dir, "doc.md",
		"---\nspace: DOCS\ntitle: BOM Front Matter\nparents: [ Parent ]\n---\n\nBody text.\n")

	require.NoError(t, Run(context.Background(), Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: path, Features: []string{"mention", "frontmatter"}, Output: io.Discard,
	}))

	assert.Equal(t, 1, countPagesTitled(t, server, "BOM Front Matter"))
	assert.NotContains(t, storedBody(t, server, "BOM Front Matter"), "space: DOCS",
		"the front matter must be read as metadata, not published as text")
}

// TestLinkToDocumentWithByteOrderMark: the document being published had its
// BOM stripped, a document it links to did not. The linked file's headers
// were invisible behind the mark, so the link was never rewritten to the page
// and was published pointing at other.md.
func TestLinkToDocumentWithByteOrderMark(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	header := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"
	other := writeWithBOM(t, dir, "other.md", header+"<!-- Title: Other -->\n\nx\n")
	doc := writeFile(t, dir, "doc.md", header+"<!-- Title: Doc -->\n\nSee [other](other.md).\n")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Features: []string{"mention"}, Output: io.Discard,
	}
	config.Files = other
	require.NoError(t, Run(context.Background(), config))
	config.Files = doc
	require.NoError(t, Run(context.Background(), config))

	body := storedBody(t, server, "Doc")
	assert.NotContains(t, body, `href="other.md"`)
	assert.Contains(t, body, `href="/wiki/x/`, "rewritten to the page it was published as")
}
