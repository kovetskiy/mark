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
			&cli.StringFlag{Name: "d2-output", Value: "png"},
			&cli.StringFlag{Name: "mermaid-output", Value: "png"},
			&cli.BoolFlag{Name: "mermaid-bundle"},
			&cli.FloatFlag{Name: "mermaid-scale", Value: 1.0},
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

// TestMermaidOutputFlagValidation covers the two settings that only mean
// anything in one of the two output formats, and the format itself.
func TestMermaidOutputFlagValidation(t *testing.T) {
	t.Run("png is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "png"}))
	})

	t.Run("svg is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "svg"}))
	})

	t.Run("anything else is rejected", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", "jpeg"}))
	})

	// A flag carrying a default is never empty unless somebody emptied it, so
	// an empty value is somebody's doing and not the absence of one. Left to
	// fall through it would publish a PNG without a word, which is the one
	// thing a value set on purpose should not do.
	t.Run("emptied on purpose is rejected", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", ""}))
	})

	// What is checked has to be what is used. Trimmed for the check and passed
	// on whole, " svg " was accepted here and then compared literally by the
	// renderer, which knows no such format and publishes a PNG -- taking a
	// bundle asked for alongside it down with it.
	t.Run("a padded value is not the value it is padding", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", " svg "}))
	})

	t.Run("a scale does not apply to an svg", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", "svg", "--mermaid-scale", "2"}))
	})

	t.Run("a bundle needs an svg to go in", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", "png", "--mermaid-bundle"}))
	})

	// Switching a bundle off is not asking for one. Read as "the flag was
	// mentioned" rather than as its value, mermaid-bundle = false left beside
	// the default PNG output failed a run that had asked for nothing.
	t.Run("but switching one off asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "png", "--mermaid-bundle=false"}))
	})

	t.Run("and a bundle in an svg is the point", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "svg", "--mermaid-bundle"}))
	})
}

// TestD2OutputFlagValidation covers the format a d2 diagram is published as.
// The renderer knows two, and anything else falls to its PNG branch: a diagram
// published as the wrong thing, with nothing said about the SVG that was asked
// for.
func TestD2OutputFlagValidation(t *testing.T) {
	t.Run("png is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--d2-output", "png"}))
	})

	t.Run("svg is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--d2-output", "svg"}))
	})

	t.Run("anything else is rejected", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--d2-output", "jpeg"}))
	})

	t.Run("emptied on purpose is rejected", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--d2-output", ""}))
	})

	// The value goes into the configuration whole and the renderer compares it
	// literally, so what is checked has to be what is used.
	t.Run("a padded value is not the value it is padding", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--d2-output", " svg "}))
	})
}
