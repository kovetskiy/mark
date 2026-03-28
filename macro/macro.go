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

var reMacroDirective = regexp.MustCompile(
	// <!-- Macro: <regexp>
	//      Template: <template path>
	//      <optional yaml data> -->

	`(?s)` + // dot capture newlines
		/**/ `<!--\s*Macro:\s*(?P<expr>[^\n]+)\n` +
		/*    */ `\s*Template:\s*(?P<template>.+?)\s*` +
		/*   */ `(?P<config>\n.*?)?-->`,
)

type Macro struct {
	Regexp   *regexp.Regexp
	Template *template.Template
	Config   string
}

func (macro *Macro) Apply(
	content []byte,
) ([]byte, error) {
	var err error

	content = macro.Regexp.ReplaceAllFunc(
		content,
		func(match []byte) []byte {
			config := map[string]interface{}{}

			err = yaml.Unmarshal([]byte(macro.Config), &config)
			if err != nil {
				err = karma.Format(
					err,
					"unable to unmarshal macros config template",
				)
				return match
			}

			var buffer bytes.Buffer

			err = macro.Template.Execute(&buffer, macro.configure(
				config,
				macro.Regexp.FindSubmatch(match),
			))
			if err != nil {
				err = karma.Format(
					err,
					"unable to execute macros template",
				)
				return match
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

func ExtractMacros(
	base string,
	includePath string,
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

			groups := reMacroDirective.FindStringSubmatch(string(spec))

			var (
				expr     = groups[reMacroDirective.SubexpIndex("expr")]
				template = groups[reMacroDirective.SubexpIndex("template")]
				config   = groups[reMacroDirective.SubexpIndex("config")]
			)

			var macro Macro

			if strings.HasPrefix(template, "#") {
				cfg := map[string]interface{}{}

				err = yaml.Unmarshal([]byte(config), &cfg)
				if err != nil {
					err = karma.Format(
						err,
						"unable to unmarshal macros config template",
					)

					return nil
				}

				body, ok := cfg[template[1:]].(string)
				if !ok {
					err = fmt.Errorf(
						"the template config doesn't have '%s' field",
						template[1:],
					)

					return nil
				}

				macro.Template, err = templates.New(template).Parse(body)
				if err != nil {
					err = karma.Format(
						err,
						"unable to parse template",
					)

					return nil
				}
			} else {
				macro.Template, err = includes.LoadTemplate(base, includePath, template, "{{", "}}", templates)
				if err != nil {
					err = karma.Format(err, "unable to load template")

					return nil
				}
			}

			facts := karma.
				Describe("template", template).
				Describe("expr", expr)

			macro.Regexp, err = regexp.Compile(expr)
			if err != nil {
				err = facts.
					Format(
						err,
						"unable to compile macros regexp",
					)

				return nil
			}

			macro.Config = config

			log.Trace().
				Interface("vardump", facts.Describe("config", macro.Config)).
				Msgf("loaded macro %q", expr)

			macros = append(macros, macro)

			return []byte{}
		},
	)

	return macros, contents, err
}
