package util

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
	altsrc "github.com/urfave/cli-altsrc/v3"
	"github.com/urfave/cli/v3"
)

// configFile is the configuration file a run takes its settings from: the one
// the "config" flag names, read and parsed once, the first time a setting is
// looked up in it, and reported -- by check -- if it cannot be used.
//
// Every flag reads it through a source from value or global, so every flag
// takes its value from it by the same rules: urfave/cli's own, applied as it
// applies any source in a flag's chain.
type configFile struct {
	// path is the "config" flag's destination.
	path *string

	// deferGlobals holds the sources from global back until settle. A global
	// flag is resolved as the root command is parsed, and "mark publish
	// --config X" names the file only once publish is parsed, after that: a
	// global flag that read the file straight away would read the default
	// one instead of X.
	deferGlobals bool
	settled      bool

	// once keeps what was read for the rest of the run. A command line is
	// built afresh for each run; the deprecated Flags are not, and read the
	// file again at every lookup, as they always did.
	once bool

	loaded     bool
	loadedPath string
	values     map[any]any
	err        error
}

// newConfigFile is the file the "config" flag will name, through path, for one
// run of a command line.
func newConfigFile(path *string, deferGlobals bool) *configFile {
	return &configFile{path: path, deferGlobals: deferGlobals, once: true}
}

// load reads and parses the file the first time it is asked for, and again
// only should the path have changed since.
func (c *configFile) load() error {
	path := *c.path
	if c.once && c.loaded && c.loadedPath == path {
		return c.err
	}

	c.loaded, c.loadedPath = true, path
	c.values, c.err = readConfigFile(path)

	return c.err
}

// check reports a configuration file that cannot be used.
//
// A setting is looked up as a flag is resolved, and a file that cannot be read
// or parsed answers every lookup with nothing. A stray syntax error therefore
// removes every setting at once and says nothing, and the first sign of
// trouble is whichever required value went missing with it -- so "confluence
// password should be specified" is what an unquoted list three lines further
// down looks like, which is a long way from where the problem is.
func (c *configFile) check() error {
	return c.load()
}

// settle releases the global flags' sources, now that the whole command line
// has been parsed and the file is the one it names, and resolves each global
// flag of cmd that neither the command line nor the environment has set.
func (c *configFile) settle(cmd *cli.Command) error {
	c.settled = true

	for _, flag := range cmd.Root().Flags {
		if flag.IsSet() {
			continue
		}
		if err := flag.PostParse(); err != nil {
			return err
		}
	}

	return nil
}

// value is a source for the setting under key, for a flag resolved once the
// file is known: any flag of a command, as opposed to the root.
func (c *configFile) value(key string) cli.ValueSource {
	return &configValue{file: c, key: key}
}

// global is value for a global flag, which settle releases.
func (c *configFile) global(key string) cli.ValueSource {
	return &configValue{file: c, key: key, deferred: c.deferGlobals}
}

// configValue is one setting of a configFile, as a flag's source.
type configValue struct {
	file     *configFile
	key      string
	deferred bool
}

// Lookup is the setting, in the form a flag parses: a list joined with
// commas, anything else as it prints, as cli-altsrc reads one. A file that
// cannot be used has no settings; check says why.
func (v *configValue) Lookup() (string, bool) {
	if v.deferred && !v.file.settled {
		return "", false
	}
	if v.file.load() != nil {
		return "", false
	}

	value, found := altsrc.NestedVal(v.key, v.file.values)
	if !found {
		return "", false
	}

	if list := reflect.ValueOf(value); list.Kind() == reflect.Slice {
		items := make([]string, list.Len())
		for i := range items {
			items[i] = fmt.Sprint(list.Index(i).Interface())
		}
		return strings.Join(items, ","), true
	}

	return fmt.Sprint(value), true
}

func (v *configValue) String() string {
	return fmt.Sprintf("toml file %q at key %q", *v.file.path, v.key)
}

func (v *configValue) GoString() string {
	return fmt.Sprintf("configValue{file:%q,key:%q}", *v.file.path, v.key)
}

// readConfigFile reads the file at path, and is nothing at all when path is
// empty or names the default location and nothing is there.
func readConfigFile(path string) (map[any]any, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Nothing at the default location is the ordinary case: most
			// people pass flags. A path somebody named themselves is
			// different, because there silence makes a typo indistinguishable
			// from a setting they forgot.
			//
			// Compared against the default rather than asked of IsSet, which
			// answers true for a flag carrying a default value and would
			// therefore fail every run that has no configuration file at all.
			if path != ConfigFilePath() {
				return nil, fmt.Errorf("configuration file %q does not exist", path)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("unable to read configuration file %q: %w", path, err)
	}

	var parsed map[string]any
	if err := toml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("unable to parse configuration file %q: %w", path, err)
	}

	values := make(map[any]any, len(parsed))
	for key, value := range parsed {
		values[key] = value
	}

	return values, nil
}
