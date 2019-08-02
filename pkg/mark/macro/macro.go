package macro

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/kovetskiy/mark/pkg/log"
	"github.com/kovetskiy/mark/pkg/mark/includes"
	"github.com/reconquest/karma-go"
	"gopkg.in/yaml.v2"
)

var reMacroDirective = regexp.MustCompile(
	`(?s)<!-- Macro: ([^\n]+)\n\s*Template: (\S+)\n(.*?)-->`,
)

type Macro struct {
	Regexp   *regexp.Regexp
	Template *template.Template
	Config   map[string]interface{}
}

func (macro *Macro) Apply(
	content []byte,
) ([]byte, error) {
	var err error

	content = macro.Regexp.ReplaceAllFunc(
		content,
		func(match []byte) []byte {
			config := macro.configure(
				macro.Config,
				macro.Regexp.FindSubmatch(match),
			)

			var buffer bytes.Buffer

			err = macro.Template.Execute(&buffer, config)
			if err != nil {
				err = karma.Format(
					err,
					"unable to execute macros template",
				)
			}

			return buffer.Bytes()
		},
	)

	return content, err
}

func (macro *Macro) configure(node interface{}, groups [][]byte) interface{} {
	switch node := node.(type) {
	case map[interface{}]interface{}:
		for key, value := range node {
			node[key] = macro.configure(value, groups)
		}

		return node
	case map[string]interface{}:
		for key, value := range node {
			node[key] = macro.configure(value, groups)
		}

		return node
	case []interface{}:
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

func LoadMacros(
	contents []byte,
	templates *template.Template,
) ([]Macro, []byte, error) {
	var err error

	var macros []Macro

	contents = reMacroDirective.ReplaceAllFunc(
		contents,
		func(spec []byte) []byte {
			if err != nil {
				return spec
			}

			groups := reMacroDirective.FindSubmatch(spec)

			var (
				expr, path, config = groups[1], string(groups[2]), groups[3]

				macro Macro
			)

			_, macro.Template, err = includes.LoadTemplate(path, templates)

			if err != nil {
				err = karma.Format(err, "unable to load template")

				return nil
			}

			facts := karma.
				Describe("template", path).
				Describe("expr", string(expr))

			macro.Regexp, err = regexp.Compile(string(expr))
			if err != nil {
				err = facts.
					Format(
						err,
						"unable to compile macros regexp",
					)

				return nil
			}

			err = yaml.Unmarshal(config, &macro.Config)
			if err != nil {
				err = facts.
					Describe("config", string(config)).
					Format(
						err,
						"unable to unmarshal template data config",
					)

				return nil
			}

			log.Tracef(
				facts.Describe("config", macro.Config),
				"loaded macro %q",
				expr,
			)

			macros = append(macros, macro)

			return []byte{}
		},
	)

	return macros, contents, err
}
