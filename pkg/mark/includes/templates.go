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

	"github.com/kovetskiy/mark/pkg/log"
	"github.com/reconquest/karma-go"
)

var (
	reIncludeDirective = regexp.MustCompile(`(?s)<!-- Include: (\S+)(.*?)-->`)
)

func LoadTemplate(
	path string,
	templates *template.Template,
) (string, *template.Template, error) {
	var (
		name  = strings.TrimSuffix(path, filepath.Ext(path))
		facts = karma.Describe("name", name)
	)

	if template := templates.Lookup(name); template != nil {
		return name, template, nil
	}

	var body []byte

	body, err := ioutil.ReadFile(path)
	if err != nil {
		err = facts.Format(
			err,
			"unable to read template file",
		)

		return name, nil, err
	}

	templates, err = templates.New(name).Parse(string(body))
	if err != nil {
		err = facts.Format(
			err,
			"unable to parse template",
		)

		return name, nil, err
	}

	return name, templates, nil
}

func ProcessIncludes(
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
				path, config = string(groups[1]), groups[2]
				data         = map[string]interface{}{}

				facts = karma.Describe("path", path)
			)

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

			var name string

			name, templates, err = LoadTemplate(path, templates)
			if err != nil {
				err = facts.Format(err, "unable to load template")

				return nil
			}

			facts = facts.Describe("name", name)

			template := templates.Lookup(string(name))
			if template == nil {
				err = facts.Reason("template not found")

				return nil
			}

			var buffer bytes.Buffer

			err = template.Execute(&buffer, data)
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
