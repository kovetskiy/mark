package main

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
	errorMesage := fmt.Sprintf(format, args...)

    if err == nil {	
		if h.ContinueOnError {	
			log.Error(errorMesage)
			return
		}
		log.Fatal(errorMesage)
    }
    
    if h.ContinueOnError { 
        log.Errorf(err, errorMesage)
		return
    }
    log.Fatalf(err, errorMesage)
}
