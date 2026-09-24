package util

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func runWithArgs(args []string) error {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "title-from-h1"},
			&cli.BoolFlag{Name: "title-from-filename"},
			&cli.StringFlag{Name: "content-appearance"},
			&cli.StringFlag{Name: "d2-output", Value: "png"},
			&cli.FloatFlag{Name: "d2-scale", Value: 1.0},
			&cli.StringFlag{Name: "mermaid-output", Value: "png"},
			&cli.BoolFlag{Name: "mermaid-bundle"},
			&cli.FloatFlag{Name: "mermaid-scale", Value: 1.0},
			&cli.FloatFlag{Name: "math-scale", Value: 2.0},
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

	// The scale applies to both formats now: it multiplies the pixels of a PNG
	// and the size the page shows an SVG at.
	t.Run("a scale applies to an svg too", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "svg", "--mermaid-scale", "2"}))
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

// TestD2ScaleFlagValidation covers the numbers that are not a scale. Zero
// renders nothing and a negative one renders nonsense; NaN and an infinity are
// worse, because a check written as "not <= 0" lets them both through and they
// reach Chrome as a screenshot scale it cannot use.
func TestD2ScaleFlagValidation(t *testing.T) {
	t.Run("a positive scale is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--d2-scale", "2"}))
	})

	for _, scale := range []string{"0", "-1", "NaN", "Inf", "+Inf"} {
		t.Run(scale+" is rejected", func(t *testing.T) {
			assert.Error(t, runWithArgs([]string{"cmd", "--d2-scale", scale}))
		})
	}
}

// TestMermaidAndMathScaleFlagValidation is the same for the other two scales.
// Mermaid took any number to the browser, where zero and a negative one waited
// out the render timeout and NaN or an infinity could not be sent at all; math
// quietly drew zero and a negative scale at 2, and let NaN through.
func TestMermaidAndMathScaleFlagValidation(t *testing.T) {
	for _, flag := range []string{"--mermaid-scale", "--math-scale"} {
		t.Run(flag+" accepts a positive scale", func(t *testing.T) {
			assert.NoError(t, runWithArgs([]string{"cmd", flag, "1.5"}))
		})

		for _, scale := range []string{"0", "-1", "NaN", "Inf", "-Inf"} {
			t.Run(flag+" rejects "+scale, func(t *testing.T) {
				err := runWithArgs([]string{"cmd", flag, scale})
				require.Error(t, err)
				assert.Contains(t, err.Error(), flag)
			})
		}
	}
}

// TestRunPublishStopsWhenCancelled: the CLI called mark.Run, which runs under a
// context of its own, so a cancelled one -- which is what Ctrl-C now produces --
// went unnoticed and every file was processed anyway. Cancelled before the first
// file, nothing is compiled and the run says why it stopped.
func TestRunPublishStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "mark.toml")
	require.NoError(t, os.WriteFile(config, nil, 0o600))

	document := filepath.Join(dir, "page.md")
	require.NoError(t, os.WriteFile(document,
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

	restore := log.Logger
	t.Cleanup(func() { log.Logger = restore })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Run(ctx, NewCommand("test"), []string{
		"mark", "publish",
		"--config", config,
		"--compile-only",
		"--files", document,
	})

	require.ErrorIs(t, err, context.Canceled)
}
