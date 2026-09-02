package util

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// TestCheckConfigFile covers the reason issue #613 was so hard to diagnose.
//
// Settings are read from the file lazily, one flag at a time, so a file that
// cannot be parsed yields nothing at all and says nothing about it. Every
// setting disappears at once, and the first sign of trouble is whichever
// required value went with them -- which is how an unquoted list produces
// "confluence password should be specified".
func TestCheckConfigFile(t *testing.T) {
	write := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "mark.toml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}

	run := func(t *testing.T, args ...string) error {
		t.Helper()
		cmd := &cli.Command{
			Flags:  Flags,
			Action: func(context.Context, *cli.Command) error { return nil },
		}
		return CheckConfigFile(mustParse(t, cmd, args...))
	}

	t.Run("a file that cannot be parsed is reported", func(t *testing.T) {
		path := write(t, "username = \"u\"\nfeatures = [frontmatter,mention]\n")
		err := run(t, "mark", "--config", path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), path, "the message must name the file")
		assert.Contains(t, err.Error(), "line 2", "and where in it to look")
	})

	t.Run("a valid file passes", func(t *testing.T) {
		path := write(t, "username = \"u\"\npassword = \"p\"\nbase-url = \"http://example.invalid\"\n")
		assert.NoError(t, run(t, "mark", "--config", path))
	})

	t.Run("a named file that is absent is reported", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "nope.toml")
		err := run(t, "mark", "--config", missing)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("nothing at the default location is fine", func(t *testing.T) {
		// The overwhelmingly common case: no config file, everything passed as
		// flags. Complaining here would break every such invocation.
		assert.NoError(t, run(t, "mark"))
	})
}

// mustParse runs a command far enough to populate its flags.
func mustParse(t *testing.T, cmd *cli.Command, args ...string) *cli.Command {
	t.Helper()
	var parsed *cli.Command
	cmd.Action = func(_ context.Context, c *cli.Command) error {
		parsed = c
		return nil
	}
	require.NoError(t, cmd.Run(context.Background(), args))
	require.NotNil(t, parsed)
	return parsed
}

// The flag set is a package-level value and the TOML source behind it keeps the
// file it first read, so a second cli.Command run in one process sees none of
// it. Each case below therefore resolves its flags in a subprocess -- which is
// also the only way mark itself ever resolves them.

// TestConfigSubprocess is the worker for the cases below and does nothing
// unless a parent names it.
func TestConfigSubprocess(t *testing.T) {
	wanted := os.Getenv("MARK_TEST_FLAGS")
	if wanted == "" {
		t.Skip("not a subprocess case")
	}

	args := []string{"mark"}
	if extra := os.Getenv("MARK_TEST_ARGS"); extra != "" {
		var extraArgs []string
		require.NoError(t, json.Unmarshal([]byte(extra), &extraArgs))
		args = append(args, extraArgs...)
	}

	parsed := mustParse(t, &cli.Command{Flags: Flags}, args...)

	resolved := map[string]string{}
	for _, name := range strings.Split(wanted, ",") {
		resolved[name] = parsed.String(name)
	}

	encoded, err := json.Marshal(resolved)
	require.NoError(t, err)

	fmt.Printf("\nRESOLVED%sRESOLVED\n", encoded)
}

// resolveFlags runs TestConfigSubprocess in a fresh process and returns what
// the named flags came out as.
func resolveFlags(t *testing.T, environment map[string]string, names []string, args ...string) map[string]string {
	t.Helper()

	encodedArgs, err := json.Marshal(args)
	require.NoError(t, err)

	worker := exec.Command(os.Args[0], "-test.run=^TestConfigSubprocess$", "-test.v")
	worker.Env = append(os.Environ(),
		"MARK_TEST_FLAGS="+strings.Join(names, ","),
		"MARK_TEST_ARGS="+string(encodedArgs),
	)
	for name, value := range environment {
		worker.Env = append(worker.Env, name+"="+value)
	}

	output, err := worker.CombinedOutput()
	require.NoError(t, err, string(output))

	_, after, found := strings.Cut(string(output), "RESOLVED")
	require.True(t, found, "worker printed no result: %s", output)
	encoded, _, found := strings.Cut(after, "RESOLVED")
	require.True(t, found, "worker printed no result: %s", output)

	var resolved map[string]string
	require.NoError(t, json.Unmarshal([]byte(encoded), &resolved))

	return resolved
}

func writeConfig(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// TestConfigFromTheEnvironmentAppliesToEveryFlag: every other flag finds the
// configuration file through a pointer to the "config" flag's destination,
// which is filled in as the flags are resolved -- in the order they are
// declared. "config" sat at index 17, so every flag declared before it looked
// for its TOML value while that pointer was still empty.
//
// A path on the command line happened to survive that. One in MARK_CONFIG did
// not, so "MARK_CONFIG=/etc/mark.toml mark" silently ignored files, username,
// password, target-url, base-url and log-level while honouring space, parents
// and features -- and the only sign of it was the misleading "confluence
// password should be specified using -p flag".
func TestConfigFromTheEnvironmentAppliesToEveryFlag(t *testing.T) {
	path := writeConfig(t, t.TempDir(), "mark.toml",
		"files = \"from-config.md\"\nusername = \"cfguser\"\n"+
			"password = \"cfgpass\"\nspace = \"CFG\"\n")

	resolved := resolveFlags(t,
		map[string]string{"MARK_CONFIG": path},
		[]string{"files", "username", "password", "space"},
	)

	// Declared after "config", and honoured even before the fix.
	assert.Equal(t, "CFG", resolved["space"])

	// Declared before it, and silently dropped.
	assert.Equal(t, "from-config.md", resolved["files"])
	assert.Equal(t, "cfguser", resolved["username"])
	assert.Equal(t, "cfgpass", resolved["password"])
}

// TestConfigOnTheCommandLineStillWins: the environment must not overtake a path
// the caller named explicitly.
func TestConfigOnTheCommandLineStillWins(t *testing.T) {
	dir := t.TempDir()
	fromEnv := writeConfig(t, dir, "env.toml", "username = \"env\"\n")
	fromFlag := writeConfig(t, dir, "flag.toml", "username = \"flag\"\n")

	resolved := resolveFlags(t,
		map[string]string{"MARK_CONFIG": fromEnv},
		[]string{"username", "config"},
		"--config", fromFlag,
	)

	assert.Equal(t, "flag", resolved["username"])
	assert.Equal(t, fromFlag, resolved["config"])
}

// TestFeaturesRejectsAnUnknownName: every feature is read with a plain
// slices.Contains where it takes effect, so a misspelling was indistinguishable
// from a feature deliberately left off -- "--features=mermaidd" published the
// page with mermaid quietly not running, and said nothing.
func TestFeaturesRejectsAnUnknownName(t *testing.T) {
	cmd := &cli.Command{Flags: Flags}
	_, err := CheckFlags(mustParseArgs(t, cmd, "mark", "--features", "mermaidd"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mermaidd", "the message names the value")
	assert.Contains(t, err.Error(), "mermaid", "and what was expected instead")
}

// TestFeaturesRejectsAnUntrimmedName: --features "math, mermaid" hands the
// second one over with its leading space still attached, and it goes
// unrecognised where it is read. Saying so beats switching it off in silence.
func TestFeaturesRejectsAnUntrimmedName(t *testing.T) {
	cmd := &cli.Command{Flags: Flags}
	_, err := CheckFlags(mustParseArgs(t, cmd, "mark", "--features", "math, mermaid"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "features")
}

// TestFeaturesAcceptsEveryKnownName guards the list against drifting from the
// names the code actually looks for.
func TestFeaturesAcceptsEveryKnownName(t *testing.T) {
	for _, feature := range KnownFeatures {
		t.Run(feature, func(t *testing.T) {
			cmd := &cli.Command{Flags: Flags}
			_, err := CheckFlags(mustParseArgs(t, cmd, "mark", "--features", feature))
			assert.NoError(t, err)
		})
	}
}

// mustParseArgs is mustParse with the context CheckFlags wants alongside.
func mustParseArgs(t *testing.T, cmd *cli.Command, args ...string) (context.Context, *cli.Command) {
	t.Helper()

	return context.Background(), mustParse(t, cmd, args...)
}
