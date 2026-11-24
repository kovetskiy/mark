package mark

import (
	"testing"

	"github.com/rs/zerolog"
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
