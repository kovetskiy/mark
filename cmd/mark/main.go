package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kovetskiy/mark/v17/util"
	"github.com/rs/zerolog/log"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	cmd := util.NewCommand(fmt.Sprintf("%s@%s", version, commit))

	// Ctrl-C or a SIGTERM stops the run at the next file rather than killing
	// the process where it stands. Killed outright, it never reached Cleanup,
	// and the headless Chrome drawing its diagrams was left running along with
	// its temporary profile directory.
	//
	// Nothing else cancels ctx, so it is done only once a signal has arrived.
	// The first signal is all it needs, and while NotifyContext stays
	// registered every later one is swallowed too. Letting go of them as soon
	// as the first arrives puts the default back, so a second Ctrl-C still ends
	// a run that is taking too long to reach a file boundary -- at the price of
	// the cleanup the first one was waiting for.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
		log.Warn().Msgf("%s: stopping after the current file; send it again to stop now", context.Cause(ctx))
	}()

	if err := util.Run(ctx, cmd, os.Args); err != nil {
		// "context canceled" says nothing about who cancelled it.
		if errors.Is(err, context.Canceled) {
			err = context.Cause(ctx)
		}
		log.Fatal().Msg(err.Error())
	}
}
