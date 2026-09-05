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

// TestMermaidScaleIsRefusedWhereItWouldHaveScaled covers a scale carried over
// from a PNG setup. Nothing multiplies the pixels of an SVG, so the run would
// publish diagrams at a size nobody asked for -- the original size -- and say
// nothing about the setting it passed over.
func TestMermaidScaleIsRefusedWhereItWouldHaveScaled(t *testing.T) {
	config := mermaidFixture(t)
	config.MermaidOutput = "svg"
	config.MermaidScale = 2

	err := Run(config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "MermaidScale")
}

// TestMermaidScaleThatScalesNothingIsLeftAlone is the boundary, and the reason
// the check is not the command line's. A Config field cannot say whether
// anybody set it, and the CLI fills this one in with 1.0 on every run: refusing
// that would refuse every SVG run mark makes of its own.
func TestMermaidScaleThatScalesNothingIsLeftAlone(t *testing.T) {
	for _, scale := range []float64{0, 1} {
		config := mermaidFixture(t)
		config.MermaidOutput = "svg"
		config.MermaidScale = scale

		assert.NoError(t, Run(config), "a scale of %v scales nothing", scale)
	}
}

// TestD2OutputRejectsAFormatMarkCannotPublish is the d2 half of the same rule:
// Config is a public API, so a library caller arrives with no command line to
// have been checked, and a format the renderer does not know falls to its PNG
// branch without a word.
func TestD2OutputRejectsAFormatMarkCannotPublish(t *testing.T) {
	config := mermaidFixture(t)
	config.D2Output = "jpeg"

	err := Run(config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "D2Output")
}

// TestD2DefaultsPublishAsBefore is its control: a caller that never heard of
// the field leaves it zero, and has always got a PNG.
func TestD2DefaultsPublishAsBefore(t *testing.T) {
	config := mermaidFixture(t)
	config.D2Output = ""

	require.NoError(t, Run(config))
}
