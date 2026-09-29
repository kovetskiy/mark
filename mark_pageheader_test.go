package mark

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageHeaderIsPublishedAboveTheDocument(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Generated -->

Body text.
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, dir, "header.html", `<p>Generated: {{ .Title | xmlesc }}</p>`)

	require.NoError(t, Run(config))

	body := bodyOfPageTitled(t, server, "Generated")
	assert.Contains(t, body, "<p>Generated: Generated</p>")
	assert.Less(t, bytes.Index([]byte(body), []byte("Generated:")), bytes.Index([]byte(body), []byte("Body text.")))
}

// TestPageHeaderSitsInsideTheArticleLayout: outside the layout, Confluence
// would show the header as a section of its own beside the sidebar.
func TestPageHeaderSitsInsideTheArticleLayout(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Article -->
<!-- Layout: article -->

Body text.
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, dir, "header.html", `<p>Header</p>`)

	require.NoError(t, Run(config))

	assert.Contains(t, bodyOfPageTitled(t, server, "Article"), "<ac:layout-cell><p>Header</p>")
}

func TestPageHeaderIsInCompileOnlyOutput(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Title: Compiled -->

Body text.
`)

	var output bytes.Buffer
	require.NoError(t, Run(Config{
		Files: file, CompileOnly: true,
		Features:   []string{"mention"},
		PageHeader: writeFile(t, dir, "header.md", "**Generated** from {{ .Space }}\n"),
		Output:     &output,
	}))

	assert.Contains(t, output.String(), "<p><strong>Generated</strong> from DOCS</p>")
}
