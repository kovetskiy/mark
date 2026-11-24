package mark

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"
)

// SetLogLevel sets the level mark logs at: TRACE, DEBUG, INFO, WARNING, ERROR or
// FATAL, in any case, as --log-level takes them.
//
// mark logs through zerolog's global logger, and zerolog logs everything until
// a level is set, every request to Confluence and every compiled page among it.
// The command line sets one from --log-level; a program publishing through
// this package sets one with this, or with zerolog.SetGlobalLevel. The level is
// the process's, not a run's.
func SetLogLevel(level string) error {
	levels := map[string]zerolog.Level{
		"TRACE":   zerolog.TraceLevel,
		"DEBUG":   zerolog.DebugLevel,
		"INFO":    zerolog.InfoLevel,
		"WARNING": zerolog.WarnLevel,
		"ERROR":   zerolog.ErrorLevel,
		"FATAL":   zerolog.FatalLevel,
	}
	l, ok := levels[strings.ToUpper(level)]
	if !ok {
		return fmt.Errorf("unknown log level: %s", level)
	}
	zerolog.SetGlobalLevel(l)

	return nil
}
