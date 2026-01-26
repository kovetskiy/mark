package includes

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"go.yaml.in/yaml/v3"

	"github.com/rs/zerolog/log"
)

// <!-- Include: <template path>
//
//	(Delims: (none | "<left>","<right>"))?
//	<optional yaml data> -->
var reIncludeDirective = regexp.MustCompile(
	`(?s)` +
		`<!--\s*Include:\s*(?P<template>.+?)\s*` +
		`(?:\n\s*Delims:\s*(?:(none|"(?P<left>.*?)"\s*,\s*"(?P<right>.*?)")))?\s*` +
		`(?:\n(?P<config>.*?))?-->`,
)

// fenceRange covers the byte range of a fenced code block, from the start of
// its opening fence line through the trailing newline of its closing fence
// line (or to EOF for an unclosed fence).
type fenceRange struct{ start, end int }

// scanFenceRanges walks contents line-by-line and returns the byte ranges of
// fenced code blocks. It recognizes ``` and ~~~ fences with up to 3 leading
// spaces of indentation per CommonMark §4.5. A closing fence must use the
// same character as its opener and be at least as long.
//
// Indented code blocks (CommonMark §4.4, 4+ leading spaces) are NOT detected:
// Include directives inside them will still be expanded. Fenced blocks cover
// the realistic self-documentation case from issue #717.
func scanFenceRanges(contents []byte) []fenceRange {
	var (
		ranges    []fenceRange
		inFence   bool
		fenceChar byte
		fenceLen  int
		openStart int
	)

	pos := 0
	for pos < len(contents) {
		lineStart := pos
		var lineEnd, nextStart int
		if nl := bytes.IndexByte(contents[pos:], '\n'); nl < 0 {
			lineEnd, nextStart = len(contents), len(contents)
		} else {
			lineEnd = pos + nl
			nextStart = lineEnd + 1
		}
		line := contents[lineStart:lineEnd]
		pos = nextStart

		indent := 0
		for indent < len(line) && indent < 4 && line[indent] == ' ' {
			indent++
		}
		if indent >= 4 || indent >= len(line) {
			continue
		}
		ch := line[indent]
		if ch != '`' && ch != '~' {
			continue
		}
		run := indent
		for run < len(line) && line[run] == ch {
			run++
		}
		runLen := run - indent
		if runLen < 3 {
			continue
		}

		if !inFence {
			inFence, fenceChar, fenceLen, openStart = true, ch, runLen, lineStart
			continue
		}
		if ch == fenceChar && runLen >= fenceLen {
			ranges = append(ranges, fenceRange{start: openStart, end: nextStart})
			inFence = false
		}
	}

	if inFence {
		ranges = append(ranges, fenceRange{start: openStart, end: len(contents)})
	}
	return ranges
}

func insideAnyFence(pos int, ranges []fenceRange) bool {
	for _, r := range ranges {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
}

func formatVardump(data map[string]any) string {
	var parts []string
	for key, value := range data {
		parts = append(parts, fmt.Sprintf("%s=%v", key, value))
	}
	return strings.Join(parts, ", ")
}

func LoadTemplate(
	base string,
	includePath string,
	path string,
	left string,
	right string,
	templates *template.Template,
) (*template.Template, error) {
	var (
		name = strings.TrimSuffix(path, filepath.Ext(path))
	)

	if template := templates.Lookup(name); template != nil {
		return template, nil
	}

	var body []byte

	body, err := os.ReadFile(filepath.Join(base, path))
	if err != nil {
		if includePath != "" {
			body, err = os.ReadFile(filepath.Join(includePath, path))
		}
		if err != nil {
			return nil, fmt.Errorf("unable to read template file %q: %w", path, err)
		}

	}

	body = bytes.ReplaceAll(
		body,
		[]byte("\r\n"),
		[]byte("\n"),
	)

	templates, err = templates.New(name).Delims(left, right).Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("unable to parse template %q: %w", name, err)
	}

	return templates, nil
}

func expandInclude(
	spec []byte,
	base string,
	includePath string,
	templates *template.Template,
) ([]byte, *template.Template, error) {
	groups := reIncludeDirective.FindSubmatch(spec)

	var (
		path       = string(groups[1])
		delimsNone = string(groups[2])
		left       = string(groups[3])
		right      = string(groups[4])
		config     = groups[5]
		data       = map[string]any{}
	)

	if delimsNone == "none" {
		left, right = "\x00", "\x01"
	}

	if err := yaml.Unmarshal(config, &data); err != nil {
		return nil, templates, fmt.Errorf("unable to unmarshal template data config (path=%q, config=%q): %w", path, string(config), err)
	}

	log.Trace().Interface("vardump", data).Msgf("including template %q", path)

	var err error
	templates, err = LoadTemplate(base, includePath, path, left, right, templates)
	if err != nil {
		return nil, templates, fmt.Errorf("unable to load template %q: %w", path, err)
	}

	var buffer bytes.Buffer
	if err := templates.Execute(&buffer, data); err != nil {
		return nil, templates, fmt.Errorf("unable to execute template %q (vars: %s): %w", path, formatVardump(data), err)
	}

	return buffer.Bytes(), templates, nil
}

// ProcessIncludes uses FindAllIndex + a manual append loop instead of the
// regexp.ReplaceAllFunc closure idiom used elsewhere in the project (see
// macro.ExtractMacros), because skipping directives inside fenced code blocks
// requires the byte position of each match to test against scanFenceRanges.
func ProcessIncludes(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
) (*template.Template, []byte, bool, error) {
	matches := reIncludeDirective.FindAllIndex(contents, -1)
	if len(matches) == 0 {
		return templates, contents, false, nil
	}

	fences := scanFenceRanges(contents)

	var (
		recurse bool
		err     error
	)

	out := make([]byte, 0, len(contents))
	last := 0

	for _, m := range matches {
		out = append(out, contents[last:m[0]]...)
		spec := contents[m[0]:m[1]]
		last = m[1]

		if err != nil || insideAnyFence(m[0], fences) {
			out = append(out, spec...)
			continue
		}

		var (
			expanded []byte
			expErr   error
		)
		expanded, templates, expErr = expandInclude(spec, base, includePath, templates)
		if expErr != nil {
			err = expErr
			out = append(out, spec...)
			continue
		}
		recurse = true
		out = append(out, expanded...)
	}

	out = append(out, contents[last:]...)
	return templates, out, recurse, err
}
