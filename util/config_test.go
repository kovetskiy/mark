package util

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// runCommands runs mark's command line with publish and export swapped out,
// and says which of them ran.
func runCommands(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var ran string
	stub := func(name string) cli.ActionFunc {
		return func(context.Context, *cli.Command) error {
			ran = name
			return nil
		}
	}
	cmd := buildCommand("test", stub(PublishCommand), stub(ExportCommand))
	cmd.Writer, cmd.ErrWriter = &bytes.Buffer{}, &bytes.Buffer{}

	_, _, err := captureOutput(t, func() error {
		return cmd.Run(context.Background(), append([]string{"mark"}, args...))
	})

	return ran, err
}

// TestConfigFileRefusesAnUnknownKey: a misspelt key would otherwise be
// ignored, and the run would go ahead without the setting it was meant to be.
func TestConfigFileRefusesAnUnknownKey(t *testing.T) {
	noDefaultConfig(t)
	path := writeConfig(t, t.TempDir(), "mark.toml", "tittle-from-h1 = true\n")

	for _, args := range [][]string{
		{"publish", "--config", path, "-f", "doc.md"},
		{"--config", path, "publish", "-f", "doc.md"},
		{"--config", path, "export", "--page-id", "1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			ran, err := runCommands(t, args...)
			require.Error(t, err)
			assert.Equal(t, fmt.Sprintf(
				`unknown key in configuration file %q: "tittle-from-h1" (did you mean "title-from-h1"?)`, path),
				err.Error())
			assert.Empty(t, ran, "nothing runs")
		})
	}
}

// TestConfigFileListsEveryUnknownKey: fixing one key at a time, a run each,
// would be tedious.
func TestConfigFileListsEveryUnknownKey(t *testing.T) {
	noDefaultConfig(t)
	path := writeConfig(t, t.TempDir(), "mark.toml",
		"zzz = 1\nusername = \"u\"\nbase-ulr = \"x\"\n[table]\nspace = \"S\"\n")

	_, err := runCommands(t, "publish", "--config", path, "-f", "doc.md")
	require.Error(t, err)
	assert.Equal(t, fmt.Sprintf(
		`unknown keys in configuration file %q: "base-ulr" (did you mean "base-url"?), "table", "zzz"`, path),
		err.Error())
}

// TestConfigFileAcceptsAnotherCommandsKey: one file serves every command.
func TestConfigFileAcceptsAnotherCommandsKey(t *testing.T) {
	noDefaultConfig(t)
	path := writeConfig(t, t.TempDir(), "mark.toml",
		"username = \"u\"\nbase-url = \"http://example.invalid\"\n"+
			"title-from-h1 = true\nfiles = \"doc.md\"\n"+
			"format = \"mark\"\nmetadata = \"comments\"\nurl = \"http://example.invalid/x\"\n")

	ran, err := runCommands(t, "publish", "--config", path)
	require.NoError(t, err)
	assert.Equal(t, PublishCommand, ran)

	ran, err = runCommands(t, "export", "--config", path)
	require.NoError(t, err)
	assert.Equal(t, ExportCommand, ran)
}

// TestEveryFlagsConfigKeyIsKnown walks the whole command tree: whatever key a
// flag reads from the file, in whichever command, the file may hold.
func TestEveryFlagsConfigKeyIsKnown(t *testing.T) {
	var file *configFile
	declared := map[string]bool{}

	var walk func(*cli.Command)
	walk = func(cmd *cli.Command) {
		for _, flag := range cmd.Flags {
			for _, source := range flagSources(flag) {
				if held, ok := source.(*heldSource); ok {
					source = held.ValueSource
				}
				value, ok := source.(*configValue)
				if !ok {
					continue
				}
				if file == nil {
					file = value.file
				}
				require.Same(t, file, value.file, "every flag reads the one file")
				declared[value.key] = true
			}
		}
		for _, sub := range cmd.Commands {
			walk(sub)
		}
	}
	walk(newCommand("test"))

	require.NotNil(t, file)
	assert.Equal(t, slices.Sorted(maps.Keys(declared)), slices.Sorted(maps.Keys(file.keys)))
}

// TestClosestIsTheNearest: a farther candidate listed first does not win.
func TestClosestIsTheNearest(t *testing.T) {
	assert.Equal(t, "parents", closest("parent", []string{"parent-id", "parents"}))
	assert.Empty(t, closest("zzz", []string{"page", "space"}))
}

// flagSources are the value sources flag reads, after the command line.
func flagSources(flag cli.Flag) []cli.ValueSource {
	field := reflect.ValueOf(flag).Elem().FieldByName("Sources")
	if !field.IsValid() {
		return nil
	}
	return field.Interface().(cli.ValueSourceChain).Chain
}
