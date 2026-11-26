// Package logctx finds the logger a run logs to: the zerolog logger its
// context carries, so that a program publishing through the library can give
// each run a logger of its own, or else the global one.
package logctx

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// From is the logger ctx carries, as zerolog.Ctx finds it. With none,
// zerolog.Ctx gives zerolog.DefaultContextLogger when that is set and a
// disabled logger otherwise; the global logger stands in for that disabled
// one, as it is what mark logged to before a context carried one.
func From(ctx context.Context) *zerolog.Logger {
	if l := Carried(ctx); l != nil {
		return l
	}

	return fallback()
}

// Carried is the logger ctx carries, or nil when it carries none, so that
// something outliving ctx can keep that logger without pinning the global one
// as it is at that moment.
func Carried(ctx context.Context) *zerolog.Logger {
	if ctx == nil {
		return nil
	}

	l := zerolog.Ctx(ctx)
	if l == zerolog.Ctx(context.Background()) {
		return nil
	}

	return l
}

func fallback() *zerolog.Logger {
	if zerolog.DefaultContextLogger != nil {
		return zerolog.DefaultContextLogger
	}

	return &log.Logger
}

// Tracing reports whether From(ctx) logs at trace level.
func Tracing(ctx context.Context) bool {
	e := From(ctx).Trace()
	if e == nil {
		return false
	}
	e.Discard()

	return true
}
