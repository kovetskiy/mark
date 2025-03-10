package util

import (
	"fmt"

	"github.com/reconquest/pkg/log"
)

type FatalErrorHandler struct {
	ContinueOnError bool
}

func NewErrorHandler(continueOnError bool) *FatalErrorHandler {
	return &FatalErrorHandler{
		ContinueOnError: continueOnError,
	}
}

func (h *FatalErrorHandler) Handle(err error, format string, args ...interface{}) {

	if err == nil {
		if h.ContinueOnError {
			log.Error(fmt.Sprintf(format, args...))
			return
		}
		log.Fatal(fmt.Sprintf(format, args...))
	}

	if h.ContinueOnError {
		log.Errorf(err, format, args...)
		return
	}
	log.Fatalf(err, format, args...)
}
