package util

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

func runWithArgs(args []string) error {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "title-from-h1"},
			&cli.BoolFlag{Name: "title-from-filename"},
			&cli.StringFlag{Name: "content-appearance"},
		},
		Before: CheckFlags,
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

func TestContentAppearanceFlagValidation(t *testing.T) {
	t.Run("fixed is accepted", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--content-appearance", "fixed"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("full-width is accepted", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--content-appearance", "full-width"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid value is rejected", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--content-appearance", "nope"})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func Test_setLogLevel(t *testing.T) {
	type args struct {
		lvl string
	}
	tests := map[string]struct {
		args        args
		want        zerolog.Level
		expectedErr string
	}{
		"invalid": {args: args{lvl: "INVALID"}, want: zerolog.InfoLevel, expectedErr: "unknown log level: INVALID"},
		"empty":   {args: args{lvl: ""}, want: zerolog.InfoLevel, expectedErr: "unknown log level: "},
		"info":    {args: args{lvl: "INFO"}, want: zerolog.InfoLevel},
		"debug":   {args: args{lvl: "DEBUG"}, want: zerolog.DebugLevel},
		"trace":   {args: args{lvl: "TRACE"}, want: zerolog.TraceLevel},
		"warning": {args: args{lvl: "WARNING"}, want: zerolog.WarnLevel},
		"error":   {args: args{lvl: "ERROR"}, want: zerolog.ErrorLevel},
		"fatal":   {args: args{lvl: "FATAL"}, want: zerolog.FatalLevel},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			prev := zerolog.GlobalLevel()
			t.Cleanup(func() { zerolog.SetGlobalLevel(prev) })
			cmd := &cli.Command{
				Name: "test",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "log-level",
						Value: tt.args.lvl,
						Usage: "set the log level. Possible values: TRACE, DEBUG, INFO, WARNING, ERROR, FATAL.",
					},
				},
			}
			err := SetLogLevel(cmd)
			if tt.expectedErr != "" {
				assert.EqualError(t, err, tt.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, zerolog.GlobalLevel())
			}
		})
	}
}
