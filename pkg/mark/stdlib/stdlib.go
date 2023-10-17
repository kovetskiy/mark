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
			`<ac:structured-macro ac:name="{{ if eq .Language "mermaid" }}cloudscript-confluence-mermaid{{ else }}code{{ end }}">`,
			/**/ `{{ if eq .Language "mermaid" }}<ac:parameter ac:name="showSource">true</ac:parameter>{{ else }}`,
			/**/ `<ac:parameter ac:name="language">{{ .Language }}</ac:parameter>{{ end }}`,
			/**/ `<ac:parameter ac:name="collapse">{{ .Collapse }}</ac:parameter>`,
			/**/ `{{ if .Theme }}<ac:parameter ac:name="theme">{{ .Theme }}</ac:parameter>{{ end }}`,
			/**/ `{{ if .Linenumbers }}<ac:parameter ac:name="linenumbers">{{ .Linenumbers }}</ac:parameter>{{ end }}`,
			/**/ `{{ if .Firstline }}<ac:parameter ac:name="firstline">{{ .Firstline }}</ac:parameter>{{ end }}`,
			/**/ `{{ if .Title }}<ac:parameter ac:name="title">{{ .Title }}</ac:parameter>{{ end }}`,
			/**/ `<ac:plain-text-body><![CDATA[{{ .Text | cdata }}]]></ac:plain-text-body>`,
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
			/**/ `{{ if .AccountID }}`,
			/****/ `<ri:user ri:account-id="{{ .AccountID }}" />`,
			/**/ `{{ else }}`,
			/****/ `<ri:user ri:userkey="{{ .UserKey }}" />`,
			/**/ `{{ end }}`,
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

		/* Used for rendering Jira Filters */

		`ac:jira:filter`: text(
			`<ac:structured-macro ac:name="jira">`,
			`<ac:parameter ac:name="server">{{ or .Server "System JIRA" }}</ac:parameter>`,
			`<ac:parameter ac:name="jqlQuery">{{ .JQL }}</ac:parameter>`,
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
			`<ac:structured-macro ac:name="{{ .Name }}">`,
			`<ac:parameter ac:name="icon">{{ or .Icon "false" }}</ac:parameter>`,
			`{{ if .Title }}<ac:parameter ac:name="title">{{ .Title }}</ac:parameter>{{ end }}`,
			`<ac:rich-text-body>{{ .Body }}</ac:rich-text-body>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/table-of-contents-macro-792499210.html */

		`ac:toc`: text(
			`<ac:structured-macro ac:name="toc">`,
			`<ac:parameter ac:name="printable">{{ or .Printable "true" }}</ac:parameter>`,
			`<ac:parameter ac:name="style">{{ or .Style "disc" }}</ac:parameter>`,
			`<ac:parameter ac:name="maxLevel">{{ or .MaxLevel "7" }}</ac:parameter>`,
			`<ac:parameter ac:name="indent">{{ or .Indent "" }}</ac:parameter>`,
			`<ac:parameter ac:name="minLevel">{{ or .MinLevel "1" }}</ac:parameter>`,
			`<ac:parameter ac:name="exclude">{{ or .Exclude "" }}</ac:parameter>`,
			`<ac:parameter ac:name="type">{{ or .Type "list" }}</ac:parameter>`,
			`<ac:parameter ac:name="outline">{{ or .Outline "clear" }}</ac:parameter>`,
			`<ac:parameter ac:name="include">{{ or .Include "" }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/doc/children-display-macro-139501.html */

		`ac:children`: text(
			`<ac:structured-macro ac:name="children">`,
			`{{ if .Reverse }}<ac:parameter ac:name="reverse">{{ or .Reverse }}</ac:parameter>{{ end }}`,
			`{{ if .Sort }}<ac:parameter ac:name="sort">{{ .Sort }}</ac:parameter>{{ end }}`,
			`{{ if .Style }}<ac:parameter ac:name="style">{{ .Style }}</ac:parameter>{{ end }}`,
			`{{ if .Page }}`,
			/**/ `<ac:parameter ac:name="page">`,
			/**/ `<ac:link>`,
			/**/ `<ri:page ri:content-title="{{ .Page }}"/>`,
			/**/ `</ac:link>`,
			/**/ `</ac:parameter>`,
			`{{ end }}`,
			`{{ if .Excerpt }}<ac:parameter ac:name="excerptType">{{ .Excerpt }}</ac:parameter>{{ end }}`,
			`{{ if .First }}<ac:parameter ac:name="first">{{ .First }}</ac:parameter>{{ end }}`,
			`{{ if .Depth }}<ac:parameter ac:name="depth">{{ .Depth }}</ac:parameter>{{ end }}`,
			`{{ if .All }}<ac:parameter ac:name="all">{{ .All }}</ac:parameter>{{ end }}`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html */

		`ac:emoticon`: text(
			`<ac:emoticon ac:name="{{ .Name }}"/>`,
		),

		`ac:image`: text(
			`<ac:image`,
			`{{ if .Width }} ac:width="{{ .Width }}"{{ end }}`,
			`{{ if .Height }} ac:height="{{ .Height }}"{{ end }}`,
			`{{ if .Title }} ac:title="{{ .Title }}"{{ end }}`,
			`{{ if .Alt }} ac:alt="{{ .Alt }}"{{ end }}>`,
			`{{ if .Attachment }}<ri:attachment ri:filename="{{ .Attachment | convertAttachment }}"/>{{ end }}`,
			`{{ if .Url }}<ri:url ri:value="{{ .Url }}"/>{{ end }}`,
			`</ac:image>`,
		),

		/* https://confluence.atlassian.com/doc/widget-connector-macro-171180449.html#WidgetConnectorMacro-YouTube */

		`ac:youtube`: text(
			`<ac:structured-macro ac:name="widget">`,
			`<ac:parameter ac:name="overlay">youtube</ac:parameter>`,
			`<ac:parameter ac:name="_template">com/atlassian/confluence/extra/widgetconnector/templates/youtube.vm</ac:parameter>`,
			`<ac:parameter ac:name="width">{{ or .Width "640px" }}</ac:parameter>`,
			`<ac:parameter ac:name="height">{{ or .Height "360px" }}</ac:parameter>`,
			`<ac:parameter ac:name="url"><ri:url ri:value="{{ .URL }}" /></ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://support.atlassian.com/confluence-cloud/docs/insert-the-iframe-macro/ */

		`ac:iframe`: text(
			`<ac:structured-macro ac:name="iframe">`,
			`<ac:parameter ac:name="src"><ri:url ri:value="{{ .URL }}" /></ac:parameter>`,
			`{{ if .Frameborder }}<ac:parameter ac:name="frameborder">{{ .Frameborder }}</ac:parameter>{{ end }}`,
			`{{ if .Scrolling }}<ac:parameter ac:name="id">{{ .Scrolling }}</ac:parameter>{{ end }}`,
			`{{ if .Align }}<ac:parameter ac:name="align">{{ .Align }}</ac:parameter>{{ end }}`,
			`<ac:parameter ac:name="width">{{ or .Width "640px" }}</ac:parameter>`,
			`<ac:parameter ac:name="height">{{ or .Height "360px" }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/doc/blog-posts-macro-139470.html */

		`ac:blog-posts`: text(
			`<ac:structured-macro ac:name="blog-posts">`,
			`{{ if .Content }}<ac:parameter ac:name="content">{{ .Content }}</ac:parameter>{{ end }}`,
			`{{ if .Spaces }}<ac:parameter ac:name="spaces">{{ .Spaces }}</ac:parameter>{{ end }}`,
			`{{ if .Author }}<ac:parameter ac:name="author">{{ .Author }}</ac:parameter>{{ end }}`,
			`{{ if .Time }}<ac:parameter ac:name="time">{{ .Time }}</ac:parameter>{{ end }}`,
			`{{ if .Reverse }}<ac:parameter ac:name="reverse">{{ .Reverse }}</ac:parameter>{{ end }}`,
			`{{ if .Sort }}<ac:parameter ac:name="sort">{{ .Sort }}</ac:parameter>{{ end }}`,
			`{{ if .Max }}<ac:parameter ac:name="max">{{ .Max }}</ac:parameter>{{ end }}`,
			`{{ if .Label }}<ac:parameter ac:name="label">{{ .Label }}</ac:parameter>{{ end }}`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/include-page-macro-792499125.html */

		`ac:include`: text(
			`<ac:structured-macro ac:name="include">`,
			`<ac:parameter ac:name="">`,
			`<ac:link>`,
			`<ri:page ri:content-title="{{ .Page }}" {{if .Space }}ri:space-key="{{ .Space }}"{{ end }}/>`,
			`</ac:link>`,
			`</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/excerpt-include-macro-792499101.html */

		`ac:excerpt-include`: text(
			`<ac:macro ac:name="excerpt-include">`,
			`<ac:parameter ac:name="nopanel">{{ if .NoPanel }}{{ .NoPanel }}{{ else }}false{{ end }}</ac:parameter>`,
			`<ac:default-parameter>{{ .Page }}</ac:default-parameter>`,
			`</ac:macro>`,
		),

		/* https://confluence.atlassian.com/conf59/excerpt-macro-792499102.html */

		`ac:excerpt`: text(
			`<ac:structured-macro ac:name="excerpt">`,
			`<ac:parameter ac:name="hidden">{{ if .Hidden }}{{ .Hidden }}{{ else }}false{{ end }}</ac:parameter>`,
			`<ac:parameter ac:name="atlassian-macro-output-type">{{ if .OutputType }}{{ .OutputType }}{{ else }}BLOCK{{ end }}</ac:parameter>`,
			`<ac:rich-text-body>`,
			`{{ .Excerpt }}`,
			`</ac:rich-text-body>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/anchor-macro-792499068.html */

		`ac:anchor`: text(
			`<ac:structured-macro ac:name="anchor">`,
			`<ac:parameter ac:name="">{{ .Anchor }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/expand-macro-792499106.html */

		`ac:expand`: text(
			`<ac:structured-macro ac:name="expand">`,
			`<ac:parameter ac:name="title">{{ .Title }}</ac:parameter>`,
			`<ac:rich-text-body>{{ .Body }}</ac:rich-text-body>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/user-profile-macro-792499223.html */

		`ac:profile`: text(
			`{{ with .Name | user  }}`,
			`<ac:structured-macro ac:name="profile">`,
			`<ac:parameter ac:name="user">`,
			`{{ if .AccountID }}`,
			/**/ `<ri:user ri:account-id="{{ .AccountID }}" />`,
			`{{ else }}`,
			/**/ `<ri:user ri:userkey="{{ .UserKey }}" />`,
			`{{ end }}`,
			`</ac:parameter>`,
			`</ac:structured-macro>`,
			`{{ end }}`,
		),

		/* https://confluence.atlassian.com/conf59/content-by-label-macro-792499087.html */

		`ac:contentbylabel`: text(
			`<ac:structured-macro ac:name="contentbylabel" ac:schema-version="3">`,
			`<ac:parameter ac:name="cql">{{ .CQL }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/page-properties-report-macro-792499165.html */

		`ac:detailssummary`: text(
			`<ac:structured-macro ac:name="detailssummary" ac:schema-version="2">`,
			`<ac:parameter ac:name="headings">{{ .Headings }}</ac:parameter>`,
			`<ac:parameter ac:name="sortBy">{{ .SortBy }}</ac:parameter>`,
			`<ac:parameter ac:name="cql">{{ .CQL }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/page-properties-macro-792499154.html */

		`ac:details`: text(
			`<ac:structured-macro ac:name="details" ac:schema-version="1"">`,
			`<ac:rich-text-body>{{ .Body }}</ac:rich-text-body>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/page-tree-macro-792499177.html */

		`ac:pagetree`: text(
			`<ac:structured-macro ac:name="pagetree" ac:schema-version="1">`,
			`<ac:parameter ac:name="root">`,
			`<ac:link>`,
			`<ri:page ri:content-title="@self"{{ or .Title "" }}/>`,
			`</ac:link>`,
			`</ac:parameter>`,
			`<ac:parameter ac:name="sort">{{ or .Sort "" }}</ac:parameter>`,
			`<ac:parameter ac:name="excerpt">{{ or .Excerpt "" }}</ac:parameter>`,
			`<ac:parameter ac:name="reverse">{{ or .Reverse "" }}</ac:parameter>`,
			`<ac:parameter ac:name="searchBox">{{ or .SearchBox "" }}</ac:parameter>`,
			`<ac:parameter ac:name="expandCollapseAll">{{ or .ExpandCollapseAll "" }}</ac:parameter>`,
			`<ac:parameter ac:name="startDepth">{{ or .StartDepth "" }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/page-tree-search-macro-792499178.html */

		`ac:pagetreesearch`: text(
			`<ac:structured-macro ac:name="pagetreesearch">`,
			`{{ if .Root }}<ac:parameter ac:name="root">{{ .Root }}</ac:parameter>{{ end }}`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/panel-macro-792499179.html */

		`ac:panel`: text(
			`<ac:structured-macro ac:name="panel">`,
			`<ac:parameter ac:name="bgColor">{{ or .BGColor "" }}</ac:parameter>`,
			`<ac:parameter ac:name="titleBGColor">{{ or .TitleBGColor "" }}</ac:parameter>`,
			`<ac:parameter ac:name="title">{{ or .Title "" }}</ac:parameter>`,
			`<ac:parameter ac:name="borderStyle">{{ or .BorderStyle "" }}</ac:parameter>`,
			`<ac:parameter ac:name="borderColor">{{ or .BorderColor "" }}</ac:parameter>`,
			`<ac:parameter ac:name="titleColor">{{ or .TitleColor "" }}</ac:parameter>`,
			`<ac:rich-text-body>{{ .Body }}</ac:rich-text-body>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/recently-updated-macro-792499187.html */
		`ac:recently-updated`: text(
			`<ac:structured-macro ac:name="recently-updated">`,
			`{{ if .Spaces }}<ac:parameter ac:name="spaces"><ri:space ri:space-key={{ .Spaces }}/></ac:parameter>{{ end }}`,
			`<ac:parameter ac:name="showProfilePic">{{ or .ShowProfilePic "" }}</ac:parameter>`,
			`<ac:parameter ac:name="types">{{ or .Types "page, comment, blogpost" }}</ac:parameter>`,
			`<ac:parameter ac:name="max">{{ or .Max "" }}</ac:parameter>`,
			`<ac:parameter ac:name="labels">{{ or .Labels "" }}</ac:parameter>`,
			`<ac:parameter ac:name="hideHeading">{{ or .HideHeading "" }}</ac:parameter>`,
			`<ac:parameter ac:name="theme">{{ or .Theme "" }}</ac:parameter>`,
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
