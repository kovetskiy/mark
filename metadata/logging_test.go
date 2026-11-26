package metadata

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

// TestAppendGeneratedHashLogsWhereItIsTold: the old form logs to the global
// logger, as it always has, and the Context form to the logger ctx carries.
func TestAppendGeneratedHashLogsWhereItIsTold(t *testing.T) {
	prevLevel, prevLogger := zerolog.GlobalLevel(), log.Logger
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	var global bytes.Buffer
	log.Logger = zerolog.New(&global)
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(prevLevel)
		log.Logger = prevLogger
	})

	AppendGeneratedHash(&Meta{Space: "DOCS", Title: "Old"})
	assert.Contains(t, global.String(), "appended hash to page title: Old - ")

	global.Reset()
	var own bytes.Buffer
	ctx := zerolog.New(&own).WithContext(context.Background())
	AppendGeneratedHashContext(ctx, &Meta{Space: "DOCS", Title: "New"})
	assert.Contains(t, own.String(), "appended hash to page title: New - ")
	assert.Empty(t, global.String())
}
