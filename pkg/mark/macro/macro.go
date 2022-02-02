package macro

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/kovetskiy/mark/pkg/mark/includes"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"github.com/reconquest/regexputil-go"
	"gopkg.in/yaml.v2"
)

var reMacroDirective = regexp.MustCompile(
	// <!-- Macro: <regexp>
	//      Template: <template path>
	//      <optional yaml data> -->

	`(?s)` + // dot capture newlines
		/**/ `<!--\s*Macro:\s*(?P<expr>[^\n]+)\n` +
		/*    */ `\s*Template:\s*(?P<template>\S+)\s*` +
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
				expr     = regexputil.Subexp(reMacroDirective, groups, "expr")
				template = regexputil.Subexp(
					reMacroDirective,
					groups,
					"template",
				)
				config = regexputil.Subexp(reMacroDirective, groups, "config")

				macro Macro
			)

			macro.Template, err = includes.LoadTemplate(base, template, templates)
			if err != nil {
				err = karma.Format(err, "unable to load template")

				return nil
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
