package mark

import (
	"io"
	"math"
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

// TestD2ScaleRejectsWhatIsNotAScale covers the public API for the same numbers.
// NaN fails every comparison it is given, so a check phrased as "not <= 0" lets
// it through; an infinity passes one outright. Both then multiply a diagram's
// size into something that is not a number of pixels.
func TestD2ScaleRejectsWhatIsNotAScale(t *testing.T) {
	for _, scale := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		config := mermaidFixture(t)
		config.Features = []string{"d2"}
		config.D2Scale = scale

		err := Run(config)
		require.Error(t, err, "a scale of %v is not one", scale)
		assert.Contains(t, err.Error(), "D2Scale")
	}
}

// TestD2ScaleIsOnlyCheckedWhereDiagramsAreDrawn is the boundary: a run that
// never turns d2 on carries the field past every path that reads it, so the
// zero value of a caller who never heard of it is not an error.
func TestD2ScaleIsOnlyCheckedWhereDiagramsAreDrawn(t *testing.T) {
	config := mermaidFixture(t)
	config.D2Scale = 0

	require.NoError(t, Run(config))
}
