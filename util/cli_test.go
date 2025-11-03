package util

import (
	"context"
	"testing"

	"github.com/urfave/cli/v3"
)

func runWithArgs(args []string) error {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "title-from-h1"},
			&cli.BoolFlag{Name: "title-from-filename"},
		},
		Before: CheckMutuallyExclusiveTitleFlags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return nil
		},
	}
	return cmd.Run(context.Background(), args)
}

func TestCheckMutuallyExclusiveTitleFlags(t *testing.T) {
	t.Run("neither flag set", func(t *testing.T) {
		err := runWithArgs([]string{"cmd"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("only title-from-h1 set", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--title-from-h1"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("only title-from-filename set", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--title-from-filename"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("both flags set", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--title-from-h1", "--title-from-filename"})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}
