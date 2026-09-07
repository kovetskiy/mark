package mermaid

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

// useEngine sets the engine for one test and puts it back afterwards, since it
// is chosen once for a whole run.
func useEngine(t *testing.T, kind string) string {
	t.Helper()

	var logged bytes.Buffer

	restore := log.Logger
	log.Logger = zerolog.New(&logged)

	previous := mermaidKind
	t.Cleanup(func() {
		log.Logger = restore
		require.NoError(t, UseEngine(previous))
	})

	require.NoError(t, UseEngine(kind))

	return logged.String()
}

// TestUseEngineRefusesWhatItCannotDraw covers a name that is neither engine.
// Falling back to chrome would publish diagrams drawn by something other than
// what was asked for, and say nothing about it.
func TestUseEngineRefusesWhatItCannotDraw(t *testing.T) {
	err := UseEngine("graphviz")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "graphviz")
	assert.Contains(t, err.Error(), EngineChrome)
	assert.Contains(t, err.Error(), EngineMerman)

	assert.Equal(t, EngineChrome, mermaidKind, "a refused name must not change what is in use")
}

// TestUseEngineDefaultsToChrome covers the empty value, which is what a caller
// that never heard of the setting passes -- and what mark has always done.
func TestUseEngineDefaultsToChrome(t *testing.T) {
	useEngine(t, "")

	assert.Equal(t, EngineChrome, mermaidKind)
}

// TestUseEngineSaysMermanIsExperimental covers the warning, which is the only
// sign that diagrams are being drawn by something other than mermaid.js.
func TestUseEngineSaysMermanIsExperimental(t *testing.T) {
	logged := useEngine(t, EngineMerman)

	assert.Contains(t, logged, "experimental")
	assert.Equal(t, EngineMerman, mermaidKind)
}

// TestChoosingChromeSaysNothing is its boundary: the default is not worth a
// warning on every run.
func TestChoosingChromeSaysNothing(t *testing.T) {
	logged := useEngine(t, EngineChrome)

	assert.Empty(t, logged)
}

// TestMermanReportsWhatIsMissingByName covers the machine without merman on it,
// which is every machine until somebody installs it. The setting that asked for
// it is named, since a diagram that will not draw says nothing about why.
func TestMermanReportsWhatIsMissingByName(t *testing.T) {
	if _, err := lookMerman(); err == nil {
		t.Skip("merman is installed here, so there is nothing missing to report")
	}

	useEngine(t, EngineMerman)

	_, err := ProcessMermaidLocally("d", []byte("graph TD;\n A-->B;"), 1.0)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "--mermaid-engine")
	assert.Contains(t, err.Error(), "merman")
}

// TestVersionFloor covers the version merman reports, which is the only thing
// separating a binary that draws what mark expects from one that draws
// something else. An older one answers the capability probe perfectly well.
func TestVersionFloor(t *testing.T) {
	// Checked against the floor rather than assumed, so that raising it fails
	// here loudly rather than leaving these testing nothing: a fixture meant to
	// be older is worth nothing once the floor has moved past it.
	floor := "v" + MinimumMermanVersion

	for _, version := range []string{"0.7.0", "0.8.0-alpha.5"} {
		require.Negative(t, semver.Compare("v"+version, floor),
			"%s is only a useful fixture while it is older than %s", version, MinimumMermanVersion)
		require.Error(t, checkMermanVersion(version))
	}

	for _, version := range []string{MinimumMermanVersion, "0.8.0-alpha.7", "0.8.0", "0.9.0", "1.0.0", "v0.8.0"} {
		require.GreaterOrEqual(t, semver.Compare("v"+strings.TrimPrefix(version, "v"), floor), 0,
			"%s is only a useful fixture while it is at least %s", version, MinimumMermanVersion)
		require.NoError(t, checkMermanVersion(version))
	}

	// Not a refusal: a version nobody can parse is more likely somebody's own
	// build than an old release, and declining to publish over a version string
	// would be the wrong way round.
	assert.NoError(t, checkMermanVersion(""))
	assert.NoError(t, checkMermanVersion("built-from-source"))
}

// TestBothEnginesDrawTheSameDiagram runs what mark actually does through each
// engine in turn.
//
// The point is not that the two agree pixel for pixel -- they are different
// implementations and will not -- but that a diagram published through either
// is a diagram: an attachment with content, a size the page can lay out, and a
// checksum that does not move between runs.
//
// Skipped where merman is not installed, which is every machine until somebody
// installs it, and not skipped in CI, where it is.
func TestBothEnginesDrawTheSameDiagram(t *testing.T) {
	if _, err := lookMerman(); err != nil {
		t.Skip("merman is not installed here")
	}

	diagram := []byte("graph TD;\n A-->B;")

	for _, kind := range []string{EngineChrome, EngineMerman} {
		t.Run(kind, func(t *testing.T) {
			useEngine(t, kind)

			png, err := ProcessMermaidLocally("both", diagram, 1.0)
			require.NoError(t, err)

			assert.NotEmpty(t, png.FileBytes)
			assert.Equal(t, "both.png", png.Filename)
			assertPixelSize(t, png.Width, png.Height)

			again, err := ProcessMermaidLocally("both", diagram, 1.0)
			require.NoError(t, err)
			assert.Equal(t, png.Checksum, again.Checksum, "the same diagram is the same attachment")

			svg, err := ProcessMermaidSVG("both", diagram, 1.0)
			require.NoError(t, err)

			assert.True(t, hasSVGRoot(string(svg.FileBytes)))
			assertPixelSize(t, svg.Width, svg.Height)
		})
	}
}

// TestMinimumVersionComesFromTheFile pins the arrangement rather than the
// number: the Dockerfile and the CI workflow read merman-version.txt, and the
// binary embeds it, so raising the floor is one edit. A constant written here
// instead would drift from what the image installs, and the drift would show up
// as a diagram drawn wrong rather than as a build that stopped.
func TestMinimumVersionComesFromTheFile(t *testing.T) {
	onDisk, err := os.ReadFile("merman-version.txt")
	require.NoError(t, err)

	assert.Equal(t, strings.TrimSpace(string(onDisk)), MinimumMermanVersion)
	assert.NotEmpty(t, MinimumMermanVersion)
	assert.True(t, semver.IsValid("v"+MinimumMermanVersion),
		"the floor has to be a version the check can compare")
}
