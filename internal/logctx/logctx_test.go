package logctx

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func swapGlobal(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = prev })

	return &buf
}

func TestFromTheContext(t *testing.T) {
	global := swapGlobal(t)
	var own bytes.Buffer
	ctx := zerolog.New(&own).WithContext(context.Background())

	From(ctx).Info().Msg("hello")

	assert.Contains(t, own.String(), "hello")
	assert.Empty(t, global.String())
}

func TestFromFallsBackToTheGlobalLogger(t *testing.T) {
	global := swapGlobal(t)

	From(context.Background()).Info().Msg("plain")
	//nolint:staticcheck // SA1012: a nil context is what this covers.
	From(nil).Info().Msg("nil")

	assert.Contains(t, global.String(), "plain")
	assert.Contains(t, global.String(), "nil")
}

func TestFromPrefersTheDefaultContextLogger(t *testing.T) {
	global := swapGlobal(t)
	var def bytes.Buffer
	l := zerolog.New(&def)
	prev := zerolog.DefaultContextLogger
	zerolog.DefaultContextLogger = &l
	t.Cleanup(func() { zerolog.DefaultContextLogger = prev })

	From(context.Background()).Info().Msg("default")

	assert.Contains(t, def.String(), "default")
	assert.Empty(t, global.String())
}

func TestTracing(t *testing.T) {
	prev := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(prev) })

	traced := zerolog.New(nil).Level(zerolog.TraceLevel).WithContext(context.Background())
	quiet := zerolog.New(nil).Level(zerolog.InfoLevel).WithContext(context.Background())

	assert.True(t, Tracing(traced))
	assert.False(t, Tracing(quiet))

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	assert.False(t, Tracing(traced), "the global level still applies")
}
