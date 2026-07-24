package includes

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

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

// ParseIncludeDirective parses an <!-- Include: ... --> HTML comment block without regex.
func ParseIncludeDirective(raw []byte) (*IncludeDirective, error) {
	s := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(s, "<!--") || !strings.HasSuffix(s, "-->") {
		return nil, nil
	}
	content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "<!--"), "-->"))
	if !strings.HasPrefix(content, "Include:") {
		return nil, nil
	}

	lines := strings.Split(content, "\n")
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

func ProcessIncludes(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
) (*template.Template, []byte, bool, error) {
	formatVardump := func(data map[string]any) string {
		var parts []string
		for key, value := range data {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
		return strings.Join(parts, ", ")
	}

	dir, err := ParseIncludeDirective(contents)
	if err != nil {
		return templates, contents, false, err
	}
	if dir == nil {
		return templates, contents, false, nil
	}

	log.Trace().Interface("vardump", dir.Data).Msgf("including template %q", dir.Template)

	templates, err = LoadTemplate(base, includePath, dir.Template, dir.Left, dir.Right, templates)
	if err != nil {
		return templates, contents, false, fmt.Errorf("unable to load template %q: %w", dir.Template, err)
	}

	var buffer bytes.Buffer
	err = templates.Execute(&buffer, dir.Data)
	if err != nil {
		return templates, contents, false, fmt.Errorf("unable to execute template %q (vars: %s): %w", dir.Template, formatVardump(dir.Data), err)
	}

	return templates, buffer.Bytes(), true, nil
}
