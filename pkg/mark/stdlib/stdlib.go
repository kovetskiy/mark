package stdlib

import (
	"strings"
	"text/template"

	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/kovetskiy/mark/pkg/mark/macro"
	"github.com/reconquest/pkg/log"

	"github.com/reconquest/karma-go"
)

type Lib struct {
	Macros    []macro.Macro
	Templates *template.Template
}

func New(api *confluence.API) (*Lib, error) {
	var (
		lib Lib
		err error
	)

	lib.Templates, err = templates(api)
	if err != nil {
		return nil, err
	}

	lib.Macros, err = macros(lib.Templates)
	if err != nil {
		return nil, err
	}

	return &lib, nil
}

func macros(templates *template.Template) ([]macro.Macro, error) {
	text := func(line ...string) []byte {
		return []byte(strings.Join(line, "\n"))
	}

	macros, _, err := macro.ExtractMacros(
		[]byte(text(
			`<!-- Macro: @\{([^}]+)\}`,
			`     Template: ac:link:user`,
			`     Name: ${1} -->`,

			// TODO(seletskiy): more macros here
		)),

		templates,
	)
	if err != nil {
		return nil, err
	}

	return macros, nil
}

func templates(api *confluence.API) (*template.Template, error) {
	text := func(line ...string) string {
		return strings.Join(line, ``)
	}

	templates := template.New(`stdlib`).Funcs(
		template.FuncMap{
			"user": func(name string) *confluence.User {
				user, err := api.GetUserByName(name)
				if err != nil {
					log.Error(err)
				}

				return user
			},

			// The only way to escape CDATA end marker ']]>' is to split it
			// into two CDATA sections.
			"cdata": func(data string) string {
				return strings.ReplaceAll(
					data,
					"]]>",
					"]]><![CDATA[]]]]><![CDATA[>",
				)
			},
		},
	)

	var err error

	for name, body := range map[string]string{
		// This template is used to select whole article layout
		`ac:layout`: text(
			`{{ if eq .Layout "article" }}`,
			/**/ `<ac:layout>`,
			/**/ `<ac:layout-section ac:type="two_right_sidebar">`,
			/**/ `<ac:layout-cell>{{ .Body }}</ac:layout-cell>`,
			/**/ `<ac:layout-cell></ac:layout-cell>`,
			/**/ `</ac:layout-section>`,
			/**/ `</ac:layout>`,
			`{{ else }}`,
			/**/ `{{ .Body }}`,
			`{{ end }}`,
		),

		// This template is used for rendering code in ```
		`ac:code`: text(
			`<ac:structured-macro ac:name="code">`,
			`<ac:parameter ac:name="language">{{ .Language }}</ac:parameter>`,
			`<ac:parameter ac:name="collapse">false</ac:parameter>`,
			`<ac:plain-text-body><![CDATA[{{ .Text | cdata }}]]></ac:plain-text-body>`,
			`</ac:structured-macro>`,
		),

		`ac:status`: text(
			`<ac:structured-macro ac:name="status">`,
			`<ac:parameter ac:name="colour">{{ or .Color "Grey" }}</ac:parameter>`,
			`<ac:parameter ac:name="title">{{ or .Title .Color }}</ac:parameter>`,
			`<ac:parameter ac:name="subtle">{{ or .Subtle false }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		`ac:link:user`: text(
			`{{ with .Name | user }}`,
			/**/ `<ac:link>`,
			/**/ `<ri:user ri:account-id="{{ .AccountID }}"/>`,
			/**/ `</ac:link>`,
			`{{ else }}`,
			/**/ `{{ .Name }}`,
			`{{ end }}`,
		),

		`ac:jira:ticket`: text(
			`<ac:structured-macro ac:name="jira">`,
			`<ac:parameter ac:name="key">{{ .Ticket }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/table-of-contents-macro-792499210.html */

		`ac:toc`: text(
			`<ac:structured-macro ac:name="toc">`,
			`<ac:parameter ac:name="printable">true</ac:parameter>`,
			`<ac:parameter ac:name="style">disc</ac:parameter>`,
			`<ac:parameter ac:name="maxLevel">7</ac:parameter>`,
			`<ac:parameter ac:name="minLevel">1</ac:parameter>`,
			`<ac:parameter ac:name="exclude">{{ .Exclude }}</ac:parameter>`,
			`<ac:parameter ac:name="outline">false</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		// TODO(seletskiy): more templates here
	} {
		templates, err = templates.New(name).Parse(body)
		if err != nil {
			return nil, karma.
				Describe("template", body).
				Format(
					err,
					"unable to parse template",
				)
		}
	}

	return templates, nil
}
