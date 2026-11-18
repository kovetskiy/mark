package header

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/kovetskiy/mark/v17/stdlib"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHeader(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func newStdlib(t *testing.T) *stdlib.Lib {
	t.Helper()

	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	return std
}

func TestNoPathIsNoHeader(t *testing.T) {
	header, err := Load(t.Context(), "", newStdlib(t), markdown.Options{})
	require.NoError(t, err)
	assert.Nil(t, header)

	html, attachments, err := header.Render(t.Context(), "doc.md", "Title", "SPACE", markdown.Options{})
	require.NoError(t, err)
	assert.Empty(t, html)
	assert.Empty(t, attachments)
}

func TestStorageFormatHeader(t *testing.T) {
	t.Chdir(t.TempDir())

	path := writeHeader(t, "header.html",
		`<p><a href="https://github.com/org/repo/blob/main/{{ .EscapedPath | xmlesc }}">{{ .Path | xmlesc }}</a> in {{ .Space }}: {{ .Title | xmlesc }}</p>`)

	header, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.NoError(t, err)

	html, _, err := header.Render(t.Context(), filepath.Join("docs", "my notes.md"), "A & B", "DOCS", markdown.Options{})
	require.NoError(t, err)
	assert.Equal(t,
		`<p><a href="https://github.com/org/repo/blob/main/docs/my%20notes.md">docs/my notes.md</a> in DOCS: A &amp; B</p>`,
		html)
}

func TestMarkdownHeader(t *testing.T) {
	path := writeHeader(t, "header.md", "> [!WARNING]\n> Generated from [{{ .Path | xmlesc }}](https://example.com/{{ .EscapedPath }}).\n\n# Not dropped\n")

	header, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.NoError(t, err)

	html, _, err := header.Render(t.Context(), "doc.md", "Title", "DOCS", markdown.Options{
		DropH1:   true,
		Features: []string{"mention"},
	})
	require.NoError(t, err)
	assert.Contains(t, html, `<ac:structured-macro ac:name="note">`)
	assert.Contains(t, html, `<a href="https://example.com/doc.md">doc.md</a>`)
	assert.Contains(t, html, `Not dropped</h1>`)
}

func TestMalformedStorageFormatHeaderFailsAtLoad(t *testing.T) {
	path := writeHeader(t, "header.html", `<p>unclosed`)

	_, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not well-formed XML")
}

// TestTemplateCannotReadTheEnvironment: the environment holds the Confluence
// password, and the header is published on every page.
func TestTemplateCannotReadTheEnvironment(t *testing.T) {
	path := writeHeader(t, "header.html", `<p>{{ env "MARK_PASSWORD" }}</p>`)

	_, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `function "env" not defined`)
}

func TestHeaderDoesNotJoinTheDocumentTemplates(t *testing.T) {
	std := newStdlib(t)
	path := writeHeader(t, "header.html", `<p>Header</p>`)

	_, err := Load(t.Context(), path, std, markdown.Options{})
	require.NoError(t, err)
	assert.Nil(t, std.Templates.Lookup("page-header"))
}

func TestFileOutsideWorkingDirectoryIsNamedByBaseName(t *testing.T) {
	t.Chdir(t.TempDir())

	outside := filepath.Join(t.TempDir(), "secret", "doc.md")

	assert.Equal(t, "doc.md", relativePath(outside))
	assert.Equal(t, "doc.md", relativePath(filepath.Join("..", "elsewhere", "doc.md")))
	assert.Equal(t, "docs/doc.md", relativePath(filepath.Join("docs", "doc.md")))
}

func TestPreflightCatchesErrorsOnlySomeDocumentsHit(t *testing.T) {
	path := writeHeader(t, "header.html", `<p>{{ if .Title }}{{ index .Path 999 }}{{ end }}</p>`)

	_, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.Error(t, err)
}

func TestPreflightCompilesMarkdownHeader(t *testing.T) {
	path := writeHeader(t, "header.md", `{{ if .Title }}{{ template "missing" . }}{{ end }}`)

	_, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.Error(t, err)
}

func TestPreflightCatchesUnescapedPath(t *testing.T) {
	path := writeHeader(t, "header.html", `<p>{{ .Path }}</p>`)

	_, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not well-formed XML")
}

func TestPreflightUsesTheRunsIncludePath(t *testing.T) {
	shared := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(shared, "banner.md"), []byte("shared banner\n"), 0o600))

	path := writeHeader(t, "header.md", "<!-- Include: banner.md -->\n")

	_, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.Error(t, err)

	_, err = Load(t.Context(), path, newStdlib(t), markdown.Options{IncludePath: shared})
	require.NoError(t, err)
}

func TestPreflightRejectsMarkdownThatCompilesToMalformedXML(t *testing.T) {
	path := writeHeader(t, "header.md", "<p>unclosed\n")

	_, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not well-formed XML")
}

// TestMarkdownHeaderIncludesDoNotJoinTheDocumentTemplates: an include
// registers its template on the set it compiles with.
func TestMarkdownHeaderIncludesDoNotJoinTheDocumentTemplates(t *testing.T) {
	shared := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(shared, "banner.md"), []byte("shared banner\n"), 0o600))

	std := newStdlib(t)
	before := len(std.Templates.Templates())

	path := writeHeader(t, "header.md", "<!-- Include: banner.md -->\n")

	loaded, err := Load(t.Context(), path, std, markdown.Options{IncludePath: shared})
	require.NoError(t, err)

	_, _, err = loaded.Render(t.Context(), "doc.md", "Title", "SPACE", markdown.Options{IncludePath: shared})
	require.NoError(t, err)

	assert.Len(t, std.Templates.Templates(), before)
}

// TestMarkdownHeaderCompilesOncePerText: a header that names nothing about the
// document compiles once for the whole run, preflight included, while one that
// names the document still renders each page's own.
func TestMarkdownHeaderCompilesOncePerText(t *testing.T) {
	fixed := writeHeader(t, "fixed.md", "> [!NOTE]\n> Generated; do not edit.\n")

	header, err := Load(t.Context(), fixed, newStdlib(t), markdown.Options{})
	require.NoError(t, err)

	// Each document brings its own warnings, which must not tell the
	// compiles apart.
	first, _, err := header.Render(t.Context(), "a.md", "A", "SPACE", markdown.Options{Warnings: report.NewWarnings("a.md")})
	require.NoError(t, err)

	second, _, err := header.Render(t.Context(), "b.md", "B", "SPACE", markdown.Options{Warnings: report.NewWarnings("b.md")})
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, 1, header.compiles)

	named := writeHeader(t, "named.md", "Generated from {{ .Path | xmlesc }}.\n")

	header, err = Load(t.Context(), named, newStdlib(t), markdown.Options{})
	require.NoError(t, err)

	first, _, err = header.Render(t.Context(), "a.md", "A", "SPACE", markdown.Options{})
	require.NoError(t, err)

	second, _, err = header.Render(t.Context(), "b.md", "B", "SPACE", markdown.Options{})
	require.NoError(t, err)

	assert.Equal(t, "<p>Generated from a.md.</p>\n", first)
	assert.Equal(t, "<p>Generated from b.md.</p>\n", second)
}

func TestEveryMarkdownExtensionIsCompiled(t *testing.T) {
	for _, name := range []string{"header.md", "header.MD", "header.markdown", "header.mdown", "header.mkd", "header.mkdn", "header.mdwn"} {
		t.Run(name, func(t *testing.T) {
			path := writeHeader(t, name, "**Generated**\n")

			header, err := Load(t.Context(), path, newStdlib(t), markdown.Options{})
			require.NoError(t, err)

			html, _, err := header.Render(t.Context(), "doc.md", "Title", "SPACE", markdown.Options{})
			require.NoError(t, err)
			assert.Equal(t, "<p><strong>Generated</strong></p>\n", html)
		})
	}
}

// TestMarkdownHeaderKeepsTheDefaultBoundary: the header is not the document,
// so the Boundary a document's root carries does not hold its images: one
// beside the header's directory is attached under attachment.BoundaryDocument
// and under a directory that does not hold it, and one outside the working
// directory is refused either way.
func TestMarkdownHeaderKeepsTheDefaultBoundary(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "templates"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "images"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "images", "logo.png"), []byte("png"), 0o600))

	path := filepath.Join("templates", "header.md")
	require.NoError(t, os.WriteFile(path, []byte("![logo](../images/logo.png)\n"), 0o600))

	outside := filepath.Join("templates", "outside.md")
	require.NoError(t, os.WriteFile(outside, []byte("![logo](../../elsewhere/logo.png)\n"), 0o600))

	for _, boundary := range []attachment.Boundary{"", attachment.BoundaryDocument, "templates"} {
		cfg := markdown.Options{Root: attachment.Root{}.WithBoundary(boundary)}

		header, err := Load(t.Context(), path, newStdlib(t), cfg)
		require.NoError(t, err, "boundary %q", boundary)

		_, attachments, err := header.Render(t.Context(), "doc.md", "Title", "DOCS", cfg)
		require.NoError(t, err, "boundary %q", boundary)
		require.Len(t, attachments, 1, "boundary %q", boundary)

		_, err = Load(t.Context(), outside, newStdlib(t), cfg)
		require.ErrorIs(t, err, attachment.ErrOutsideProject, "boundary %q", boundary)
	}
}

// TestMarkdownHeaderAttachesWhatItLinksWhateverTheDocumentPublishes: the
// header's links are not resolved to pages, so a file it links is attached
// even when the document would link to that file's page, and each document's
// own answer does not tell the compiles apart.
func TestMarkdownHeaderAttachesWhatItLinksWhateverTheDocumentPublishes(t *testing.T) {
	path := writeHeader(t, "header.md", "See the [guide](guide.rst).\n")
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(path), "guide.rst"),
		[]byte("<!-- Space: SPACE -->\n<!-- Title: Guide -->\n"), 0o600))

	header, err := Load(t.Context(), path, newStdlib(t), markdown.Options{AttachReferenced: true})
	require.NoError(t, err)

	for _, file := range []string{"a.md", "b.md"} {
		html, attachments, err := header.Render(t.Context(), file, "T", "SPACE", markdown.Options{
			AttachReferenced: true,
			Publishes:        func(string) bool { return file != "" },
		})
		require.NoError(t, err)
		assert.Contains(t, html, `<ri:attachment ri:filename="guide.rst"/>`)
		assert.Len(t, attachments, 1)
	}

	assert.Equal(t, 1, header.compiles)
}
