package mark

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"
)

// SetLogLevel sets the level mark logs at: TRACE, DEBUG, INFO, WARNING, ERROR or
// FATAL, in any case, as --log-level takes them.
//
// mark logs through zerolog's global logger, whose level is debug until one is
// set; the command line sets one from --log-level, which defaults to info. This
// sets zerolog's global level, so it applies to the whole process, the
// program's own zerolog logging included.
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
