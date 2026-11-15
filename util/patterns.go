package util

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

// patternSeparator splits a value a source gave for --files into patterns.
// A comma cannot, since a glob may hold one: {a,b}/*.md.
const patternSeparator = "\n"

// patternList is a repeatable flag value that, unlike cli.StringSlice, never
// splits on a comma.
type patternList struct {
	values *[]string
	set    bool
}

// patternListType is the cli.ValueCreator of patternList.
type patternListType struct{}

func (patternListType) Create(defaults []string, p *[]string, _ cli.NoConfig) cli.Value {
	*p = defaults
	return &patternList{values: p}
}

func (patternListType) ToString(values []string) string {
	return strings.Join(values, ", ")
}

func (l *patternList) Set(value string) error {
	if !l.set {
		*l.values = nil
		l.set = true
	}
	*l.values = append(*l.values, strings.Split(value, patternSeparator)...)
	return nil
}

func (l *patternList) String() string {
	if l.values == nil {
		return ""
	}
	return strings.Join(*l.values, ", ")
}

func (l *patternList) Get() any { return *l.values }

// joinPatterns is the one glob matching what any of patterns does, as
// mark.Config.Files takes it: the page manifest records a run's files under a
// single pattern.
//
// Several patterns are refused when one names a drive: a glob with a volume
// inside braces matches nothing, silently.
func joinPatterns(patterns []string) (string, error) {
	var distinct []string
	for _, pattern := range patterns {
		if pattern != "" && !slices.Contains(distinct, pattern) {
			distinct = append(distinct, pattern)
		}
	}

	switch len(distinct) {
	case 0:
		return "", nil
	case 1:
		return distinct[0], nil
	}

	for _, pattern := range distinct {
		if filepath.VolumeName(pattern) != "" {
			return "", fmt.Errorf("--files %q names a drive, which cannot be combined with other --files patterns: use paths relative to the current directory", pattern)
		}
	}

	for i, pattern := range distinct {
		// A leading ./ matches nothing inside braces.
		distinct[i] = escapeAlternative(strings.TrimPrefix(pattern, "./"))
	}

	for _, pattern := range distinct {
		if matched, err := doublestar.FilepathGlob(pattern); err == nil && len(matched) == 0 {
			log.Warn().Msgf("--files %q matched no files", pattern)
		}
	}

	return "{" + strings.Join(distinct, ",") + "}", nil
}

// escapeAlternative escapes the commas of pattern that are not inside braces
// or a class of its own, so that it stays one alternative of an enclosing {a,b}. A class
// rather than a backslash, which is a path separator to a Windows glob.
func escapeAlternative(pattern string) string {
	var escaped strings.Builder
	depth := 0
	inClass := false
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; {
		case c == '\\' && i+1 < len(pattern):
			escaped.WriteByte(c)
			i++
			escaped.WriteByte(pattern[i])
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case inClass:
		case c == '{':
			depth++
		case c == '}' && depth > 0:
			depth--
		case c == ',' && depth == 0:
			escaped.WriteString("[,]")
			continue
		}
		escaped.WriteByte(pattern[i])
	}

	return escaped.String()
}
