package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
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
			&cli.StringFlag{Name: "mermaid-engine", Value: "chrome"},
			&cli.StringFlag{Name: "mermaid-output", Value: "png"},
			&cli.BoolFlag{Name: "mermaid-bundle"},
			&cli.StringFlag{Name: "mermaid-macro-name", Value: "mermaid-macro"},
			&cli.FloatFlag{Name: "mermaid-scale", Value: 1.0},
			&cli.StringFlag{Name: "math-output", Value: "png"},
			&cli.FloatFlag{Name: "math-scale", Value: 2.0},
		},
		Before: checkPublishFlags,
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
	t.Run("fixed is refused", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--content-appearance", "fixed"})
		if err == nil || !strings.Contains(err.Error(), `use "default" for a narrow page`) {
			t.Errorf("expected fixed to be refused, got %v", err)
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

// TestMermaidOutputFlagValidation covers the settings that only mean anything
// in one of the output formats; the formats themselves are covered by
// TestMermaidMacroOutputFlagValidation.
func TestMermaidOutputFlagValidation(t *testing.T) {
	// The bundle is checked against the normalised value, the one published.
	t.Run("a padded svg takes a bundle", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", " SVG ", "--mermaid-bundle"}))
	})

	// The scale applies to both formats: it multiplies the pixels of a PNG
	// and the size the page shows an SVG at.
	t.Run("a scale applies to an svg too", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "svg", "--mermaid-scale", "2"}))
	})

	t.Run("a bundle needs an svg to go in", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", "png", "--mermaid-bundle"}))
	})

	// Switching a bundle off is not asking for one, so the bundle is read by
	// its value rather than by whether the flag was mentioned.
	t.Run("but switching one off asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "png", "--mermaid-bundle=false"}))
	})

	t.Run("and a bundle in an svg is the point", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "svg", "--mermaid-bundle"}))
	})
}

// TestMermaidMacroOutputFlagValidation covers the third output: the diagram's
// source is published as the instance's macro, and mark draws nothing.
func TestMermaidMacroOutputFlagValidation(t *testing.T) {
	t.Run("macro is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro"}))
	})

	t.Run("a padded macro is accepted", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", " Macro "}))
	})

	t.Run("jpeg is refused", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--mermaid-output", "jpeg"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "png, svg or macro")
	})

	t.Run("an emptied output is refused", func(t *testing.T) {
		assert.Error(t, runWithArgs([]string{"cmd", "--mermaid-output", ""}))
	})

	t.Run("a bundle in a macro is refused", func(t *testing.T) {
		err := runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-bundle"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the macro publishes the diagram's source itself")
	})

	// Nothing is drawn, so the drawing settings are unused.
	t.Run("a scale with macro asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-scale", "-1"}))
	})

	t.Run("an engine with macro asks for nothing", func(t *testing.T) {
		assert.NoError(t, runWithArgs([]string{"cmd", "--mermaid-output", "macro", "--mermaid-engine", "bogus"}))
	})
}

// TestMermaidMacroNameFlagValidation covers the macro key a diagram is
// published under with --mermaid-output=macro.
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

// TestMermaidMacroOutputNeedsNoDrawingFlags: --mermaid-output=macro goes from
// the CLI through Config into the validation, which must accept a run that
// asked only for the macro without also naming a usable engine or scale.
func TestMermaidMacroOutputNeedsNoDrawingFlags(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	stdout, _, err := runMark(t, "publish",
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

	stdout, _, err := runMark(t, "publish",
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

	stdout, _, err := runMark(t, "publish",
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

	stdout, _, err := runMark(t, "publish",
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

		_, _, err := runMark(t, "publish",
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

		_, _, err := runMark(t, "publish",
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

		_, _, err := runMark(t, "publish",
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
		_, stderr, err := runMark(t, "publish",
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
		_, stderr, err := runMark(t, "publish",
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
		_, stderr, err := runMark(t, "publish",
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

// TestMermaidMacroFeatureWarningIsOncePerRun: the settings are checked once for
// a run, not once per file, so a run over several documents warns about the
// feature once.
func TestMermaidMacroFeatureWarningIsOncePerRun(t *testing.T) {
	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name),
			[]byte("<!-- Space: DOCS -->\n<!-- Title: "+name+" -->\n\n```mermaid\nflowchart TD\nA --> B\n```\n"),
			0o600))
	}

	_, stderr, err := runMark(t, "publish",
		"--config", config,
		"--compile-only",
		"--mermaid-output", "macro",
		"--features", "mention",
		"--files", filepath.Join(dir, "*.md"),
	)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(stderr, "needs the mermaid feature"))
}

// TestImageOutputFlagsShareOneRule: the image --X-output flags (d2 and math)
// take the same values, padded or not, and refuse the rest in the same words.
// Mermaid has its own test: its output may be the macro as well.
func TestImageOutputFlagsShareOneRule(t *testing.T) {
	for _, name := range imageOutputFlags {
		for _, value := range []string{"png", "svg", " SVG ", "Png\t"} {
			t.Run(name+" accepts "+value, func(t *testing.T) {
				assert.NoError(t, runWithArgs([]string{"cmd", "--" + name, value}))
			})
		}

		for _, value := range []string{"jpeg", "", " "} {
			t.Run(name+" refuses "+value, func(t *testing.T) {
				err := runWithArgs([]string{"cmd", "--" + name, value})
				require.Error(t, err)
				assert.Equal(t, fmt.Sprintf("invalid value for --%s: %q (expected: svg or png)", name, value), err.Error())
			})
		}
	}
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
		watchers []string
	}{
		{name: "by default", watchers: []string{"acct-current"}},
		{name: "--watch=false", args: []string{"--watch=false"}},
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
				args := []string{"-c", config, "--color", "never", "-u", "user", "-p", "token",
					"publish", "-b", server, "--files", document}
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

// A program that built a command of its own on RunPublish, and no "watch"
// among its flags, leaves watches alone.
func TestWatchWithoutTheFlag(t *testing.T) {
	file := &configFile{path: new(string), settled: true}
	flags := slices.DeleteFunc(append(globalFlags(file), publishFlags(file)...),
		func(flag cli.Flag) bool { return flag.Names()[0] == "watch" })

	watchers, err := watchedAfterPublish(t, "", func(server, config, document string) error {
		cmd := &cli.Command{Name: "mark", Flags: flags, Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := SetUpLogging(cmd); err != nil {
				return err
			}
			return RunPublish(ctx, cmd)
		}}
		_, _, err := captureOutput(t, func() error {
			return cmd.Run(context.Background(), []string{"mark", "-c", config, "--color", "never",
				"-u", "user", "-p", "token", "-b", server, "--files", document})
		})
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"acct-current"}, watchers)
}

// --watch=false typed on the command line asks for no watch, so an empty
// MARK_WATCH or watch = "" beside it is set aside rather than refused.
func TestWatchFalseOnTheCommandLineOverAnEmptyValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    bool
		config string
	}{
		{name: "MARK_WATCH", env: true},
		{name: "configuration file", config: "watch = \"\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env {
				t.Setenv("MARK_WATCH", "")
			}

			watchers, err := watchedAfterPublish(t, tc.config, func(server, config, document string) error {
				_, _, err := runMark(t, "-c", config, "--color", "never", "-u", "user", "-p", "token",
					"publish", "-b", server, "--files", document, "--watch=false")
				return err
			})
			require.NoError(t, err)
			assert.Empty(t, watchers)
		})
	}
}

// generated.by names mark's version without the commit the binary's version
// carries, so two builds of a release write the same concepts.
func TestGenerator(t *testing.T) {
	assert.Equal(t, "mark/v17.0.0", generator("v17.0.0@3f2a9c1"))
	assert.Equal(t, "mark/dev", generator("dev@none"))
	assert.Equal(t, "mark/v17.0.0", generator("v17.0.0"))
	assert.Empty(t, generator(""))
	assert.Empty(t, generator("@3f2a9c1"))
}

// TestPublishURLOnAnotherInstanceIsRefused: with a base URL configured, a
// --url on another instance is refused before anything is sent, rather than
// the page with its id on the configured instance being written. The same
// instance written another way still publishes, to the configured base URL.
func TestPublishURLOnAnotherInstanceIsRefused(t *testing.T) {
	noDefaultConfig(t)

	for _, tc := range []struct {
		name    string
		pageURL func(server, id string) string
		refused bool
	}{
		{name: "another host", refused: true, pageURL: func(_, id string) string {
			return "https://b.example/pages/viewpage.action?pageId=" + id
		}},
		{name: "same instance", pageURL: func(server, id string) string {
			return server + "/pages/viewpage.action?pageId=" + id
		}},
		{name: "same instance in capitals, under /wiki", pageURL: func(server, id string) string {
			return strings.ToUpper(server) + "/wiki/spaces/DOCS/pages/" + id + "/Page"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			existing := server.AddPage("DOCS", "Page", "page", home.ID)

			dir := t.TempDir()
			config := writeConfig(t, dir, "mark.toml", "base-url = \""+server.URL+"\"\n")
			document := filepath.Join(dir, "page.md")
			require.NoError(t, os.WriteFile(document, []byte("Body.\n"), 0o600))

			_, _, err := runMark(t, "-c", config, "--color", "never", "-u", "user", "-p", "token",
				"publish", "--url", tc.pageURL(server.URL, existing.ID), "--files", document)

			if tc.refused {
				require.ErrorIs(t, err, confluence.ErrOtherInstance)
				assert.ErrorContains(t, err, "--url is not a page of the instance the base URL")
				assert.Zero(t, server.CountRequests("PUT", ""), "nothing was written")
				assert.NotContains(t, server.Page(existing.ID).Body, "Body.")
				return
			}
			require.NoError(t, err)
			assert.Contains(t, server.Page(existing.ID).Body, "Body.")
		})
	}
}

// TestPublishURLWithoutPageID: a --url that names a page without its id is
// refused before anything is sent, rather than the document's own headers
// deciding where it goes; a short link, which carries the id, publishes to
// the page it names.
func TestPublishURLWithoutPageID(t *testing.T) {
	noDefaultConfig(t)

	for _, tc := range []struct {
		name    string
		pageURL func(server, id string) string
		refused bool
	}{
		{name: "display form", refused: true, pageURL: func(server, _ string) string {
			return server + "/display/DOCS/Page"
		}},
		{name: "bare id", refused: true, pageURL: func(_, id string) string { return id }},
		{name: "short link", pageURL: func(server, id string) string {
			link, err := confluence.GenerateTinyLink(server, id)
			require.NoError(t, err)
			return link
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			existing := server.AddPage("DOCS", "Page", "page", home.ID)

			dir := t.TempDir()
			config := writeConfig(t, dir, "mark.toml", "base-url = \""+server.URL+"\"\n")
			document := filepath.Join(dir, "page.md")
			require.NoError(t, os.WriteFile(document,
				[]byte("<!-- Space: DOCS -->\n<!-- Title: Another page -->\n\nBody.\n"), 0o600))

			_, _, err := runMark(t, "-c", config, "--color", "never", "-u", "user", "-p", "token",
				"publish", "--url", tc.pageURL(server.URL, existing.ID), "--files", document)

			if tc.refused {
				require.ErrorIs(t, err, ErrNoPageID)
				assert.Zero(t, server.CountRequests("POST", ""), "no page was created")
				assert.Zero(t, server.CountRequests("PUT", ""), "nothing was written")
				return
			}
			require.NoError(t, err)
			assert.Contains(t, server.Page(existing.ID).Body, "Body.")
			assert.Zero(t, server.CountRequests("POST", "/content"), "no page was created")
		})
	}
}

// TestPublishURLSetAsideOnTheCommandLine: an empty --url sets aside a url the
// configuration file keeps for mark export, and the document's headers name
// the page.
func TestPublishURLSetAsideOnTheCommandLine(t *testing.T) {
	noDefaultConfig(t)

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	config := writeConfig(t, dir, "mark.toml",
		"base-url = \""+server.URL+"\"\nurl = \""+server.URL+"/display/DOCS/Home\"\n")
	document := filepath.Join(dir, "page.md")
	require.NoError(t, os.WriteFile(document,
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Another page -->\n\nBody.\n"), 0o600))

	_, _, err := runMark(t, "-c", config, "--color", "never", "-u", "user", "-p", "token",
		"publish", "--files", document)
	require.ErrorIs(t, err, ErrNoPageID)
	assert.ErrorContains(t, err, "configuration file")
	assert.Zero(t, server.CountRequests("POST", ""), "no page was created")

	_, _, err = runMark(t, "-c", config, "--color", "never", "-u", "user", "-p", "token",
		"publish", "--url", "", "--files", document)
	require.NoError(t, err)
	assert.Equal(t, 1, server.CountRequests("POST", "/content"), "the page the headers name was created")
}
