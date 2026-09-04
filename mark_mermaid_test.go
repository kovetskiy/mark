package mark

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mermaidFixture returns a server and a configuration that publishes one
// ordinary document, so that what a test changes about the mermaid settings is
// the only thing that can fail the run.
func mermaidFixture(t *testing.T) Config {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n")

	return Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    filepath.Join(dir, "*.md"),
		Output:   io.Discard,
	}
}

// TestMermaidOutputRejectsAFormatMarkCannotPublish covers the settings from the
// side the flags do not reach. Config is a public API, so a library caller
// arrives here without a command line to have been checked, and a format the
// renderer does not know falls to its default branch: a PNG, published without
// a word about the SVG that was asked for.
func TestMermaidOutputRejectsAFormatMarkCannotPublish(t *testing.T) {
	config := mermaidFixture(t)
	config.MermaidOutput = "jpeg"

	err := Run(config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "MermaidOutput")
}

// TestMermaidBundleNeedsAnSVGToGoIn is the same for the pair of them: a bundle
// is kept inside the SVG, so asking for one alongside a PNG asks for something
// that cannot happen, and silently getting the PNG is the worst reading of it.
func TestMermaidBundleNeedsAnSVGToGoIn(t *testing.T) {
	config := mermaidFixture(t)
	config.MermaidBundle = true

	err := Run(config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "MermaidBundle")
}

// TestMermaidDefaultsPublishAsBefore is the control, and the reason the empty
// value is accepted rather than checked against the two names: a caller that
// never heard of these fields leaves them zero, and has always got a PNG.
func TestMermaidDefaultsPublishAsBefore(t *testing.T) {
	require.NoError(t, Run(mermaidFixture(t)))
}
