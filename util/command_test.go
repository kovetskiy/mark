package util

import (
	"bytes"
	"context"
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

// TestDefaultToPublish covers how a command line that names no command is
// read: as "mark publish" with the same arguments, which is what it did before
// there were commands.
func TestDefaultToPublish(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want []string
		bare bool
	}{
		{"nothing at all", []string{"mark"}, []string{"mark", "publish"}, true},
		{"flags only", []string{"mark", "-f", "a.md", "--dry-run"}, []string{"mark", "publish", "-f", "a.md", "--dry-run"}, true},
		{"global flags only", []string{"mark", "-c", "x.toml"}, []string{"mark", "publish", "-c", "x.toml"}, true},
		{"an inline value", []string{"mark", "--files=a.md"}, []string{"mark", "publish", "--files=a.md"}, true},
		{"publish named", []string{"mark", "publish", "-f", "a.md"}, []string{"mark", "publish", "-f", "a.md"}, false},
		{"global flags before publish", []string{"mark", "-u", "me", "--config", "x.toml", "publish"}, []string{"mark", "-u", "me", "--config", "x.toml", "publish"}, false},
		// A flag's value is not a command, however it is spelled.
		{"a value that reads as a command", []string{"mark", "--version-message", "publish", "-f", "a.md"}, []string{"mark", "publish", "--version-message", "publish", "-f", "a.md"}, true},
		{"a global flag's value that reads as a command", []string{"mark", "-u", "publish"}, []string{"mark", "publish", "-u", "publish"}, true},
		// An argument that is no command was ignored before, and still is.
		{"a stray argument", []string{"mark", "-f", "a.md", "stray"}, []string{"mark", "publish", "-f", "a.md", "stray"}, true},
		{"after a double dash", []string{"mark", "--", "publish"}, []string{"mark", "publish", "--", "publish"}, true},
		{"help", []string{"mark", "--help"}, []string{"mark", "--help"}, false},
		{"short help", []string{"mark", "-h"}, []string{"mark", "-h"}, false},
		{"version", []string{"mark", "--version"}, []string{"mark", "--version"}, false},
		{"short version", []string{"mark", "-v"}, []string{"mark", "-v"}, false},
		// The version is mark's, whatever comes with it.
		{"version after a publish flag", []string{"mark", "-f", "doc.md", "--version"}, []string{"mark", "--version"}, false},
		{"short version before a publish flag", []string{"mark", "-v", "--files=doc.md"}, []string{"mark", "-v"}, false},
		{"version with global flags", []string{"mark", "-u", "me", "--version"}, []string{"mark", "--version"}, false},
		{"the help command", []string{"mark", "help", "publish"}, []string{"mark", "help", "publish"}, false},
		{"the completion command", []string{"mark", "completion", "bash"}, []string{"mark", "completion", "bash"}, false},
		{"shell completion", []string{"mark", "--generate-shell-completion"}, []string{"mark", "--generate-shell-completion"}, false},
		{"help with global flags", []string{"mark", "-u", "me", "--help"}, []string{"mark", "-u", "me", "--help"}, false},
		// Help next to a publish flag is publish's help, wherever it is written.
		{"help after a publish flag", []string{"mark", "-f", "doc.md", "--help"}, []string{"mark", "publish", "-f", "doc.md", "--help"}, true},
		{"help before a publish flag", []string{"mark", "-h", "--files=doc.md"}, []string{"mark", "publish", "-h", "--files=doc.md"}, true},
		{"shell completion after a publish flag", []string{"mark", "-f", "doc.md", "--generate-shell-completion"}, []string{"mark", "publish", "-f", "doc.md", "--generate-shell-completion"}, true},
		// urfave/cli's own commands, after a flag only publish has, are stray
		// arguments as they were before there were commands.
		{"a stray help", []string{"mark", "--compile-only", "-f", "page.md", "help"}, []string{"mark", "publish", "--compile-only", "-f", "page.md"}, true},
		{"a stray h", []string{"mark", "-f", "page.md", "h", "more"}, []string{"mark", "publish", "-f", "page.md", "more"}, true},
		{"a stray completion", []string{"mark", "-f", "page.md", "completion"}, []string{"mark", "publish", "-f", "page.md"}, true},
		// Help and completion came before the version, and still do.
		{"help and version", []string{"mark", "--help", "--version"}, []string{"mark", "--help", "--version"}, false},
		{"version and shell completion", []string{"mark", "-v", "--generate-shell-completion"}, []string{"mark", "-v", "--generate-shell-completion"}, false},
		{"version and publish's help", []string{"mark", "-f", "doc.md", "--version", "--help"}, []string{"mark", "publish", "-f", "doc.md", "--version", "--help"}, true},
		// The version is mark's after a command too.
		{"version after publish", []string{"mark", "publish", "-c", "x.toml", "--version"}, []string{"mark", "--version"}, false},
		{"a value that reads as the version", []string{"mark", "publish", "--version-message", "--version"}, []string{"mark", "publish", "--version-message", "--version"}, false},
		// A version flag that is false asks for nothing.
		{"version false", []string{"mark", "-f", "doc.md", "--version=false"}, []string{"mark", "publish", "-f", "doc.md"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, bare := defaultToPublish(newCommand("test"), test.args)
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.bare, bare)
		})
	}
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
// does -- and say that it is deprecated, which "mark publish" does not.
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

	err := run(context.Background(), cmd, []string{"mark", "--files", "a.md", "publish"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "files")
}

// TestBrokenConfigFileLeavesHelpAlone: urfave/cli runs the root's Before for
// its own help and completion commands, and for a shell asking for
// completions, so a configuration file that does not parse used to break all
// of them. They read nothing from it; publish, which does, still says so.
func TestBrokenConfigFileLeavesHelpAlone(t *testing.T) {
	t.Setenv("MARK_CONFIG", writeConfig(t, t.TempDir(), "mark.toml", "x = ["))

	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		cmd := newCommand("test")
		cmd.Writer, cmd.ErrWriter = &out, &bytes.Buffer{}
		err := run(context.Background(), cmd, append([]string{"mark"}, args...))
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

	require.NoError(t, run(context.Background(), cmd, append([]string{"mark"}, args...)))

	return out.String()
}

// TestHelp: "mark --help" is about mark as a whole -- its commands and the
// flags they share -- and "mark publish --help" about publishing.
func TestHelp(t *testing.T) {
	t.Run("mark --help", func(t *testing.T) {
		help := helpOutput(t, "test", "--help")
		assert.Contains(t, help, "COMMANDS:")
		assert.Regexp(t, `(?m)^\s+publish\s`, help)
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

	t.Run("mark -f doc.md --help", func(t *testing.T) {
		help := helpOutput(t, "test", "-f", "doc.md", "--help")
		assert.Contains(t, help, "mark publish", "a publish flag makes it publish's help")
		assert.Contains(t, help, "--files")
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

// TestREADMEUsageMatchesHelp keeps the help the README shows in step with what
// the binary prints, placeholders aside: the version, and the home directory
// in the default configuration file.
func TestREADMEUsageMatchesHelp(t *testing.T) {
	skipWithoutUnixConfigDir(t)
	// The README shows the default file where Linux keeps it. Wherever this
	// platform keeps it instead, it is that file all the same.
	defaultFile := filepath.Join(userConfigDir(t, "${HOME}"), "mark.toml")
	shown := func(help string) string {
		return strings.ReplaceAll(help, defaultFile, "${HOME}/.config/mark.toml")
	}

	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	require.NoError(t, err)

	blocks := regexp.MustCompile("(?s)```text\n(NAME:\n.*?)```\n").FindAllStringSubmatch(string(readme), -1)

	find := func(heading string) string {
		for _, block := range blocks {
			if strings.HasPrefix(block[1], "NAME:\n   "+heading+" - ") {
				return block[1]
			}
		}
		t.Fatalf("README.md has no help block for %q", heading)
		return ""
	}

	assert.Equal(t, find("mark"), shown(helpOutput(t, "v16.x.x", "--help")),
		"README.md's mark --help block has drifted from the binary")
	assert.Equal(t, find("mark publish"), shown(helpOutput(t, "v16.x.x", "publish", "--help")),
		"README.md's mark publish --help block has drifted from the binary")
}

// TestAStrayHelpStillPublishes: before there were commands, an argument that
// named nothing was ignored, "help" among them. urfave/cli's own commands are
// real commands now, and the bare form must not have them swallow the run.
func TestAStrayHelpStillPublishes(t *testing.T) {
	config, document := publishable(t)

	for _, stray := range []string{"help", "h", "completion"} {
		t.Run(stray, func(t *testing.T) {
			out, _, err := runMark(t, "--config", config, "--compile-only", "-f", document, stray)
			require.NoError(t, err)
			assert.Contains(t, out, "<p>Body.</p>")
		})
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

// TestHelpComesBeforeTheVersion: before there were commands, "mark --help
// --version" printed help, and the version flag was a thing a publish command
// line could carry. Both still hold, whichever form the command line takes.
func TestHelpComesBeforeTheVersion(t *testing.T) {
	assert.Contains(t, helpOutput(t, "v1.2.3@abc", "--help", "--version"), "COMMANDS:")
	assert.Contains(t, helpOutput(t, "v1.2.3@abc", "--version", "--help"), "COMMANDS:")
	assert.Equal(t, "mark version v1.2.3@abc\n", helpOutput(t, "v1.2.3@abc", "publish", "--version"))
	assert.Equal(t, "mark version v1.2.3@abc\n", helpOutput(t, "v1.2.3@abc", "publish", "-c", "x.toml", "-v"))
}

// TestTheBareFormIsDeprecatedAtEveryLogLevel: the notice is what tells a
// pipeline it has to move to "mark publish", so it must not depend on the log
// level the pipeline chose, nor on the run getting far enough to log.
func TestTheBareFormIsDeprecatedAtEveryLogLevel(t *testing.T) {
	config, document := publishable(t)

	_, logged, err := runMark(t, "--config", config, "--log-level", "error", "--compile-only", "-f", document)
	require.NoError(t, err)
	assert.Contains(t, logged, `deprecated: run "mark publish"`)

	broken := writeConfig(t, t.TempDir(), "broken.toml", "x = [")
	_, logged, err = runMark(t, "--config", broken, "--compile-only", "-f", document)
	require.Error(t, err)
	assert.Contains(t, logged, `deprecated: run "mark publish"`)

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

// TestFlagsStillMakeACommand: mark's command line before there were commands,
// as a program built on this package put it together, keeps working.
func TestFlagsStillMakeACommand(t *testing.T) {
	_, document := publishable(t)
	config := writeConfig(t, t.TempDir(), "mark.toml",
		"username = \"cfguser\"\nfiles = \""+filepath.ToSlash(document)+"\"\n")

	legacy := func(action cli.ActionFunc) *cli.Command {
		return &cli.Command{Name: "mark", Flags: Flags, Before: CheckFlags, Action: action}
	}

	t.Run("the file is read, global flags and all", func(t *testing.T) {
		var username, files string
		err := legacy(func(_ context.Context, cmd *cli.Command) error {
			username, files = cmd.String("username"), cmd.String("files")
			return nil
		}).Run(context.Background(), []string{"mark", "--config", config})
		require.NoError(t, err)
		assert.Equal(t, "cfguser", username)
		assert.Equal(t, filepath.ToSlash(document), files)
	})

	t.Run("a file that does not parse is reported", func(t *testing.T) {
		broken := writeConfig(t, t.TempDir(), "broken.toml", "x = [")
		err := legacy(RunMark).Run(context.Background(), []string{"mark", "--config", broken})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to parse configuration file")
	})

	// Flags are one set of flags, and urfave/cli remembers that a flag was set
	// from its source from one run to the next, so only the first run takes
	// --files from the file. One run a process is what they were for.
	t.Run("it publishes", func(t *testing.T) {
		out, _, err := captureOutput(t, func() error {
			return legacy(RunMark).Run(context.Background(),
				[]string{"mark", "--config", config, "--color", "never", "--compile-only", "-f", document})
		})
		require.NoError(t, err)
		assert.Contains(t, out, "<p>Body.</p>")
	})
}

// TestTheConfigFileIsReadOnce: every flag looks its setting up in the file,
// and a run reads and parses it the first time, not once a flag. The
// deprecated Flags read it at every lookup, as they always did.
func TestTheConfigFileIsReadOnce(t *testing.T) {
	path := writeConfig(t, t.TempDir(), "mark.toml", "username = \"first\"\n")

	once := newConfigFile(&path, false)
	every := &configFile{path: &path}

	for _, file := range []*configFile{once, every} {
		value, found := file.value("username").Lookup()
		require.True(t, found)
		assert.Equal(t, "first", value)
	}

	writeConfig(t, filepath.Dir(path), "mark.toml", "username = \"second\"\n")

	value, _ := once.value("username").Lookup()
	assert.Equal(t, "first", value, "a run reads the file once")
	value, _ = every.value("username").Lookup()
	assert.Equal(t, "second", value)
}
