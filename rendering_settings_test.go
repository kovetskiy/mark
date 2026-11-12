package mark

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderingFixture is a run of one document that has no page yet, and the
// server it would be created on.
func renderingFixture(t *testing.T, rendering RenderingConfig) (Config, *confluencetest.Server) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n")

	return Config{
		PublishConfig: PublishConfig{Rendering: rendering, Output: io.Discard},
		Files:         filepath.Join(dir, "*.md"),
		Connection:    ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
	}, server
}

// requireNothingWritten fails when the run changed anything on the server.
func requireNothingWritten(t *testing.T, server *confluencetest.Server) {
	t.Helper()

	for _, request := range server.Requests() {
		require.Equal(t, http.MethodGet, request.Method, "%s %s", request.Method, request.Path)
	}
}

// TestImageAlignIsCheckedBeforeAnythingIsPublished: the alignment is the same
// for every document, so a value no image can take is refused with the other
// rendering settings rather than at the first compile, which comes after the
// document's page has been created.
func TestImageAlignIsCheckedBeforeAnythingIsPublished(t *testing.T) {
	for _, align := range []string{"diagonal", "middle", " "} {
		rendering := RenderingConfig{ImageAlign: align}

		err := PublishConfig{Rendering: rendering}.Validate()
		require.Error(t, err, "%q", align)
		assert.Contains(t, err.Error(), "ImageAlign")

		config, server := renderingFixture(t, rendering)
		err = Run(context.Background(), config)
		require.Error(t, err, "%q", align)
		assert.Contains(t, err.Error(), "ImageAlign")
		requireNothingWritten(t, server)

	}
}

// TestImageAlignAcceptsWhatADocumentHeaderDoes: the field is read as the
// Image-Align header is, in any case and with space around it, and empty
// leaves images where they are.
func TestImageAlignAcceptsWhatADocumentHeaderDoes(t *testing.T) {
	for _, align := range []string{"", "left", "center", "right", "Center", " right "} {
		config, _ := renderingFixture(t, RenderingConfig{ImageAlign: align})

		require.NoError(t, config.Validate(), "%q", align)
		require.NoError(t, Run(context.Background(), config), "%q", align)
	}
}
