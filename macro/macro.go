package macro

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/kovetskiy/mark/v16/includes"
	"github.com/rs/zerolog/log"
	"go.yaml.in/yaml/v3"
)

type Macro struct {
	Regexp   *regexp.Regexp
	Template *template.Template
	Config   string
	Name     string
}

// MacroDirective contains parsed parameters from a <!-- Macro: ... --> block.
type MacroDirective struct {
	Expr     string
	Template string
	Config   string
}

// ParseMacroDirective parses a <!-- Macro: ... --> HTML comment block without directive regexes.
func ParseMacroDirective(raw []byte) (*MacroDirective, error) {
	s := string(raw)
	startIdx := strings.Index(s, "<!--")
	if startIdx == -1 {
		return nil, nil
	}
	macroIdx := strings.Index(s[startIdx:], "Macro:")
	if macroIdx == -1 {
		return nil, nil
	}
	endIdx := strings.LastIndex(s[startIdx:], "-->")
	if endIdx == -1 {
		return nil, nil
	}
	endIdx += startIdx + 3

	comment := strings.TrimSpace(s[startIdx+4 : endIdx-3])
	if !strings.HasPrefix(comment, "Macro:") {
		return nil, nil
	}

	lines := strings.Split(comment, "\n")
	expr := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "Macro:"))

	var tmplPath string
	var configLines []string

	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if tmplPath == "" && strings.HasPrefix(trimmed, "Template:") {
			tmplPath = strings.TrimSpace(strings.TrimPrefix(trimmed, "Template:"))
		} else {
			configLines = append(configLines, line)
		}
	}

	if expr == "" || tmplPath == "" {
		return nil, nil
	}

	return &MacroDirective{
		Expr:     expr,
		Template: tmplPath,
		Config:   strings.Join(configLines, "\n"),
	}, nil
}

func (macro *Macro) Apply(
	content []byte,
) ([]byte, error) {
	var err error

	content = macro.Regexp.ReplaceAllFunc(
		content,
		func(match []byte) []byte {
			config := map[string]any{}

			err = yaml.Unmarshal([]byte(macro.Config), &config)
			if err != nil {
				err = fmt.Errorf("unable to unmarshal macros config template: %w", err)
				return match
			}

			cfgData := macro.configure(
				config,
				macro.Regexp.FindSubmatch(match),
			)

			tmpl := macro.Template
			if mData, ok := cfgData.(map[string]any); ok && macro.Name != "" {
				if body, ok := mData[macro.Name].(string); ok {
					if t, parseErr := template.New("inline").Parse(body); parseErr == nil {
						tmpl = t
					}
				}
			}

			var buffer bytes.Buffer

			err = tmpl.Execute(&buffer, cfgData)
			if err != nil {
				err = fmt.Errorf("unable to execute macros template: %w", err)
				return match
			}

			return buffer.Bytes()
		},
	)

	return content, err
}

func (macro *Macro) configure(node any, groups [][]byte) any {
	switch node := node.(type) {
	case map[any]any:
		for key, value := range node {
			node[key] = macro.configure(value, groups)
		}

		return node
	case map[string]any:
		for key, value := range node {
			node[key] = macro.configure(value, groups)
		}

		return node
	case []any:
		for key, value := range node {
			node[key] = macro.configure(value, groups)
		}

		return node
	case string:
		for i, group := range groups {
			node = strings.ReplaceAll(
				node,
				fmt.Sprintf("${%d}", i),
				string(group),
			)
		}

		return node
	}

	return node
}

func ExtractMacros(
	base string,
	includePath string,
	contents []byte,
	templates *template.Template,
) ([]Macro, []byte, error) {
	s := string(contents)
	startIdx := strings.Index(s, "<!--")
	if startIdx == -1 {
		return nil, contents, nil
	}
	macroIdx := strings.Index(s[startIdx:], "Macro:")
	if macroIdx == -1 {
		return nil, contents, nil
	}
	endIdx := strings.LastIndex(s[startIdx:], "-->")
	if endIdx == -1 {
		return nil, contents, nil
	}
	endIdx += startIdx + 3

	rawDirective := contents[startIdx:endIdx]
	dir, err := ParseMacroDirective(rawDirective)
	if err != nil {
		return nil, contents, err
	}
	if dir == nil {
		return nil, contents, nil
	}

	var m Macro
	if strings.HasPrefix(dir.Template, "#") {
		m.Name = dir.Template[1:]
		cfg := map[string]any{}

		err = yaml.Unmarshal([]byte(dir.Config), &cfg)
		if err != nil {
			return nil, contents, fmt.Errorf("unable to unmarshal macros config template: %w", err)
		}

		body, ok := cfg[m.Name].(string)
		if !ok {
			return nil, contents, fmt.Errorf("the template config doesn't have '%s' field", m.Name)
		}

		m.Template, err = templates.New(dir.Template).Parse(body)
		if err != nil {
			return nil, contents, fmt.Errorf("unable to parse template: %w", err)
		}
	} else {
		m.Template, err = includes.LoadTemplate(base, includePath, dir.Template, "{{", "}}", templates)
		if err != nil {
			return nil, contents, fmt.Errorf("unable to load template: %w", err)
		}
	}

	m.Regexp, err = regexp.Compile(dir.Expr)
	if err != nil {
		return nil, contents, fmt.Errorf("unable to compile macros regexp (expr=%q, template=%q): %w", dir.Expr, dir.Template, err)
	}

	m.Config = dir.Config

	log.Trace().
		Interface("vardump", map[string]any{
			"expr":     dir.Expr,
			"template": dir.Template,
			"config":   m.Config,
		}).
		Msgf("loaded macro %q", dir.Expr)

	var remaining bytes.Buffer
	remaining.Write(contents[:startIdx])
	remaining.Write(contents[endIdx:])

	return []Macro{m}, remaining.Bytes(), nil
}
