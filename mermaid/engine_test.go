package mermaid

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
