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
		{"the help command", []string{"mark", "help", "publish"}, []string{"mark", "help", "publish"}, false},
		{"the completion command", []string{"mark", "completion", "bash"}, []string{"mark", "completion", "bash"}, false},
		{"shell completion", []string{"mark", "--generate-shell-completion"}, []string{"mark", "--generate-shell-completion"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, bare := defaultToPublish(NewCommand("test"), test.args)
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
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0o700))
	writeConfig(t, filepath.Join(home, ".config"), "mark.toml",
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

// runMark runs mark's command line in this process and returns what it wrote
// to stdout and stderr.
func runMark(t *testing.T, args ...string) (string, string, error) {
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

	runErr := Run(context.Background(), NewCommand("test"), append([]string{"mark"}, args...))

	os.Stdout, os.Stderr = originalStdout, originalStderr
	require.NoError(t, stdout.Close())
	require.NoError(t, stderr.Close())

	out, err := os.ReadFile(stdout.Name())
	require.NoError(t, err)
	logged, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)

	return string(out), string(logged), runErr
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
	cmd := NewCommand("test")
	cmd.Writer, cmd.ErrWriter = &bytes.Buffer{}, &bytes.Buffer{}

	err := Run(context.Background(), cmd, []string{"mark", "--files", "a.md", "publish"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "files")
}

// helpOutput is what the command line prints for args.
func helpOutput(t *testing.T, version string, args ...string) string {
	t.Helper()

	var out bytes.Buffer
	cmd := NewCommand(version)
	cmd.Writer = &out

	require.NoError(t, Run(context.Background(), cmd, append([]string{"mark"}, args...)))

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

// TestREADMEUsageMatchesHelp keeps the help the README shows in step with what
// the binary prints, placeholders aside: the version, and the home directory
// in the default configuration file.
func TestREADMEUsageMatchesHelp(t *testing.T) {
	skipWithoutUnixConfigDir(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "${HOME}")

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

	assert.Equal(t, find("mark"), helpOutput(t, "v17.x.x", "--help"),
		"README.md's mark --help block has drifted from the binary")
	assert.Equal(t, find("mark publish"), helpOutput(t, "v17.x.x", "publish", "--help"),
		"README.md's mark publish --help block has drifted from the binary")
	assert.Equal(t, find("mark export"), helpOutput(t, "v17.x.x", "export", "--help"),
		"README.md's mark export --help block has drifted from the binary")
}
