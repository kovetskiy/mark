package page

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
	"go.yaml.in/yaml/v3"
)

// LoadGlobalProperties reads the properties to set on every page.
//
// YAML, which also reads the JSON a caller may prefer to write, since JSON is
// valid YAML. An empty path means there are none.
func LoadGlobalProperties(path string) (map[string]any, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read properties file %q: %w", path, err)
	}

	var properties map[string]any
	if err := yaml.Unmarshal(data, &properties); err != nil {
		return nil, fmt.Errorf("unable to parse properties file %q: %w", path, err)
	}

	return properties, nil
}

// MergeProperties combines the properties every page gets with the ones a
// document asked for, the document winning.
//
// A document naming a property the file also names is being specific about its
// own page, which is the more particular statement of the two.
func MergeProperties(global, document map[string]any) map[string]any {
	if len(global) == 0 && len(document) == 0 {
		return nil
	}

	merged := make(map[string]any, len(global)+len(document))
	for key, value := range global {
		merged[key] = value
	}
	for key, value := range document {
		merged[key] = value
	}

	return merged
}

// ApplyProperties writes content properties onto a page.
//
// Only what changed is written. A property holds a version that Confluence
// increments on every write, so rewriting an unchanged value would fill its
// history as surely as republishing an unchanged page fills the page's.
func ApplyProperties(api *confluence.API, pageID string, properties map[string]any, dryRun bool) error {
	if len(properties) == 0 {
		return nil
	}

	existing, err := api.ListContentProperties(pageID)
	if err != nil {
		return fmt.Errorf("unable to read properties of page %s: %w", pageID, err)
	}

	current := make(map[string]*confluence.Property, len(existing))
	for i := range existing {
		current[existing[i].Key] = &existing[i]
	}

	// Sorted so that a run writing several properties does so in the same order
	// every time, which makes a page history readable.
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value, err := json.Marshal(properties[key])
		if err != nil {
			return fmt.Errorf("unable to encode property %q: %w", key, err)
		}

		if held, ok := current[key]; ok && json.Valid(held.Value) &&
			equalJSON(held.Value, value) {
			continue
		}

		if dryRun {
			log.Info().Msgf("property %q of page %s would be set to %s", key, pageID, value)
			continue
		}

		log.Info().Msgf("setting property %q of page %s", key, pageID)
		if err := api.SetContentProperty(pageID, key, value, current[key]); err != nil {
			return err
		}
	}

	return nil
}

// equalJSON compares two encodings by what they mean rather than how they were
// written, so that whitespace or key order coming back from Confluence does not
// read as a change.
func equalJSON(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}

	leftBytes, err := json.Marshal(left)
	if err != nil {
		return false
	}

	rightBytes, err := json.Marshal(right)
	if err != nil {
		return false
	}

	return string(leftBytes) == string(rightBytes)
}
