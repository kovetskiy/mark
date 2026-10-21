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

// published is what a run handed to publish: whether it came through the
// bare form, and the files it was to publish.
type published struct {
	bare  bool
	files string
}

// dispatch runs mark's command line with publishing swapped out, and says
// what would have been published.
func dispatch(t *testing.T, args ...string) (*published, string, error) {
	t.Helper()

	var got *published
	cmd := buildCommand("test", func(_ context.Context, c *cli.Command) error {
		got = &published{bare: c == c.Root(), files: c.String("files")}
		return nil
	}, nil)
	var out bytes.Buffer
	cmd.Writer, cmd.ErrWriter = &out, &out

	_, logged, err := captureOutput(t, func() error {
		return cmd.Run(context.Background(), append([]string{"mark"}, args...))
	})

	return got, out.String() + logged, err
}

// TestTheBareForm covers a command line that names no command: it publishes
// as "mark publish" with the same arguments would, which is what mark did
// before it had commands.
func TestTheBareForm(t *testing.T) {
	noDefaultConfig(t)
	config := writeConfig(t, t.TempDir(), "mark.toml", "")

	for _, test := range []struct {
		name  string
		args  []string
		want  *published
		error string
	}{
		{"flags only", []string{"-f", "a.md", "--dry-run"}, &published{true, "a.md"}, ""},
		{"an inline value", []string{"--files=a.md"}, &published{true, "a.md"}, ""},
		{"global flags too", []string{"-c", config, "-u", "me", "-f", "a.md"}, &published{true, "a.md"}, ""},
		{"publish named", []string{"publish", "-f", "a.md"}, &published{false, "a.md"}, ""},
		{"global flags before publish", []string{"-u", "me", "--config", config, "publish", "-f", "a.md"}, &published{false, "a.md"}, ""},
		// A flag's value is not a command, however it is spelled.
		{"a value that reads as a command", []string{"--version-message", "publish", "-f", "a.md"}, &published{true, "a.md"}, ""},
		{"a global flag's value that reads as a command", []string{"-u", "publish", "-f", "a.md"}, &published{true, "a.md"}, ""},
		// Arguments that are not flags were ignored, and a --files pattern the
		// shell expanded still is.
		{"a stray argument", []string{"-f", "a.md", "b.md"}, &published{true, "a.md"}, ""},
		// publish has no such history to keep.
		{"a stray argument after publish", []string{"publish", "-f", "a.md", "b.md"}, nil, `unexpected argument "b.md"`},
		// A mistyped command is not a stray argument.
		{"a mistyped command first", []string{"publsh", "--compile-only", "-f", "x.md"}, nil, `unknown command "publsh": did you mean "publish"?`},
		{"a mistyped command last", []string{"-f", "x.md", "pubish"}, nil, `did you mean "publish"?`},
		{"anything at all without --files", []string{"--compile-only", "doc.md"}, nil, `unknown command "doc.md"`},
		// A publish flag before a command is not that command's.
		{"a publish flag before publish", []string{"-f", "a.md", "publish"}, nil, `--files is a flag of "publish": write it after "publish"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _, err := dispatch(t, test.args...)
			if test.error != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.error)
				assert.Nil(t, got, "nothing is published")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

// TestMarkOnItsOwn: "mark" alone publishes what the environment or the file
// names, as it did, and otherwise shows the help -- and still fails, so that a
// pipeline that lost its settings does not pass.
func TestMarkOnItsOwn(t *testing.T) {
	noDefaultConfig(t)

	got, out, err := dispatch(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command given")
	assert.Nil(t, got)
	assert.Contains(t, out, "COMMANDS:")
	assert.NotContains(t, out, "--files", "the help is mark's, not publish's")

	t.Setenv("MARK_FILES", "env.md")
	got, out, err = dispatch(t)
	require.NoError(t, err)
	assert.Equal(t, &published{true, "env.md"}, got)
	assert.Contains(t, out, "deprecated")
}

// TestTheBareFormReadsTheFileForPublishFlags: publish's flags on the root
// read the environment and the file as publish's own do.
func TestTheBareFormReadsTheFileForPublishFlags(t *testing.T) {
	noDefaultConfig(t)
	config := writeConfig(t, t.TempDir(), "mark.toml", "files = \"from-file.md\"\n")

	got, _, err := dispatch(t, "-c", config)
	require.NoError(t, err)
	assert.Equal(t, &published{true, "from-file.md"}, got)

	t.Setenv("MARK_FILES", "from-env.md")
	got, _, err = dispatch(t, "-c", config)
	require.NoError(t, err)
	assert.Equal(t, &published{true, "from-env.md"}, got)

	// Held until the root's action, so they are not mistaken for flags
	// written before a command.
	got, _, err = dispatch(t, "-c", config, "publish")
	require.NoError(t, err)
	assert.Equal(t, &published{false, "from-env.md"}, got)
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
		{"file named with no command", nil, []string{"-c", config}},
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

// TestBareMarkStillPublishes: "mark <flags>" is how mark runs in a great many
// CI pipelines, so it has to go on doing exactly what "mark publish <flags>"
// does, and log that it is deprecated.
func TestBareMarkStillPublishes(t *testing.T) {
	dir := t.TempDir()
	config := writeConfig(t, dir, "mark.toml", "")
	document := filepath.Join(dir, "page.md")
	require.NoError(t, os.WriteFile(document,
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

	args := []string{"--config", config, "--color", "never", "--compile-only", "--files", document}

	bareOut, bareLog, err := runMark(t, args...)
	require.NoError(t, err)

	publishOut, publishLog, err := runMark(t, append([]string{"publish"}, args...)...)
	require.NoError(t, err)

	assert.Contains(t, bareOut, "<p>Body.</p>", "the bare form publishes")
	assert.Equal(t, publishOut, bareOut, "and publishes exactly what mark publish does")

	assert.Contains(t, bareLog, `deprecated: run "mark publish"`)
	assert.NotContains(t, publishLog, "deprecated")
}

// TestPublishFlagsBelongAfterPublish: a publish flag written before the
// command it belongs to is refused rather than silently dropped.
func TestPublishFlagsBelongAfterPublish(t *testing.T) {
	cmd := newCommand("test")
	cmd.Writer, cmd.ErrWriter = &bytes.Buffer{}, &bytes.Buffer{}

	err := cmd.Run(context.Background(), []string{"mark", "--files", "a.md", "publish"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "files")
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

	t.Run("mark -f doc.md --help", func(t *testing.T) {
		want := helpOutput(t, "test", "--help")
		assert.Equal(t, want, helpOutput(t, "test", "-f", "doc.md", "--help"), "the bare form gets mark's help, which names publish")
		assert.Equal(t, want, helpOutput(t, "test", "-h", "--files=doc.md"))
	})

	t.Run("mark help publish", func(t *testing.T) {
		assert.Contains(t, helpOutput(t, "test", "help", "publish"), "--files")
	})

	t.Run("mark --version", func(t *testing.T) {
		assert.Equal(t, "mark version v1.2.3@abc\n", helpOutput(t, "v1.2.3@abc", "--version"))
	})

	t.Run("mark -f doc.md --version", func(t *testing.T) {
		assert.Equal(t, "mark version v1.2.3@abc\n", helpOutput(t, "v1.2.3@abc", "-f", "doc.md", "--version"))
	})
}

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

		want := "```text\n" + shown(helpOutput(t, "v16.x.x", block.args...)) + "```\n"
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
	assert.Contains(t, helpOutput(t, "v1.2.3@abc", "-f", "doc.md", "--version", "--help"), "COMMANDS:")
}

// TestTheDeprecationIsLogged: the notice is a warning, so --log-level
// governs it, and it comes only from a run that publishes.
func TestTheDeprecationIsLogged(t *testing.T) {
	config, document := publishable(t)

	_, logged, err := runMark(t, "--config", config, "--compile-only", "-f", document)
	require.NoError(t, err)
	assert.Contains(t, logged, `deprecated: run "mark publish"`)

	_, logged, err = runMark(t, "--config", config, "--log-level", "error", "--compile-only", "-f", document)
	require.NoError(t, err)
	assert.NotContains(t, logged, "deprecated")

	broken := writeConfig(t, t.TempDir(), "broken.toml", "x = [")
	_, logged, err = runMark(t, "--config", broken, "--compile-only", "-f", document)
	require.Error(t, err)
	assert.NotContains(t, logged, "deprecated", "the error is what needs fixing first")

	_, logged, err = runMark(t, "--nosuchflag")
	require.Error(t, err)
	assert.NotContains(t, logged, "deprecated")

	_, logged, err = runMark(t, "-f", document, "--help")
	require.NoError(t, err)
	assert.NotContains(t, logged, "deprecated", "asking for help is not a run to migrate")
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
	walk("mark", newCommand("v16.x.x"))
}
