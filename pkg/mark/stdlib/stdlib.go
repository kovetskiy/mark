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
		"",
		text(
			`<!-- Macro: @\{([^}]+)\}`,
			`     Template: ac:link:user`,
			`     Name: ${1} -->`,

			// TODO(seletskiy): more macros here
		),

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
			"convertAttachment": func(data string) string {
				return strings.ReplaceAll(
					data,
					"/",
					"_",
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
			/**/ `<ac:layout-cell>{{ .Sidebar }}</ac:layout-cell>`,
			/**/ `</ac:layout-section>`,
			/**/ `</ac:layout>`,
			`{{ else }}`,
			/**/ `{{ .Body }}`,
			`{{ end }}`,
		),

		// This template is used for rendering code in ```
		`ac:code`: text(
			`<ac:structured-macro ac:name="{{ if eq .Language "mermaid" }}cloudscript-confluence-mermaid{{ else }}code{{ end }}">{{printf "\n"}}`,
			/**/ `{{ if eq .Language "mermaid" }}<ac:parameter ac:name="showSource">true</ac:parameter>{{printf "\n"}}{{ else }}`,
			/**/ `<ac:parameter ac:name="language">{{ .Language }}</ac:parameter>{{printf "\n"}}{{ end }}`,
			/**/ `<ac:parameter ac:name="collapse">{{ .Collapse }}</ac:parameter>{{printf "\n"}}`,
			/**/ `{{ if .Theme }}<ac:parameter ac:name="theme">{{ .Theme }}</ac:parameter>{{printf "\n"}}{{ end }}`,
			/**/ `{{ if .Linenumbers }}<ac:parameter ac:name="linenumbers">{{ .Linenumbers }}</ac:parameter>{{printf "\n"}}{{ end }}`,
			/**/ `{{ if .Firstline }}<ac:parameter ac:name="firstline">{{ .Firstline }}</ac:parameter>{{printf "\n"}}{{ end }}`,
			/**/ `{{ if .Title }}<ac:parameter ac:name="title">{{ .Title }}</ac:parameter>{{printf "\n"}}{{ end }}`,
			/**/ `<ac:plain-text-body><![CDATA[{{ .Text | cdata }}]]></ac:plain-text-body>{{printf "\n"}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
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

		/* https://confluence.atlassian.com/doc/jira-issues-macro-139380.html */
		`ac:jiraissues`: text(
			`<ac:structured-macro ac:name="jiraissues">`,
			`<ac:parameter ac:name="anonymous">{{ or .Anonymous false }}</ac:parameter>`,
			`<ac:parameter ac:name="baseurl">{{ or .BaseURL .URL }}</ac:parameter>`,
			`<ac:parameter ac:name="columns">{{ or .Columns "type;key;summary;assignee;reporter;priority;status;resolution;created;updated;due" }}</ac:parameter>`,
			`<ac:parameter ac:name="count">{{ or .Count false }}</ac:parameter>`,
			`<ac:parameter ac:name="cache">{{ or .Cache "on" }}</ac:parameter>`,
			`<ac:parameter ac:name="height">{{ or .Height 480 }}</ac:parameter>`,
			`<ac:parameter ac:name="renderMode">{{ or .RenderMode "static" }}</ac:parameter>`,
			`<ac:parameter ac:name="title">{{ or .Title "Jira Issues" }}</ac:parameter>`,
			`<ac:parameter ac:name="url">{{ .URL }}</ac:parameter>`,
			`<ac:parameter ac:name="width">{{ or .Width "100%" }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/info-tip-note-and-warning-macros-792499127.html */

		`ac:box`: text(
			`<ac:structured-macro ac:name="{{ .Name }}">{{printf "\n"}}`,
			`<ac:parameter ac:name="icon">{{ or .Icon "false" }}</ac:parameter>{{printf "\n"}}`,
			`{{ if .Title }}<ac:parameter ac:name="title">{{ .Title }}</ac:parameter>{{printf "\n"}}{{ end }}`,
			`<ac:rich-text-body>{{ .Body }}</ac:rich-text-body>{{printf "\n"}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
		),

		/* https://confluence.atlassian.com/conf59/table-of-contents-macro-792499210.html */

		`ac:toc`: text(
			`<ac:structured-macro ac:name="toc">{{printf "\n"}}`,
			`<ac:parameter ac:name="printable">{{ or .Printable "true" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="style">{{ or .Style "disc" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="maxLevel">{{ or .MaxLevel "7" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="indent">{{ or .Indent "" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="minLevel">{{ or .MinLevel "1" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="exclude">{{ or .Exclude "" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="type">{{ or .Type "list" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="outline">{{ or .Outline "clear" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="include">{{ or .Include "" }}</ac:parameter>{{printf "\n"}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
		),

		/* https://confluence.atlassian.com/doc/children-display-macro-139501.html */

		`ac:children`: text(
			`<ac:structured-macro ac:name="children">{{printf "\n"}}`,
			`{{ if .Reverse}}<ac:parameter ac:name="reverse">{{ or .Reverse }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Sort}}<ac:parameter ac:name="sort">{{ .Sort }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Style}}<ac:parameter ac:name="style">{{ .Style }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Page}}`,
			/**/ `<ac:parameter ac:name="page">`,
			/**/ `<ac:link>`,
			/**/ `<ri:page ri:content-title="{{ .Page }}"/>`,
			/**/ `</ac:link>`,
			/**/ `</ac:parameter>`,
			`{{printf "\n"}}{{end}}`,
			`{{ if .Excerpt}}<ac:parameter ac:name="excerptType">{{ .Excerpt }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .First}}<ac:parameter ac:name="first">{{ .First }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Depth}}<ac:parameter ac:name="depth">{{ .Depth }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .All}}<ac:parameter ac:name="all">{{ .All }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
		),

		/* https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html */

		`ac:emoticon`: text(
			`<ac:emoticon ac:name="{{ .Name }}"/>`,
		),

		`ac:image`: text(
			`<ac:image`,
			`{{ if .Width }} ac:width="{{ .Width }}"{{end}}`,
			`{{ if .Height }} ac:height="{{ .Height }}"{{end}}`,
			`{{ if .Title }} ac:title="{{ .Title }}"{{end}}`,
			`{{ if .Alt }} ac:alt="{{ .Alt }}"{{end}}>`,
			`{{ if .Attachment }}<ri:attachment ri:filename="{{ .Attachment | convertAttachment }}"/>{{end}}`,
			`{{ if .Url }}<ri:url ri:value="{{ .Url }}"/>{{end}}`,
			`</ac:image>`,
		),

		/* https://confluence.atlassian.com/doc/widget-connector-macro-171180449.html#WidgetConnectorMacro-YouTube */

		`ac:youtube`: text(
			`<ac:structured-macro ac:name="widget">{{printf "\n"}}`,
			`<ac:parameter ac:name="overlay">youtube</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="_template">com/atlassian/confluence/extra/widgetconnector/templates/youtube.vm</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="width">{{ or .Width "640px" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="height">{{ or .Height "360px" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="url"><ri:url ri:value="{{ .URL }}" /></ac:parameter>{{printf "\n"}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
		),

		/* https://support.atlassian.com/confluence-cloud/docs/insert-the-iframe-macro/ */

		`ac:iframe`: text(
			`<ac:structured-macro ac:name="iframe">{{printf "\n"}}`,
			`<ac:parameter ac:name="src"><ri:url ri:value="{{ .URL }}" /></ac:parameter>{{printf "\n"}}`,
			`{{ if .Frameborder }}<ac:parameter ac:name="frameborder">{{ .Frameborder }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Scrolling }}<ac:parameter ac:name="id">{{ .Scrolling }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Align }}<ac:parameter ac:name="align">{{ .Align }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`<ac:parameter ac:name="width">{{ or .Width "640px" }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="height">{{ or .Height "360px" }}</ac:parameter>{{printf "\n"}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
		),

		/* https://confluence.atlassian.com/doc/blog-posts-macro-139470.html */

		`ac:blog-posts`: text(
			`<ac:structured-macro ac:name="blog-posts">{{printf "\n"}}`,
			`{{ if .Content }}<ac:parameter ac:name="content">{{ .Content }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Spaces }}<ac:parameter ac:name="spaces">{{ .Spaces }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Author }}<ac:parameter ac:name="author">{{ .Author }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Time }}<ac:parameter ac:name="time">{{ .Time }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Reverse }}<ac:parameter ac:name="reverse">{{ .Reverse }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Sort }}<ac:parameter ac:name="sort">{{ .Sort }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Max }}<ac:parameter ac:name="max">{{ .Max }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`{{ if .Label }}<ac:parameter ac:name="label">{{ .Label }}</ac:parameter>{{printf "\n"}}{{end}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
		),

		/* https://confluence.atlassian.com/conf59/include-page-macro-792499125.html */

		`ac:include`: text(
			`<ac:structured-macro ac:name="include">{{printf "\n"}}`,
			`<ac:parameter ac:name="">{{printf "\n"}}`,
			`<ac:link>{{printf "\n"}}`,
			`<ri:page ri:content-title="{{ .Page }}" {{if .Space }}ri:space-key="{{ .Space }}"{{end}}/>{{printf "\n"}}`,
			`</ac:link>{{printf "\n"}}`,
			`</ac:parameter>{{printf "\n"}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
		),

		/* https://confluence.atlassian.com/conf59/excerpt-include-macro-792499101.html */

		`ac:excerpt-include`: text(
			`<ac:macro ac:name="excerpt-include">{{printf "\n"}}`,
			`<ac:parameter ac:name="nopanel">{{ if .NoPanel }}{{ .NoPanel }}{{ else }}false{{ end }}</ac:parameter>{{printf "\n"}}`,
			`<ac:default-parameter>{{ .Page }}</ac:default-parameter>{{printf "\n"}}`,
			`</ac:macro>{{printf "\n"}}`,
		),

		/* https://confluence.atlassian.com/conf59/excerpt-macro-792499102.html */

		`ac:excerpt`: text(
			`<ac:structured-macro ac:name="excerpt">{{printf "\n"}}`,
			`<ac:parameter ac:name="hidden">{{ if .Hidden }}{{ .Hidden }}{{ else }}false{{ end }}</ac:parameter>{{printf "\n"}}`,
			`<ac:parameter ac:name="atlassian-macro-output-type">{{ if .OutputType }}{{ .OutputType }}{{ else }}BLOCK{{ end }}</ac:parameter>{{printf "\n"}}`,
			`<ac:rich-text-body>{{printf "\n"}}`,
			`{{ .Excerpt }}{{printf "\n"}}`,
			`</ac:rich-text-body>{{printf "\n"}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
		),

		/* https://confluence.atlassian.com/conf59/anchor-macro-792499068.html */

		`ac:anchor`: text(
			`<ac:structured-macro ac:name="anchor">{{printf "\n"}}`,
			`<ac:parameter ac:name="">{{ .Anchor }}</ac:parameter>{{printf "\n"}}`,
			`</ac:structured-macro>{{printf "\n"}}`,
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
