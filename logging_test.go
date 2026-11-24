package mark

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetLogLevel(t *testing.T) {
	prev := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(prev) })

	for level, want := range map[string]zerolog.Level{
		"TRACE": zerolog.TraceLevel, "debug": zerolog.DebugLevel, "Info": zerolog.InfoLevel,
		"WARNING": zerolog.WarnLevel, "error": zerolog.ErrorLevel, "FATAL": zerolog.FatalLevel,
	} {
		require.NoError(t, SetLogLevel(level))
		assert.Equal(t, want, zerolog.GlobalLevel(), level)
	}
}

func TestSetLogLevelRefusesAnUnknownLevel(t *testing.T) {
	prev := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(prev) })
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	assert.EqualError(t, SetLogLevel("loud"), "unknown log level: loud")
	assert.EqualError(t, SetLogLevel(""), "unknown log level: ")
	assert.Equal(t, zerolog.InfoLevel, zerolog.GlobalLevel(), "an unknown level changes nothing")
}

// TestRunLogsToTheLoggerInItsContext: a program gives a run its own logger by
// putting it in the run's context, and nothing of the run then reaches the
// global one, its includes and macros included.
func TestRunLogsToTheLoggerInItsContext(t *testing.T) {
	prevLevel, prevLogger := zerolog.GlobalLevel(), log.Logger
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	var global bytes.Buffer
	log.Logger = zerolog.New(&global)
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(prevLevel)
		log.Logger = prevLogger
	})

	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "fragment.md", "Included text.\n")
	writeFile(t, dir, "box.md", "Box {{ .Text }}\n")
	file := writeFile(t, dir, "doc.md", dryRunDocBody(
		"<!-- Include: fragment.md -->\n\n"+
			"<!-- Macro: BOX-(\\w+)\n     Template: box.md\n     Text: ${1} -->\n\nBOX-hello\n"))

	var own bytes.Buffer
	ctx := zerolog.New(&own).Level(zerolog.TraceLevel).WithContext(context.Background())
	require.NoError(t, Run(ctx, changesOnlyConfig(server, file)))

	assert.Contains(t, own.String(), "content hash")
	assert.Contains(t, own.String(), "rest:", "the API's request dumps follow the run's logger")
	assert.Contains(t, own.String(), "including template", "includes log to the run's logger")
	assert.Contains(t, own.String(), "loaded macro", "macros log to the run's logger")
	assert.Empty(t, global.String())
}
