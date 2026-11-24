package confluence

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
)

// TestRequestDumpsFollowTheLoggerInTheContext: one API dumps a request when
// the logger in that request's context logs at trace, and not otherwise,
// whatever the global logger does.
func TestRequestDumpsFollowTheLoggerInTheContext(t *testing.T) {
	prevLevel, prevLogger := zerolog.GlobalLevel(), log.Logger
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	var global bytes.Buffer
	log.Logger = zerolog.New(&global).Level(zerolog.InfoLevel)
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(prevLevel)
		log.Logger = prevLogger
	})

	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	api := NewAPI(server.URL, Options{Username: "user", Password: "token"})

	var traced, quiet bytes.Buffer
	tracing := zerolog.New(&traced).Level(zerolog.TraceLevel).WithContext(context.Background())
	notTracing := zerolog.New(&quiet).Level(zerolog.InfoLevel).WithContext(context.Background())

	_, err := api.GetSpaceID(tracing, "DOCS")
	require.NoError(t, err)
	_, err = api.GetSpaceID(notTracing, "DOCS")
	require.NoError(t, err)
	_, err = api.GetSpaceID(context.Background(), "DOCS")
	require.NoError(t, err)

	assert.Contains(t, traced.String(), "rest")
	assert.Contains(t, traced.String(), "GET ")
	assert.Empty(t, quiet.String())
	assert.Empty(t, global.String(), "the global logger is at info")
}
