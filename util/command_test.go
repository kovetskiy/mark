package util

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// noDefaultConfig points the default configuration file at an empty
// directory, so that a developer's own ~/.config/mark.toml stays out of a test.
func noDefaultConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

// dispatch runs mark's command line with publishing swapped out, and says
// what would have been published: the files publish was given, or nil.
func dispatch(t *testing.T, args ...string) (*string, string, error) {
	t.Helper()

	var got *string
	cmd := buildCommand("test", func(_ context.Context, c *cli.Command) error {
		files := c.String("files")
		got = &files
		return nil
	}, nil)
	var out bytes.Buffer
	cmd.Writer, cmd.ErrWriter = &out, &out

	_, logged, err := captureOutput(t, func() error {
		return cmd.Run(context.Background(), append([]string{"mark"}, args...))
	})

	return got, out.String() + logged, err
}

// TestTheRootCommand covers a command line that names no command, or names
// publish's flags before it, which fails naming the command the flag belongs
// to.
func TestTheRootCommand(t *testing.T) {
	noDefaultConfig(t)
	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	hint := `it is a flag of "publish", so write it after "publish"`
	for _, test := range []struct {
		name  string
		args  []string
		want  string
		error string
	}{
		{"publish named", []string{"publish", "-f", "a.md"}, "a.md", ""},
		{"global flags before publish", []string{"-u", "me", "--config", config, "publish", "-f", "a.md"}, "a.md", ""},
		{"a stray argument after publish", []string{"publish", "-f", "a.md", "b.md"}, "", `unexpected argument "b.md"`},
		{"publish's flags alone", []string{"-f", "a.md", "--dry-run"}, "", "flag provided but not defined: -f: " + hint},
		{"an inline value", []string{"--files=a.md"}, "", "flag provided but not defined: -files: " + hint},
		{"after a global flag", []string{"-c", config, "-u", "me", "-f", "a.md"}, "", hint},
		{"a publish flag before publish", []string{"-f", "a.md", "publish"}, "", hint},
		{"a global flag's value that reads as a command", []string{"-u", "publish", "-f", "a.md"}, "", hint},
		{"a flag of no command", []string{"--no-such-flag"}, "", "flag provided but not defined: -no-such-flag"},
		{"a mistyped command", []string{"publsh", "--compile-only", "-f", "x.md"}, "", `unknown command "publsh": did you mean "publish"?`},
		{"an argument that is no command", []string{"doc.md"}, "", `unknown command "doc.md"`},
		{"an argument that is no command, then publish's flags", []string{"doc.md", "-f", "a.md"}, "", `unknown command "doc.md"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _, err := dispatch(t, test.args...)
			if test.error != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.error)
				if !strings.Contains(test.error, hint) {
					assert.NotContains(t, err.Error(), hint)
				}
				assert.Nil(t, got, "nothing is published")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, test.want, *got)
		})
	}
}

// TestMarkOnItsOwn: "mark" alone shows the help and fails, so that a pipeline
// still written for "mark" with its settings in the environment does not pass.
func TestMarkOnItsOwn(t *testing.T) {
	noDefaultConfig(t)
	t.Setenv("MARK_FILES", "env.md")

	got, out, err := dispatch(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no command given: run "mark publish" to publish`)
	assert.Nil(t, got)
	assert.Contains(t, out, "COMMANDS:")
	assert.NotContains(t, out, "--files", "the help is mark's, not publish's")
}

// TestPublishResolvesFlags: the global flags belong to the root command and
// are resolved as it is parsed, while the file they may come from can be named
// after "publish", once that has happened. Each flag has to come out the same
// wherever it was written and wherever the file was named.
func TestPublishResolvesFlags(t *testing.T) {
	dir := t.TempDir()
	config := writeConfig(t, dir, "mark.toml",
		"username = \"cfguser\"\nbase-url = \"http://cfg.invalid\"\n"+
			"insecure-skip-tls-verify = true\nlog-level = \"error\"\n"+
			"space = \"CFG\"\nfiles = \"from-config.md\"\n")

	names := []string{"username", "base-url", "insecure-skip-tls-verify", "log-level", "space", "files"}

	fromFile := map[string]string{
		"username":                 "cfguser",
		"base-url":                 "http://cfg.invalid",
		"insecure-skip-tls-verify": "true",
		"log-level":                "error",
		"space":                    "CFG",
		"files":                    "from-config.md",
	}

	for _, test := range []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"file named after publish", nil, []string{"publish", "--config", config}},
		{"file named before publish", nil, []string{"--config", config, "publish"}},
		{"file named in the environment", map[string]string{"MARK_CONFIG": config}, []string{"publish"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolved := resolveFlags(t, test.env, names, test.args...)
			for name, want := range fromFile {
				assert.Equal(t, want, resolved[name], name)
			}
		})
	}

	t.Run("the command line wins over the file, on either side of publish", func(t *testing.T) {
		resolved := resolveFlags(t, nil, []string{"username", "base-url", "space"},
			"-u", "before", "publish", "-c", config, "-b", "http://after.invalid", "--space", "CLI")
		assert.Equal(t, "before", resolved["username"])
		assert.Equal(t, "http://after.invalid", resolved["base-url"])
		assert.Equal(t, "CLI", resolved["space"])
	})

	t.Run("the environment wins over the file", func(t *testing.T) {
		resolved := resolveFlags(t,
			map[string]string{"MARK_USERNAME": "envuser", "MARK_SPACE": "ENV"},
			[]string{"username", "space"},
			"publish", "-c", config)
		assert.Equal(t, "envuser", resolved["username"])
		assert.Equal(t, "ENV", resolved["space"])
	})

	t.Run("a flag on the command line wins over the environment", func(t *testing.T) {
		resolved := resolveFlags(t,
			map[string]string{"MARK_USERNAME": "envuser", "MARK_SPACE": "ENV"},
			[]string{"username", "space"},
			"publish", "-u", "cli", "--space", "CLI")
		assert.Equal(t, "cli", resolved["username"])
		assert.Equal(t, "CLI", resolved["space"])
	})
}

// TestConfigNamedAfterPublishReplacesTheDefault: had the global flags read the
// file through their own sources, they would have read the default one -- the
// root command is resolved before "publish --config X" is even parsed -- and
// kept whatever it held, whatever X said.
func TestConfigNamedAfterPublishReplacesTheDefault(t *testing.T) {
	skipWithoutUnixConfigDir(t)

	home := t.TempDir()
	configDir := userConfigDir(t, home)
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	writeConfig(t, configDir, "mark.toml",
		"username = \"default-user\"\nbase-url = \"http://default.invalid\"\n")

	named := writeConfig(t, t.TempDir(), "named.toml", "username = \"named-user\"\n")

	env := map[string]string{"HOME": home, "XDG_CONFIG_HOME": ""}

	resolved := resolveFlags(t, env, []string{"config", "username", "base-url"}, "publish", "--config", named)
	assert.Equal(t, named, resolved["config"])
	assert.Equal(t, "named-user", resolved["username"])
	assert.Empty(t, resolved["base-url"], "a setting only the default file holds must not leak into a run that named another")

	resolved = resolveFlags(t, env, []string{"username", "base-url"}, "publish")
	assert.Equal(t, "default-user", resolved["username"], "the default file is still read when none is named")
	assert.Equal(t, "http://default.invalid", resolved["base-url"])
}

// captureOutput runs do with stdout and stderr captured, and returns what it
// wrote to each, restoring the logger it may set up on the way.
func captureOutput(t *testing.T, do func() error) (string, string, error) {
	t.Helper()

	dir := t.TempDir()
	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	require.NoError(t, err)
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	require.NoError(t, err)

	originalStdout, originalStderr := os.Stdout, os.Stderr
	originalLogger, originalLevel := log.Logger, zerolog.GlobalLevel()
	os.Stdout, os.Stderr = stdout, stderr
	t.Cleanup(func() {
		os.Stdout, os.Stderr = originalStdout, originalStderr
		log.Logger = originalLogger
		zerolog.SetGlobalLevel(originalLevel)
	})

	runErr := do()

	os.Stdout, os.Stderr = originalStdout, originalStderr
	require.NoError(t, stdout.Close())
	require.NoError(t, stderr.Close())

	out, err := os.ReadFile(stdout.Name())
	require.NoError(t, err)
	logged, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)

	return string(out), string(logged), runErr
}

// runMark runs mark's command line, as main does, in this process and returns
// what it wrote to stdout and stderr.
func runMark(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	return captureOutput(t, func() error {
		return Run(context.Background(), "test", append([]string{"mark"}, args...))
	})
}

// publishable is a configuration file and a document that publishes with
// --compile-only.
func publishable(t *testing.T) (config, document string) {
	t.Helper()

	dir := t.TempDir()
	config = writeConfig(t, dir, "mark.toml", "")
	document = filepath.Join(dir, "page.md")
	require.NoError(t, os.WriteFile(document,
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

	return config, document
}

// TestContinueOnErrorStillFailsTheRun: --continue-on-error processes every
// file even when some fail, but the run still has to fail so that CI notices.
func TestContinueOnErrorStillFailsTheRun(t *testing.T) {
	noDefaultConfig(t)

	files := filepath.Join("..", "testdata", "batch-tests", "*.md")

	out, _, err := runMark(t, "publish", "--color", "never",
		"--compile-only", "--continue-on-error", "--files", files)

	require.Error(t, err)
	assert.ErrorContains(t, err, "one or more files failed to process")
	assert.Contains(t, out, ">e</h5>", "a file after the first failure is still compiled")
}

// TestBrokenConfigFileLeavesHelpAlone: help and completion read nothing from
// the configuration file, so one that does not parse must not break them.
// publish, which does read it, says so.
func TestBrokenConfigFileLeavesHelpAlone(t *testing.T) {
	t.Setenv("MARK_CONFIG", writeConfig(t, t.TempDir(), "mark.toml", "x = ["))

	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		cmd := newCommand("test")
		cmd.Writer, cmd.ErrWriter = &out, &bytes.Buffer{}
		err := cmd.Run(context.Background(), append([]string{"mark"}, args...))
		return out.String(), err
	}

	for _, args := range [][]string{
		{"help"},
		{"help", "publish"},
		{"completion", "bash"},
		{"--generate-shell-completion"},
		{"publish", "--generate-shell-completion"},
		{"publish", "--help"},
		{"publish", "help"},
		{"publish", "h"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := run(args...)
			require.NoError(t, err)
			// The root's completions come from os.Args, which in a test are
			// the test binary's, so only the error says anything there.
			if args[0] != "--generate-shell-completion" {
				assert.NotEmpty(t, out)
			}
		})
	}

	t.Run("publish", func(t *testing.T) {
		_, err := run("publish", "--compile-only", "--files", "doc.md")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to parse configuration file")
	})
}

// helpOutput is what the command line prints for args.
func helpOutput(t *testing.T, version string, args ...string) string {
	t.Helper()

	var out bytes.Buffer
	cmd := newCommand(version)
	cmd.Writer = &out

	require.NoError(t, cmd.Run(context.Background(), append([]string{"mark"}, args...)))

	return out.String()
}

// TestHelp: "mark --help" is about mark as a whole -- its commands and the
// flags they share -- and "mark publish --help" and "mark export --help"
// about the one command.
func TestHelp(t *testing.T) {
	t.Run("mark --help", func(t *testing.T) {
		help := helpOutput(t, "test", "--help")
		assert.Contains(t, help, "COMMANDS:")
		assert.Regexp(t, `(?m)^\s+publish\s`, help)
		assert.Regexp(t, `(?m)^\s+export\s`, help)
		assert.Contains(t, help, "--username")
		assert.Contains(t, help, "--config")
		assert.NotContains(t, help, "--files", "publish flags belong to publish's help")
	})

	t.Run("mark publish --help", func(t *testing.T) {
		help := helpOutput(t, "test", "publish", "--help")
		assert.Contains(t, help, "mark publish")
		assert.Contains(t, help, "--files")
		assert.Contains(t, help, "--features")
		assert.Contains(t, help, "GLOBAL OPTIONS:")
		assert.Contains(t, help, "--username", "the global flags are listed with it")
	})

	t.Run("mark export --help", func(t *testing.T) {
		help := helpOutput(t, "test", "export", "--help")
		assert.Contains(t, help, "mark export")
		assert.Contains(t, help, "--page-id")
		assert.Contains(t, help, "--attachments-dir")
		assert.NotContains(t, help, "--files", "publish flags belong to publish's help")
		assert.Contains(t, help, "--username", "the global flags are listed with it")
	})

	t.Run("mark help publish", func(t *testing.T) {
		assert.Contains(t, helpOutput(t, "test", "help", "publish"), "--files")
	})

	t.Run("mark --version", func(t *testing.T) {
		assert.Equal(t, "mark version v1.2.3@abc\n", helpOutput(t, "v1.2.3@abc", "--version"))
	})

}

// readmeVersion is what the README's help blocks show for the version: the
// same for every release, so a release leaves them as they are.
const readmeVersion = "vX.Y.Z"

var updateREADME = flag.Bool("update-readme", false, "rewrite README.md's help blocks from the binary")

// TestREADMEUsageMatchesHelp keeps the help the README shows in step with what
// the binary prints, placeholders aside: the version, and the home directory
// in the default configuration file. To bring README.md up to date, run
//
//	go test ./util -run TestREADMEUsageMatchesHelp -args -update-readme
func TestREADMEUsageMatchesHelp(t *testing.T) {
	skipWithoutUnixConfigDir(t)
	// The README shows the default file where Linux keeps it.
	defaultFile := filepath.Join(userConfigDir(t, "${HOME}"), "mark.toml")
	shown := func(help string) string {
		return strings.ReplaceAll(help, defaultFile, "${HOME}/.config/mark.toml")
	}

	path := filepath.Join("..", "README.md")
	readme, err := os.ReadFile(path)
	require.NoError(t, err)

	updated := string(readme)
	for _, block := range []struct {
		heading string
		args    []string
	}{
		{"mark", []string{"--help"}},
		{"mark publish", []string{"publish", "--help"}},
		{"mark export", []string{"export", "--help"}},
	} {
		pattern := regexp.MustCompile("(?s)```text\nNAME:\n   " + regexp.QuoteMeta(block.heading) + " - .*?```\n")
		found := pattern.FindString(updated)
		require.NotEmpty(t, found, "README.md has no help block for %q", block.heading)

		want := "```text\n" + shown(helpOutput(t, readmeVersion, block.args...)) + "```\n"
		if *updateREADME {
			updated = strings.Replace(updated, found, want, 1)
			continue
		}
		if found != want {
			t.Errorf("README.md's %q help block has drifted from the binary. Run\n\n"+
				"\tgo test ./util -run TestREADMEUsageMatchesHelp -args -update-readme\n\n"+
				"or replace the block with:\n\n%s", "mark "+strings.Join(block.args, " "), want)
		}
	}

	if *updateREADME {
		require.NoError(t, os.WriteFile(path, []byte(updated), 0o644)) //nolint:gosec // G306: README.md is not secret
	}
}

// TestPublishHelpLeavesTheFlagsAlone: "mark publish help" prints help, whatever
// publish's flags would have made of the environment.
func TestPublishHelpLeavesTheFlagsAlone(t *testing.T) {
	t.Setenv("MARK_FEATURES", "bogus")

	for _, args := range [][]string{{"publish", "help"}, {"publish", "h"}, {"help", "publish"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			assert.Contains(t, helpOutput(t, "test", args...), "--files")
		})
	}
}

// TestHelpComesBeforeTheVersion, as it did before there were commands.
func TestHelpComesBeforeTheVersion(t *testing.T) {
	assert.Contains(t, helpOutput(t, "v1.2.3@abc", "--help", "--version"), "COMMANDS:")
	assert.Contains(t, helpOutput(t, "v1.2.3@abc", "--version", "--help"), "COMMANDS:")
}

// TestTheConfigDumpNamesEachFlagOnce: the root and publish both carry a help
// flag, and the dump went over both lists.
func TestTheConfigDumpNamesEachFlagOnce(t *testing.T) {
	config, document := publishable(t)

	_, logged, err := runMark(t, "publish", "--config", config, "--log-level", "debug",
		"--color", "never", "--compile-only", "-f", document)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(logged, " help: "), logged)
	assert.Equal(t, 1, strings.Count(logged, " username: "), logged)
	assert.Equal(t, 1, strings.Count(logged, " files: "), logged)
}

// TestTheConfigFileIsReadOnce: every flag looks its setting up in the file,
// and the file is read and parsed at the first lookup, not once a flag.
func TestTheConfigFileIsReadOnce(t *testing.T) {
	path := writeConfig(t, t.TempDir(), "mark.toml", "username = \"first\"\n")
	file := newConfigFile(&path)

	value, found := file.value("username").Lookup()
	require.True(t, found)
	assert.Equal(t, "first", value)

	writeConfig(t, filepath.Dir(path), "mark.toml", "username = \"second\"\n")

	value, _ = file.value("username").Lookup()
	assert.Equal(t, "first", value)

	other := writeConfig(t, t.TempDir(), "other.toml", "username = \"other\"\n")
	path = other
	value, _ = file.value("username").Lookup()
	assert.Equal(t, "other", value, "a new path is read afresh")
}

// TestAHeldSourceOpensAtSettle: a held source answers nothing until settle,
// and then what it holds.
func TestAHeldSourceOpensAtSettle(t *testing.T) {
	path := writeConfig(t, t.TempDir(), "mark.toml", "username = \"me\"\n")
	file := newConfigFile(&path)
	held := file.hold(file.value("username"))

	_, found := held.Lookup()
	assert.False(t, found)

	require.NoError(t, file.settle(&cli.Command{}))

	value, found := held.Lookup()
	assert.True(t, found)
	assert.Equal(t, "me", value)
}

// TestShortFlagsMeanOneThing: a letter learned in one command should work, and
// mean the same, in every other that takes the argument.
func TestShortFlagsMeanOneThing(t *testing.T) {
	// Different names for one argument: a page given by its address.
	sameArgument := map[string]string{"url": "target-url"}
	argument := func(name string) string {
		if same, ok := sameArgument[name]; ok {
			return same
		}

		return name
	}

	shorts := map[string]string{} // long name -> its short flags, as first seen
	longs := map[string]string{}  // short flag -> the argument it stands for
	where := map[string]string{}  // long name -> the command it was first seen in
	var walk func(path string, cmd *cli.Command)
	walk = func(path string, cmd *cli.Command) {
		for _, f := range cmd.Flags {
			names := f.Names()
			long, aliases := names[0], strings.Join(names[1:], ",")

			if seen, ok := shorts[long]; ok {
				assert.Equal(t, seen, aliases,
					"--%s has short flags %q in %q but %q in %q", long, seen, where[long], aliases, path)
			} else {
				shorts[long], where[long] = aliases, path
			}

			for _, short := range names[1:] {
				if other, ok := longs[short]; ok {
					assert.Equal(t, other, argument(long), "-%s stands for --%s and --%s", short, other, long)
				} else {
					longs[short] = argument(long)
				}
			}
		}

		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}
	walk("mark", newCommand(readmeVersion))
}

// TestMathFormatIsPublishedAsChecked: the check trims --math-format, so a
// padded value from any source has to reach the configuration trimmed as well,
// or Config's validation refuses it as an invalid MathFormat.
func TestMathFormatIsPublishedAsChecked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		env    string
		args   []string
	}{
		{name: "flag", args: []string{"--math-format", " svg "}},
		{name: "environment", env: " svg "},
		{name: "configuration file", config: "math-format = \" svg \"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noDefaultConfig(t)
			if tc.env != "" {
				t.Setenv("MARK_MATH_FORMAT", tc.env)
			}

			dir := t.TempDir()
			config := writeConfig(t, dir, "mark.toml", tc.config)
			document := filepath.Join(dir, "page.md")
			require.NoError(t, os.WriteFile(document,
				[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\n$$x^2$$\n"), 0o600))

			args := append([]string{"publish", "--config", config, "--color", "never",
				"--compile-only", "--features", "math", "--files", document}, tc.args...)
			out, _, err := runMark(t, args...)

			require.NoError(t, err)
			assert.Regexp(t, `ri:filename="math-[0-9a-f]+\.svg"`, out)
		})
	}
}
