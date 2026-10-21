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

// configFile is the configuration file the "config" flag names, as every
// flag's source reads it: parsed at the first lookup, and again only if the
// path has changed since.
type configFile struct {
	// path is the "config" flag's destination.
	path *string

	// settled opens the sources hold wraps. Until then they answer nothing.
	settled bool

	loaded     bool
	loadedPath string
	values     map[any]any
	err        error
}

// newConfigFile is the file the "config" flag will name through path, with
// its held sources shut until settle.
func newConfigFile(path *string) *configFile {
	return &configFile{path: path}
}

func (c *configFile) load() error {
	path := *c.path
	if c.loaded && c.loadedPath == path {
		return c.err
	}

	c.loaded, c.loadedPath = true, path
	c.values, c.err = readConfigFile(path)

	return c.err
}

// check reports a file that cannot be used. A lookup in one answers nothing,
// so without this a syntax error surfaces only as whichever required setting
// went missing with it.
func (c *configFile) check() error {
	return c.load()
}

// settle opens the held sources and resolves every flag of root that the
// command line did not set.
func (c *configFile) settle(root *cli.Command) error {
	c.settled = true

	for _, flag := range root.Flags {
		if flag.IsSet() {
			continue
		}
		if err := flag.PostParse(); err != nil {
			return err
		}
	}

	return nil
}

// value is a source for the setting under key.
func (c *configFile) value(key string) cli.ValueSource {
	return &configValue{file: c, key: key}
}

// hold makes source answer nothing until settle. urfave/cli resolves the
// root's flags as it parses the root, before "mark publish --config X" has
// named X, and before the root's action can tell which of its flags the
// command line set.
func (c *configFile) hold(source cli.ValueSource) cli.ValueSource {
	return &heldSource{ValueSource: source, file: c}
}

type heldSource struct {
	cli.ValueSource
	file *configFile
}

func (h *heldSource) Lookup() (string, bool) {
	if !h.file.settled {
		return "", false
	}
	return h.ValueSource.Lookup()
}

// configValue is one setting of a configFile, as a flag's source.
type configValue struct {
	file *configFile
	key  string
}

// Lookup is the setting, in the form a flag parses: a list joined with
// commas, anything else as it prints, as cli-altsrc reads one. A file that
// cannot be used has no settings; check says why.
func (v *configValue) Lookup() (string, bool) {
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
