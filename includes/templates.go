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
	formatVardump := func(
		data map[string]any,
	) string {
		var parts []string
		for key, value := range data {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}

		return strings.Join(parts, ", ")
	}

	var (
		recurse bool
		err     error
	)

	contents = reIncludeDirective.ReplaceAllFunc(
		contents,
		func(spec []byte) []byte {
			if err != nil {
				return spec
			}

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
				left = "\x00"
				right = "\x01"
			}

			err = yaml.Unmarshal(config, &data)
			if err != nil {
				err = fmt.Errorf("unable to unmarshal template data config (path=%q, config=%q): %w", path, string(config), err)

				return spec
			}

			log.Trace().Interface("vardump", data).Msgf("including template %q", path)

			templates, err = LoadTemplate(base, includePath, path, left, right, templates)
			if err != nil {
				err = fmt.Errorf("unable to load template %q: %w", path, err)
				return spec
			}

			var buffer bytes.Buffer

			err = templates.Execute(&buffer, data)
			if err != nil {
				err = fmt.Errorf("unable to execute template %q (vars: %s): %w", path, formatVardump(data), err)

				return spec
			}

			recurse = true

			return buffer.Bytes()
		},
	)

	return templates, contents, recurse, err
}
