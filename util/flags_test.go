package util

import (
	"context"
	"os"
	"path/filepath"
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
