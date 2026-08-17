package includes

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/rs/zerolog/log"
	"go.yaml.in/yaml/v3"
)

// IncludeDirective contains parsed parameters from an <!-- Include: ... --> block.
type IncludeDirective struct {
	Template string
	Left     string
	Right    string
	Data     map[string]any
}

func findIncludeDirectiveBounds(s string) (startIdx int, endIdx int) {
	return findIncludeDirective(s, nil)
}

// findIncludeDirective finds the first Include directive that skip does not
// reject, which is how a directive shown inside a code block is passed over
// rather than acted on.
func findIncludeDirective(s string, skip func(offset int) bool) (startIdx int, endIdx int) {
	searchFrom := 0
	for {
		start := strings.Index(s[searchFrom:], "<!--")
		if start == -1 {
			return -1, -1
		}
		startIdx = searchFrom + start

		// Search past the opener: starting at startIdx lets the closer overlap it,
		// so "<!-->" matches a "-->" at offset 2 and yields a 5-byte comment whose
		// body slice comment[4:2] panics.
		end := strings.Index(s[startIdx+4:], "-->")
		if end == -1 {
			return -1, -1
		}
		endIdx = startIdx + 4 + end + 3

		comment := s[startIdx:endIdx]
		trimmed := strings.TrimSpace(comment[4 : len(comment)-3])
		if strings.HasPrefix(trimmed, "Include:") && (skip == nil || !skip(startIdx)) {
			return startIdx, endIdx
		}

		searchFrom = endIdx
	}
}

// ParseIncludeDirective parses an <!-- Include: ... --> HTML comment block without regex.
func ParseIncludeDirective(raw []byte) (*IncludeDirective, error) {
	s := string(raw)
	startIdx, endIdx := findIncludeDirectiveBounds(s)
	if startIdx == -1 {
		return nil, nil
	}

	comment := strings.TrimSpace(s[startIdx+4 : endIdx-3])
	if !strings.HasPrefix(comment, "Include:") {
		return nil, nil
	}

	normalizedComment := strings.ReplaceAll(comment, "\\n", "\n")
	lines := strings.Split(normalizedComment, "\n")
	firstLine := strings.TrimSpace(lines[0])
	templatePath := strings.TrimSpace(strings.TrimPrefix(firstLine, "Include:"))
	if templatePath == "" {
		return nil, nil
	}

	dir := &IncludeDirective{
		Template: templatePath,
		Data:     make(map[string]any),
	}

	var configLines []string
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Delims:") {
			delimVal := strings.TrimSpace(strings.TrimPrefix(trimmed, "Delims:"))
			if delimVal == "none" {
				dir.Left = "\x00"
				dir.Right = "\x01"
			} else {
				parts := strings.Split(delimVal, ",")
				if len(parts) == 2 {
					dir.Left = strings.Trim(strings.TrimSpace(parts[0]), `"`)
					dir.Right = strings.Trim(strings.TrimSpace(parts[1]), `"`)
				}
			}
		} else if trimmed != "" {
			configLines = append(configLines, line)
		}
	}

	if len(configLines) > 0 {
		configStr := strings.Join(configLines, "\n")
		err := yaml.Unmarshal([]byte(configStr), &dir.Data)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshal template data config (path=%q, config=%q): %w", templatePath, configStr, err)
		}
	}

	return dir, nil
}

func LoadTemplate(
	base string,
	includePath string,
	path string,
	left string,
	right string,
	templates *template.Template,
) (*template.Template, error) {
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	name := strings.TrimSuffix(cleanPath, filepath.Ext(cleanPath))

	// A stdlib template (ac:box, ac:status, ...) is pre-registered under its plain
	// name and has no delimiters of its own, so it must always be found by that
	// name.
	if template := templates.Lookup(name); template != nil {
		return template, nil
	}

	// For file-backed templates the delimiters are part of what makes the parse,
	// so they belong in the cache key. Keying on the path alone meant a file
	// included twice with different Delims: reused the first parse and silently
	// discarded the second set, rendering the wrong output with no error.
	cacheName := name
	if left != "" || right != "" {
		cacheName = name + "\x00" + left + "\x00" + right
	}

	if template := templates.Lookup(cacheName); template != nil {
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

	// An included file may mark regions as not for Confluence just as the
	// document that includes it can. Stripping happens per file as each is
	// read, rather than once over the finished document, because by the time
	// the pieces are assembled the markers have already been through template
	// expansion -- and because the whole point of the feature is that the file
	// reads well on its own, which is as true of a fragment as of a page.
	body, err = metadata.StripIgnoredBlocks(body)
	if err != nil {
		return nil, fmt.Errorf("unable to process template file %q: %w", path, err)
	}

	templates, err = templates.New(cacheName).Delims(left, right).Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("unable to parse template %q: %w", name, err)
	}

	return templates, nil
}

func ProcessIncludes(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
) (*template.Template, []byte, bool, error) {
	return ProcessIncludesWithStack(base, includePath, contents, templates, nil)
}

func ProcessIncludesWithStack(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
	stack []string,
) (*template.Template, []byte, bool, error) {
	formatVardump := func(data map[string]any) string {
		var parts []string
		for key, value := range data {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
		return strings.Join(parts, ", ")
	}

	s := string(contents)

	// A directive inside a code block is a code sample: a page documenting how
	// to write an Include had the file pulled in where its own example should
	// have been.
	code := metadata.CodeRegions(contents)
	startIdx, endIdx := findIncludeDirective(s, func(offset int) bool {
		return metadata.InCode(code, offset)
	})
	if startIdx == -1 {
		return templates, contents, false, nil
	}

	rawDirective := contents[startIdx:endIdx]
	dir, err := ParseIncludeDirective(rawDirective)
	if err != nil {
		return templates, contents, false, err
	}
	if dir == nil {
		return templates, contents, false, nil
	}

	// Detect circular include loops
	cleanTmpl := filepath.Clean(dir.Template)
	for _, item := range stack {
		if filepath.Clean(item) == cleanTmpl {
			return templates, contents, false, fmt.Errorf("circular include detected: %s -> %s", strings.Join(append(stack, dir.Template), " -> "), dir.Template)
		}
	}

	log.Trace().Interface("vardump", dir.Data).Msgf("including template %q", dir.Template)

	// LoadTemplate returns the specific template it loaded, whose name encodes the
	// delimiters. Execute it directly rather than looking it up by path again,
	// which would find the wrong parse when the same file is included twice with
	// different Delims:.
	loaded, err := LoadTemplate(base, includePath, dir.Template, dir.Left, dir.Right, templates)
	if err != nil {
		return templates, contents, false, fmt.Errorf("unable to load template %q: %w", dir.Template, err)
	}
	templates = loaded

	var buffer bytes.Buffer
	err = loaded.Execute(&buffer, dir.Data)
	if err != nil {
		return templates, contents, false, fmt.Errorf("unable to execute template %q (vars: %s): %w", dir.Template, formatVardump(dir.Data), err)
	}

	// Recursively process nested includes with updated stack
	newStack := append(stack, dir.Template)
	subTemplates, subBytes, _, subErr := ProcessIncludesWithStack(base, includePath, buffer.Bytes(), templates, newStack)
	if subErr != nil {
		return templates, contents, false, subErr
	}
	templates = subTemplates

	var res bytes.Buffer
	res.Write(contents[:startIdx])
	res.Write(subBytes)
	res.Write(contents[endIdx:])

	return templates, res.Bytes(), true, nil
}

// DirectiveTargets reports the files a document asks to include, in the order
// they appear and skipping any shown inside a code block.
//
// Wanted so that a relative link written in an included fragment can be
// resolved against the fragment's own directory: the fragment reads as a
// document in its own right, and its links are written from where it lives
// rather than from wherever it is pulled into.
func DirectiveTargets(contents []byte) []string {
	s := string(contents)
	code := metadata.CodeRegions(contents)

	var targets []string
	searchFrom := 0

	for searchFrom < len(s) {
		start, end := findIncludeDirective(s[searchFrom:], func(offset int) bool {
			return metadata.InCode(code, searchFrom+offset)
		})
		if start == -1 {
			break
		}

		directive, err := ParseIncludeDirective([]byte(s[searchFrom+start : searchFrom+end]))
		if err == nil && directive != nil && directive.Template != "" {
			targets = append(targets, directive.Template)
		}

		searchFrom += end
	}

	return targets
}
