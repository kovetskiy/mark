package util

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
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
			&cli.StringFlag{Name: "mermaid-engine", Value: "chrome"},
			&cli.StringFlag{Name: "mermaid-macro-name", Value: "mermaid-macro"},
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

// writeMermaidDoc is a document with one mermaid fence, publishable with
// --compile-only.
func writeMermaidDoc(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "page.md")
	require.NoError(t, os.WriteFile(path,
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\n```mermaid\nflowchart TD\nA --> B\n```\n"), 0o600))

	return path
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

	t.Run("max is accepted", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--content-appearance", "max"})
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

	t.Run("macro is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro"}))
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

	// With macro nothing is drawn, so the settings that drive drawing have no
	// effect. The bundle is refused like it is with a PNG; the engine and scale
	// are warned about where the run is configured, not refused, because a
	// value may come from the configuration file or the environment.
	t.Run("a bundle in a macro is refused", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-bundle"}))
	})

	t.Run("a scale with macro asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-scale", "2"}))
	})

	t.Run("an engine with macro asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-engine", "merman"}))
	})

	// An invalid value is not one either: nothing is drawn, so a negative scale
	// and a name mark does not know are simply unused.
	t.Run("an invalid scale with macro asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-scale", "-1"}))
	})

	t.Run("an invalid engine with macro asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-engine", "bogus"}))
	})
}

// TestMermaidMacroNameFlagValidation covers the macro key a diagram is
// published under with --mermaid-output=macro. An empty name is refused; a name
// beside an image output is warned about where the run is configured, not
// refused, since the value may come from the configuration file.
func TestMermaidMacroNameFlagValidation(t *testing.T) {
	t.Run("a name with macro is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-macro-name", "mermaid-diagrams"}))
	})

	t.Run("a name without macro is not refused", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-macro-name", "mermaid-diagrams"}))
	})

	t.Run("an empty name is refused", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-macro-name", ""}))
	})

	// A name that is only space is as unusable as an empty one: written into
	// ac:name it publishes "Unknown macro", and the run still exits 0.
	t.Run("a blank name is refused", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-macro-name", "  "}))
	})

	t.Run("a padded name is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-macro-name", " mermaid-diagrams "}))
	})

	// The name is unused beside an image output, where an emptied one is the
	// same "has no effect" case as a name that is set: warned about, not refused.
	t.Run("an empty name beside an image output asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-macro-name", ""}))
	})

	t.Run("the default without macro is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd"}))
	})
}

// TestMermaidMacroOutputNeedsNoDrawingFlags: --mermaid-output=macro on its own
// goes from the CLI through Config into prepare(), which must accept a run that
// asked only for the macro without also naming an engine or a scale. The values
// are the ones a configuration file carries for the image outputs. It runs the
// command line rather than the package's Flags: urfave/cli remembers that a flag
// was set from one run to the next, and a second run would read them.
func TestMermaidMacroOutputNeedsNoDrawingFlags(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	stdout, _, err := runMark(t,
		"--config", config,
		"--compile-only",
		"--mermaid-output", "macro",
		"--mermaid-engine", "bogus",
		"--mermaid-scale", "-1",
		"--files", writeMermaidDoc(t),
	)
	require.NoError(t, err)
	assert.Contains(t, stdout, `ac:name="mermaid-macro"`, "the diagram is published as the macro")
	assert.Contains(t, stdout, "flowchart TD")
}

// TestMermaidMacroNameReachesTheOutput: the name the flag carries is trimmed
// and reaches ac:name.
func TestMermaidMacroNameReachesTheOutput(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	stdout, _, err := runMark(t,
		"--config", config,
		"--compile-only",
		"--mermaid-output", "macro",
		"--mermaid-macro-name", " my-macro ",
		"--files", writeMermaidDoc(t),
	)
	require.NoError(t, err)
	assert.Contains(t, stdout, `ac:name="my-macro"`)
}

// TestMermaidMacroFromTheConfigFile: the output and the name resolve from the
// TOML file, not only from flags.
func TestMermaidMacroFromTheConfigFile(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "mark.toml",
		"mermaid-output = \"macro\"\nmermaid-macro-name = \"cfg-macro\"\n")

	stdout, _, err := runMark(t,
		"--config", config,
		"--compile-only",
		"--files", writeMermaidDoc(t),
	)
	require.NoError(t, err)
	assert.Contains(t, stdout, `ac:name="cfg-macro"`)
}

// TestMermaidMacroFromTheEnvironment: the same two settings resolve from the
// environment.
func TestMermaidMacroFromTheEnvironment(t *testing.T) {
	t.Setenv("MARK_MERMAID_OUTPUT", "macro")
	t.Setenv("MARK_MERMAID_MACRO_NAME", "env-macro")

	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	stdout, _, err := runMark(t,
		"--config", config,
		"--compile-only",
		"--files", writeMermaidDoc(t),
	)
	require.NoError(t, err)
	assert.Contains(t, stdout, `ac:name="env-macro"`)
}

// TestMermaidMacroBlankNameIsRefusedFromEverySource: an emptied name is a
// mistake in a flag, the config file and the environment alike, and a macro run
// says so instead of publishing "Unknown macro".
func TestMermaidMacroBlankNameIsRefusedFromEverySource(t *testing.T) {
	t.Run("the flag", func(t *testing.T) {
		config := writeConfig(t, t.TempDir(), "mark.toml", "")

		_, _, err := runMark(t,
			"--config", config,
			"--compile-only",
			"--mermaid-output", "macro",
			"--mermaid-macro-name", " ",
			"--files", writeMermaidDoc(t),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blank")
	})

	t.Run("the config file", func(t *testing.T) {
		config := writeConfig(t, t.TempDir(), "mark.toml", "mermaid-macro-name = \"\"\n")

		_, _, err := runMark(t,
			"--config", config,
			"--compile-only",
			"--mermaid-output", "macro",
			"--files", writeMermaidDoc(t),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blank")
	})

	t.Run("the environment", func(t *testing.T) {
		t.Setenv("MARK_MERMAID_MACRO_NAME", "")

		config := writeConfig(t, t.TempDir(), "mark.toml", "")

		_, _, err := runMark(t,
			"--config", config,
			"--compile-only",
			"--mermaid-output", "macro",
			"--files", writeMermaidDoc(t),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blank")
	})
}

// TestMermaidMacroWarnsAboutWhatItDoesNotUse: with the macro output the drawing
// settings have no effect, and without the mermaid feature the fence stays a
// code block; both are said out loud.
func TestMermaidMacroWarnsAboutWhatItDoesNotUse(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	t.Run("an engine", func(t *testing.T) {
		_, stderr, err := runMark(t,
			"--config", config,
			"--compile-only",
			"--mermaid-output", "macro",
			"--mermaid-engine", "chrome",
			"--files", writeMermaidDoc(t),
		)
		require.NoError(t, err)
		assert.Contains(t, stderr, "--mermaid-engine has no effect")
	})

	t.Run("a scale", func(t *testing.T) {
		_, stderr, err := runMark(t,
			"--config", config,
			"--compile-only",
			"--mermaid-output", "macro",
			"--mermaid-scale", "2",
			"--files", writeMermaidDoc(t),
		)
		require.NoError(t, err)
		assert.Contains(t, stderr, "--mermaid-scale has no effect")
	})

	t.Run("the feature", func(t *testing.T) {
		_, stderr, err := runMark(t,
			"--config", config,
			"--compile-only",
			"--mermaid-output", "macro",
			"--features", "mention",
			"--files", writeMermaidDoc(t),
		)
		require.NoError(t, err)
		assert.Contains(t, stderr, "needs the mermaid feature")
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

	err := newCommand("test").Run(ctx, []string{
		"mark", "publish",
		"--config", config,
		"--compile-only",
		"--files", document,
	})

	require.ErrorIs(t, err, context.Canceled)
}

// watchedAfterPublish publishes a page to a Confluence that makes its writers
// watch what they write, and is who watches it afterwards.
func watchedAfterPublish(
	t *testing.T, config string, publish func(server, config, document string) error,
) ([]string, error) {
	t.Helper()

	server := confluencetest.New(t)
	server.SetWatchOnWrite()
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	existing := server.AddPage("DOCS", "Page", "page", home.ID)

	dir := t.TempDir()
	path := writeConfig(t, dir, "mark.toml", config)
	document := filepath.Join(dir, "page.md")
	require.NoError(t, os.WriteFile(document,
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

	if err := publish(server.URL, path, document); err != nil {
		return nil, err
	}
	assert.Contains(t, server.Page(existing.ID).Body, "Body.", "the page was published")

	return server.Watchers(existing.ID), nil
}

// TestWatchFlag: publishing leaves the account watching what it wrote, as
// Confluence makes it, unless --watch is false wherever it is given.
func TestWatchFlag(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      map[string]string
		config   string
		args     []string
		bare     bool
		watchers []string
	}{
		{name: "by default", watchers: []string{"acct-current"}},
		{name: "--watch=false", args: []string{"--watch=false"}},
		{name: "--watch=false without publish", args: []string{"--watch=false"}, bare: true},
		{name: "MARK_WATCH=false", env: map[string]string{"MARK_WATCH": "false"}},
		{name: "watch = false", config: "watch = false\n"},
		{name: "--watch over watch = false", config: "watch = false\n", args: []string{"--watch"},
			watchers: []string{"acct-current"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			watchers, err := watchedAfterPublish(t, tc.config, func(server, config, document string) error {
				args := []string{"-c", config, "--color", "never", "-u", "user", "-p", "token"}
				if !tc.bare {
					args = append(args, "publish")
				}
				args = append(args, "-b", server, "--files", document)
				_, _, err := runMark(t, append(args, tc.args...)...)
				return err
			})
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.watchers, watchers)
		})
	}
}

// An empty MARK_WATCH or watch = "" reads as false, which for a flag that is
// on by default would stop watching without anybody asking.
func TestWatchRefusesAnEmptyValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    bool
		config string
		want   string
	}{
		{name: "MARK_WATCH", env: true, want: `environment variable "MARK_WATCH" is empty`},
		{name: "configuration file", config: "watch = \"\"\n", want: `at key "watch" is empty`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env {
				t.Setenv("MARK_WATCH", "")
			}

			_, err := watchedAfterPublish(t, tc.config, func(server, config, document string) error {
				_, _, err := runMark(t, "-c", config, "--color", "never", "-u", "user", "-p", "token",
					"publish", "-b", server, "--files", document)
				return err
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// A program that built mark's single command with flags of its own, and no
// "watch" among them, leaves watches alone.
func TestWatchWithoutTheFlag(t *testing.T) {
	file := &configFile{path: new(string), settled: true}
	flags := slices.DeleteFunc(append(globalFlags(file), publishFlags(file)...),
		func(flag cli.Flag) bool { return flag.Names()[0] == "watch" })

	watchers, err := watchedAfterPublish(t, "", func(server, config, document string) error {
		cmd := &cli.Command{Name: "mark", Flags: flags, Action: RunMark}
		_, _, err := captureOutput(t, func() error {
			return cmd.Run(context.Background(), []string{"mark", "-c", config, "--color", "never",
				"-u", "user", "-p", "token", "-b", server, "--files", document})
		})
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"acct-current"}, watchers)
}
