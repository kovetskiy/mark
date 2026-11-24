package mark

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"
)

// SetLogLevel sets the level mark logs at: TRACE, DEBUG, INFO, WARNING, ERROR or
// FATAL, in any case, as --log-level takes them.
//
// It sets zerolog's global level, which is TRACE until one is set, so it
// applies to the whole process. confluence.NewAPI decides from the level when
// it is built whether to dump every request, so set it before that.
func SetLogLevel(level string) error {
	var l zerolog.Level
	switch strings.ToUpper(level) {
	case "TRACE":
		l = zerolog.TraceLevel
	case "DEBUG":
		l = zerolog.DebugLevel
	case "INFO":
		l = zerolog.InfoLevel
	case "WARNING":
		l = zerolog.WarnLevel
	case "ERROR":
		l = zerolog.ErrorLevel
	case "FATAL":
		l = zerolog.FatalLevel
	default:
		return fmt.Errorf("unknown log level: %s", level)
	}
	zerolog.SetGlobalLevel(l)

	return nil
}
