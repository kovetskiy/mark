package includes

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v2"

	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
)

// <!-- Include: <template path>
//      (Delims: (none | "<left>","<right>"))?
//      <optional yaml data> -->
var reIncludeDirective = regexp.MustCompile(
	`(?s)` +
	`<!--\s*Include:\s*(?P<template>.+?)\s*` +
	`(?:\n\s*Delims:\s*(?:(none|"(?P<left>.*?)"\s*,\s*"(?P<right>.*?)")))?\s*` +
	`(?:\n(?P<config>.*?))?-->`,
)

func LoadTemplate(
	base string,
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

	body, err := ioutil.ReadFile(filepath.Join(base, path))
	if err != nil {
		err = facts.Format(
			err,
			"unable to read template file",
		)

		return nil, err
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

	contents = reIncludeDirective.ReplaceAllFunc(
		contents,
		func(spec []byte) []byte {
			if err != nil {
				return nil
			}

			groups := reIncludeDirective.FindSubmatch(spec)

			var (
				path, none, left, right, config = string(groups[1]), string(groups[2]), string(groups[3]), string(groups[4]), groups[5]
				data = map[string]interface{}{}

				facts = karma.Describe("path", path)
			)
			if none == "none" {
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

				return nil
			}

			log.Tracef(vardump(facts, data), "including template %q", path)

			templates, err = LoadTemplate(base, path, left, right, templates)
			if err != nil {
				err = facts.Format(err, "unable to load template")

				return nil
			}

			var buffer bytes.Buffer

			err = templates.Execute(&buffer, data)
			if err != nil {
				err = vardump(facts, data).Format(
					err,
					"unable to execute template",
				)

				return nil
			}

			recurse = true

			return buffer.Bytes()
		},
	)

	return templates, contents, recurse, err
}
