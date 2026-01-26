package includes

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
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

// reFencedCodeBlock matches fenced code blocks (``` or ~~~)
// It captures the opening fence to ensure the closing fence uses the same character
var reFencedCodeBlock = regexp.MustCompile("(?m)^(```+|~~~+)")

// codeBlockRange represents a range of bytes that is inside a code block
type codeBlockRange struct {
	start int
	end   int
}

// findCodeBlockRanges finds all fenced code block ranges in the content
// Returns a slice of ranges where each range represents content inside a code block
func findCodeBlockRanges(contents []byte) []codeBlockRange {
	var ranges []codeBlockRange

	matches := reFencedCodeBlock.FindAllIndex(contents, -1)
	if len(matches) == 0 {
		return ranges
	}

	// Process matches in pairs (opening and closing fences)
	i := 0
	for i < len(matches) {
		openStart := matches[i][0]
		openEnd := matches[i][1]
		openFence := string(contents[openStart:openEnd])
		fenceChar := openFence[0]
		fenceLen := len(openFence)

		// Find the matching closing fence
		foundClose := false
		for j := i + 1; j < len(matches); j++ {
			closeStart := matches[j][0]
			closeEnd := matches[j][1]
			closeFence := string(contents[closeStart:closeEnd])

			// Closing fence must use the same character and be at least as long
			if closeFence[0] == fenceChar && len(closeFence) >= fenceLen {
				// The code block includes everything from the start of the opening fence
				// to the end of the line containing the closing fence
				closeLineEnd := closeEnd
				for closeLineEnd < len(contents) && contents[closeLineEnd] != '\n' {
					closeLineEnd++
				}
				if closeLineEnd < len(contents) {
					closeLineEnd++ // include the newline
				}

				ranges = append(ranges, codeBlockRange{
					start: openStart,
					end:   closeLineEnd,
				})
				i = j + 1
				foundClose = true
				break
			}
		}

		if !foundClose {
			// No closing fence found, treat rest of document as code block
			ranges = append(ranges, codeBlockRange{
				start: openStart,
				end:   len(contents),
			})
			break
		}
	}

	return ranges
}

// isInsideCodeBlock checks if the given position is inside any code block
func isInsideCodeBlock(pos int, ranges []codeBlockRange) bool {
	for _, r := range ranges {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
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
		name  = strings.TrimSuffix(path, filepath.Ext(path))
		facts = karma.Describe("name", name)
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
			err = facts.Format(
				err,
				"unable to read template file",
			)
			return nil, err
		}

	}

	body = bytes.ReplaceAll(
		body,
		[]byte("\r\n"),
		[]byte("\n"),
	)

	templates, err = templates.New(name).Delims(left, right).Parse(string(body))
	if err != nil {
		err = facts.Format(
			err,
			"unable to parse template",
		)

		return nil, err
	}

	return templates, nil
}

func ProcessIncludes(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
) (*template.Template, []byte, bool, error) {
	vardump := func(
		facts *karma.Context,
		data map[string]interface{},
	) *karma.Context {
		for key, value := range data {
			key = "var " + key
			facts = facts.Describe(
				key,
				strings.ReplaceAll(
					fmt.Sprint(value),
					"\n",
					"\n"+strings.Repeat(" ", len(key)+2),
				),
			)
		}

		return facts
	}

	var (
		recurse bool
		err     error
	)

	// Find all code block ranges to skip Include directives inside them
	codeBlockRanges := findCodeBlockRanges(contents)

	// Find all matches and their positions
	matches := reIncludeDirective.FindAllIndex(contents, -1)
	if len(matches) == 0 {
		return templates, contents, false, nil
	}

	// Build result by processing matches that are outside code blocks
	var result []byte
	lastEnd := 0

	for _, match := range matches {
		matchStart := match[0]
		matchEnd := match[1]

		// Add content before this match
		result = append(result, contents[lastEnd:matchStart]...)

		// Check if this match is inside a code block
		if isInsideCodeBlock(matchStart, codeBlockRanges) {
			// Keep the original content unchanged
			result = append(result, contents[matchStart:matchEnd]...)
			lastEnd = matchEnd
			continue
		}

		// Process this Include directive
		spec := contents[matchStart:matchEnd]
		if err != nil {
			result = append(result, spec...)
			lastEnd = matchEnd
			continue
		}

		groups := reIncludeDirective.FindSubmatch(spec)

		var (
			path       = string(groups[1])
			delimsNone = string(groups[2])
			left       = string(groups[3])
			right      = string(groups[4])
			config     = groups[5]
			data       = map[string]interface{}{}

			facts = karma.Describe("path", path)
		)

		if delimsNone == "none" {
			left = "\x00"
			right = "\x01"
		}

		err = yaml.Unmarshal(config, &data)
		if err != nil {
			err = facts.
				Describe("config", string(config)).
				Format(
					err,
					"unable to unmarshal template data config",
				)

			result = append(result, spec...)
			lastEnd = matchEnd
			continue
		}

		log.Tracef(vardump(facts, data), "including template %q", path)

		templates, err = LoadTemplate(base, includePath, path, left, right, templates)
		if err != nil {
			err = facts.Format(err, "unable to load template")
			result = append(result, spec...)
			lastEnd = matchEnd
			continue
		}

		var buffer bytes.Buffer

		err = templates.Execute(&buffer, data)
		if err != nil {
			err = vardump(facts, data).Format(
				err,
				"unable to execute template",
			)

			result = append(result, spec...)
			lastEnd = matchEnd
			continue
		}

		recurse = true
		result = append(result, buffer.Bytes()...)
		lastEnd = matchEnd
	}

	// Add remaining content after the last match
	result = append(result, contents[lastEnd:]...)

	return templates, result, recurse, err
}
