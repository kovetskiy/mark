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

// TestPageHeaderNamesThePageInPageIDMode: --page-id discards the file's
// metadata, so the title has to come from the page being updated.
func TestPageHeaderNamesThePageInPageIDMode(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	existing := server.AddPage("DOCS", "Existing", "page", "")
	file := writeFile(t, dir, "doc.md", "Just a body.\n")

	config := publishConfig(server.URL, file)
	config.PageID = existing.ID
	config.Space = "DOCS"
	config.PageHeader = writeFile(t, dir, "header.html", `<p>{{ .Title | xmlesc }} in {{ .Space | xmlesc }}</p>`)

	require.NoError(t, Run(config))

	assert.Contains(t, bodyOfPageTitled(t, server, "Existing"), "<p>Existing in DOCS</p>")
}

// TestPageHeaderNamesThePageInPageIDDryRun: the dry run already looks the page
// up, so the header it prints matches the one a real run would publish.
func TestPageHeaderNamesThePageInPageIDDryRun(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	existing := server.AddPage("DOCS", "Existing", "page", "")
	file := writeFile(t, dir, "doc.md", "Just a body.\n")

	var output bytes.Buffer

	config := publishConfig(server.URL, file)
	config.PageID = existing.ID
	config.Space = "DOCS"
	config.DryRun = true
	config.Output = &output
	config.PageHeader = writeFile(t, dir, "header.html", `<p>{{ .Title | xmlesc }} in {{ .Space | xmlesc }}</p>`)

	require.NoError(t, Run(config))

	assert.Contains(t, output.String(), "<p>Existing in DOCS</p>")
}

// TestPageHeaderImageThatClashesWithTheDocumentsIsRefused: one filename is one
// attachment on the page, so the second upload would replace the first.
func TestPageHeaderImageThatClashesWithTheDocumentsIsRefused(t *testing.T) {
	server, _ := docsSpace(t)
	docDir := t.TempDir()
	headerDir := t.TempDir()

	writeFile(t, docDir, "logo.png", "document logo")
	writeFile(t, headerDir, "logo.png", "header logo")

	file := writeFile(t, docDir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Clash -->

![logo](logo.png)
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, headerDir, "header.md", "![logo](logo.png)\n")

	err := Run(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page header attachment")
}

func TestPageHeaderImageIdenticalToTheDocumentsIsAttachedOnce(t *testing.T) {
	server, _ := docsSpace(t)
	docDir := t.TempDir()
	headerDir := t.TempDir()

	writeFile(t, docDir, "logo.png", "same logo")
	writeFile(t, headerDir, "logo.png", "same logo")

	file := writeFile(t, docDir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Same -->

![logo](logo.png)
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, headerDir, "header.md", "![logo](logo.png)\n")

	require.NoError(t, Run(config))
}

// TestPageHeaderImageThatClashesWithADeclaredAttachmentUploadsNothing: the
// clash is known before the first upload, so the document's own file is not
// sent for a page the run is about to fail on.
func TestPageHeaderImageThatClashesWithADeclaredAttachmentUploadsNothing(t *testing.T) {
	server, _ := docsSpace(t)
	docDir := t.TempDir()
	headerDir := t.TempDir()

	writeFile(t, docDir, "logo.png", "document logo")
	writeFile(t, headerDir, "logo.png", "header logo")

	file := writeFile(t, docDir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Declared Clash -->
<!-- Attachment: logo.png -->

Text.
`)
	config := publishConfig(server.URL, file)
	config.PageHeader = writeFile(t, headerDir, "header.md", "![logo](logo.png)\n")

	err := Run(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page header attachment")
	assert.Equal(t, 0, server.CountRequests("POST", "/child/attachment"))
}

// TestPageHeaderAttachmentClashFailsADryRun: a dry run that reports success
// where the real run refuses the page is not a dry run of it. Both a declared
// attachment and one the document only references are covered.
func TestPageHeaderAttachmentClashFailsADryRun(t *testing.T) {
	for _, mode := range []string{"dry-run", "compile-only"} {
		t.Run(mode, func(t *testing.T) {
			server, _ := docsSpace(t)
			docDir := t.TempDir()
			headerDir := t.TempDir()

			writeFile(t, docDir, "logo.png", "document logo")
			writeFile(t, docDir, "chart.png", "document chart")
			writeFile(t, headerDir, "logo.png", "header logo")
			writeFile(t, headerDir, "chart.png", "header chart")

			file := writeFile(t, docDir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Dry Clash -->
<!-- Attachment: logo.png -->

![chart](chart.png)
`)
			for _, header := range []string{"![logo](logo.png)\n", "![chart](chart.png)\n"} {
				config := publishConfig(server.URL, file)
				config.DryRun = mode == "dry-run"
				config.CompileOnly = mode == "compile-only"
				config.Output = &bytes.Buffer{}
				config.PageHeader = writeFile(t, headerDir, "header.md", header)

				err := Run(config)
				require.Error(t, err, header)
				assert.Contains(t, err.Error(), "page header attachment")
			}
		})
	}
}
