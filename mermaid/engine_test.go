package mermaid

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

// useEngine creates an engine of kind for one test, closed when it ends, and
// returns it with what creating it logged.
func useEngine(t *testing.T, kind string) (*Engine, string) {
	t.Helper()

	var logged bytes.Buffer

	restore := log.Logger
	log.Logger = zerolog.New(&logged)

	engine, err := NewEngine(kind)
	log.Logger = restore
	require.NoError(t, err)

	t.Cleanup(engine.Close)

	return engine, logged.String()
}

// TestNewEngineRefusesWhatItCannotDraw covers a name that is neither engine.
// Falling back to chrome would publish diagrams drawn by something other than
// what was asked for, and say nothing about it.
func TestNewEngineRefusesWhatItCannotDraw(t *testing.T) {
	_, err := NewEngine("graphviz")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "graphviz")
	assert.Contains(t, err.Error(), EngineChrome)
	assert.Contains(t, err.Error(), EngineMerman)
}

// TestNewEngineDefaultsToChrome covers the empty value, which is what a caller
// that never heard of the setting passes -- and what mark has always done.
func TestNewEngineDefaultsToChrome(t *testing.T) {
	engine, _ := useEngine(t, "")

	assert.Equal(t, EngineChrome, engine.kind)
}

// TestNewEngineSaysMermanIsExperimental covers the warning, which is the only
// sign that diagrams are being drawn by something other than mermaid.js.
func TestNewEngineSaysMermanIsExperimental(t *testing.T) {
	engine, logged := useEngine(t, EngineMerman)

	assert.Contains(t, logged, "experimental")
	assert.Equal(t, EngineMerman, engine.kind)
}

// TestChoosingChromeSaysNothing is its boundary: the default is not worth a
// warning on every run.
func TestChoosingChromeSaysNothing(t *testing.T) {
	_, logged := useEngine(t, EngineChrome)

	assert.Empty(t, logged)
}

// TestEngineStartsOnlyWhenUsed: a run that draws no diagram launches nothing.
func TestEngineStartsOnlyWhenUsed(t *testing.T) {
	engine, _ := useEngine(t, EngineChrome)

	assert.Nil(t, engine.renderer)
}

// TestNilEngineSaysSo covers a caller that turned diagrams on without giving
// the renderer an engine to draw them with.
func TestNilEngineSaysSo(t *testing.T) {
	var engine *Engine

	_, err := engine.ProcessMermaidLocally("d", []byte("graph TD;\n A-->B;"), 1.0)
	assert.ErrorIs(t, err, ErrNoEngine)

	engine.Close()
}

// closingRenderer is a renderer whose engine is closed while it draws, as a
// Publisher closed from another goroutine would.
type closingRenderer struct {
	mermaid.Renderer
	engine  *Engine
	renders int
}

func (r *closingRenderer) RenderContext(context.Context, string, ...mermaid.RenderOption) (string, error) {
	r.renders++
	r.engine.Close()

	return "", mermaid.ErrEngineClosed
}

func (r *closingRenderer) Cancel() {}

// TestCloseDuringARenderIsNotACrash: the render Close cancels is reported as
// the engine closing, not logged and retried as though the browser had died.
func TestCloseDuringARenderIsNotACrash(t *testing.T) {
	engine, _ := useEngine(t, EngineChrome)
	renderer := &closingRenderer{engine: engine}
	engine.renderer = renderer

	var logged bytes.Buffer

	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	t.Cleanup(func() { log.Logger = restore })

	_, err := engine.ProcessMermaidSVG("d", []byte("graph TD;\n A-->B;"), 1.0)

	require.ErrorIs(t, err, errClosed)
	assert.Equal(t, 1, renderer.renders)
	assert.NotContains(t, logged.String(), "died", "a closed engine did not crash")
}

// TestMermanReportsWhatIsMissingByName covers the machine without merman on it,
// which is every machine until somebody installs it. The setting that asked for
// it is named, since a diagram that will not draw says nothing about why.
func TestMermanReportsWhatIsMissingByName(t *testing.T) {
	if _, err := lookMerman(); err == nil {
		t.Skip("merman is installed here, so there is nothing missing to report")
	}

	engine, _ := useEngine(t, EngineMerman)

	_, err := engine.ProcessMermaidLocally("d", []byte("graph TD;\n A-->B;"), 1.0)
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

	for _, version := range []string{"0.7.0", "0.8.0-alpha.6", "0.9.1", "0.9.2-rc.1"} {
		require.Negative(t, semver.Compare("v"+version, floor),
			"%s is only a useful fixture while it is older than %s", version, MinimumMermanVersion)
		require.Error(t, checkMermanVersion(version))
	}

	for _, version := range []string{MinimumMermanVersion, "0.9.3", "0.10.0", "1.0.0", "v0.9.2"} {
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
			engine, _ := useEngine(t, kind)

			png, err := engine.ProcessMermaidLocally("both", diagram, 1.0)
			require.NoError(t, err)

			assert.NotEmpty(t, png.FileBytes)
			assert.Equal(t, "both.png", png.Filename)
			assertPixelSize(t, png.Width, png.Height)

			again, err := engine.ProcessMermaidLocally("both", diagram, 1.0)
			require.NoError(t, err)
			assert.Equal(t, png.Checksum, again.Checksum, "the same diagram is the same attachment")

			svg, err := engine.ProcessMermaidSVG("both", diagram, 1.0)
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

// lookMerman finds the merman binary, under either name the library accepts:
// merman-cli is what its releases are called, and merman is what a locally
// built or renamed copy often is.
func lookMerman() (string, error) {
	// Not nil to begin with: a library that named no binary at all would
	// otherwise be reported as a merman found at the empty path.
	err := exec.ErrNotFound

	for _, name := range mermaid.MermanBinaries {
		path, lookErr := exec.LookPath(name)
		if lookErr == nil {
			return path, nil
		}

		err = lookErr
	}

	return "", err
}
