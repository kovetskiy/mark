package util

import (
	"github.com/rs/zerolog/log"
)

type FatalErrorHandler struct {
	ContinueOnError bool
}

func NewErrorHandler(continueOnError bool) *FatalErrorHandler {
	return &FatalErrorHandler{
		ContinueOnError: continueOnError,
	}
}

func (h *FatalErrorHandler) Handle(err error, format string, args ...any) {

	if err == nil {
		if h.ContinueOnError {
			log.Error().Msgf(format, args...)
			return
		}
		log.Fatal().Msgf(format, args...)
	}

	if h.ContinueOnError {
		log.Error().Err(err).Msgf(format, args...)
		return
	}
	log.Fatal().Err(err).Msgf(format, args...)
}
