package stdlib

import (
	"fmt"
	"html"
	"strings"
	"text/template"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

type Lib struct {
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

	return &lib, nil
}

func templates(api *confluence.API) (*template.Template, error) {
	text := func(line ...string) string {
		return strings.Join(line, ``)
	}

	templates := template.New(`stdlib`).Funcs(
		template.FuncMap{
			"user": func(name string) *confluence.User {
				if api == nil {
					return nil
				}
				user, err := api.GetUserByName(name)
				if err != nil {
					log.Error().Err(err).Send()
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
			// The result is always interpolated into an ri:filename attribute, so
			// it has to be escaped as well as slash-flattened: a quote in a
			// diagram title (```d2 title My "x" Diagram) otherwise closed the
			// attribute early and produced malformed XML.
			"convertAttachment": func(data string) string {
				return html.EscapeString(
					strings.ReplaceAll(
						data,
						"/",
						"_",
					),
				)
			},
			// Takes any so that a template may pipe a parameter with a
			// non-string default through it. A bool or a number reaching
			// html.EscapeString directly is a template execution error, which
			// would make escaping those parameters impossible without
			// rewriting every default as a string.
			"xmlesc": func(v any) string {
				return html.EscapeString(fmt.Sprint(v))
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
			`<ac:structured-macro ac:name="code">`,
			/**/ `<ac:parameter ac:name="language">{{ .Language | xmlesc }}</ac:parameter>`,
			/**/ `<ac:parameter ac:name="collapse">{{ .Collapse | xmlesc }}</ac:parameter>`,
			/**/ `{{ if .Theme }}<ac:parameter ac:name="theme">{{ .Theme | xmlesc }}</ac:parameter>{{ end }}`,
			/**/ `{{ if .Linenumbers }}<ac:parameter ac:name="linenumbers">{{ .Linenumbers | xmlesc }}</ac:parameter>{{ end }}`,
			/**/ `{{ if .Firstline }}<ac:parameter ac:name="firstline">{{ .Firstline | xmlesc }}</ac:parameter>{{ end }}`,
			/**/ `{{ if .Title }}<ac:parameter ac:name="title">{{ .Title | xmlesc }}</ac:parameter>{{ end }}`,
			/**/ `<ac:plain-text-body><![CDATA[{{ .Text | cdata }}]]></ac:plain-text-body>`,
			`</ac:structured-macro>`,
		),

		`ac:status`: text(
			`<ac:structured-macro ac:name="status">`,
			`<ac:parameter ac:name="colour">{{ or .Color "Grey" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="title">{{ or .Title .Color | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="subtle">{{ or .Subtle false | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		`ac:link:user`: text(
			`{{ with .Name | user }}`,
			/**/ `<ac:link>`,
			/**/ `{{ if .AccountID }}`,
			/****/ `<ri:user ri:account-id="{{ .AccountID | xmlesc }}" />`,
			/**/ `{{ else }}`,
			/****/ `<ri:user ri:userkey="{{ .UserKey | xmlesc }}" />`,
			/**/ `{{ end }}`,
			/**/ `</ac:link>`,
			`{{ else }}`,
			/**/ `{{ .Name | xmlesc }}`,
			`{{ end }}`,
		),

		`ac:jira:ticket`: text(
			`<ac:structured-macro ac:name="jira">`,
			`<ac:parameter ac:name="key">{{ .Ticket | xmlesc }}</ac:parameter>`,
			`{{ if .Server }}`,
			`<ac:parameter ac:name="server">{{ .Server | xmlesc }}</ac:parameter>`,
			`{{ end }}`,
			`</ac:structured-macro>`,
		),

		/* Used for rendering Jira Filters */

		`ac:jira:filter`: text(
			`<ac:structured-macro ac:name="jira">`,
			`<ac:parameter ac:name="server">{{ or .Server "System JIRA" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="jqlQuery">{{ .JQL | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/doc/jira-issues-macro-139380.html */
		`ac:jiraissues`: text(
			`<ac:structured-macro ac:name="jiraissues">`,
			`<ac:parameter ac:name="anonymous">{{ or .Anonymous false | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="baseurl"><ri:url ri:value="{{ or .BaseURL .URL | xmlesc }}" /></ac:parameter>`,
			`<ac:parameter ac:name="columns">{{ or .Columns "type;key;summary;assignee;reporter;priority;status;resolution;created;updated;due" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="count">{{ or .Count false | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="cache">{{ or .Cache "on" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="height">{{ or .Height 480 | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="renderMode">{{ or .RenderMode "static" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="title">{{ or .Title "Jira Issues" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="url"><ri:url ri:value="{{ .URL | xmlesc }}" /></ac:parameter>`,
			`<ac:parameter ac:name="width">{{ or .Width "100%" | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/info-tip-note-and-warning-macros-792499127.html */

		// The body is separated from the wrapper tags by blank lines; see
		// ac:details. A body ending in a list or table would otherwise absorb
		// the closing tags as content.
		`ac:box`: text(
			`<ac:structured-macro ac:name="{{ .Name | xmlesc }}">`,
			`<ac:parameter ac:name="icon">{{ or .Icon "false" | xmlesc }}</ac:parameter>`,
			`{{ if .Title }}<ac:parameter ac:name="title">{{ .Title | xmlesc }}</ac:parameter>{{ end }}`,
			"<ac:rich-text-body>\n\n{{ .Body }}\n\n</ac:rich-text-body>",
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/table-of-contents-macro-792499210.html */

		`ac:toc`: text(
			`<ac:structured-macro ac:name="toc">`,
			`<ac:parameter ac:name="printable">{{ or .Printable "true" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="style">{{ or .Style "disc" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="maxLevel">{{ or .MaxLevel "7" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="indent">{{ or .Indent "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="minLevel">{{ or .MinLevel "1" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="exclude">{{ or .Exclude "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="type">{{ or .Type "list" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="outline">{{ or .Outline "clear" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="include">{{ or .Include "" | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/doc/children-display-macro-139501.html */

		`ac:children`: text(
			`<ac:structured-macro ac:name="children">`,
			`{{ if .Reverse }}<ac:parameter ac:name="reverse">{{ or .Reverse | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Sort }}<ac:parameter ac:name="sort">{{ .Sort | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Style }}<ac:parameter ac:name="style">{{ .Style | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Page }}`,
			/**/ `<ac:parameter ac:name="page">`,
			/**/ `<ac:link>`,
			/**/ `<ri:page ri:content-title="{{ .Page | xmlesc }}"/>`,
			/**/ `</ac:link>`,
			/**/ `</ac:parameter>`,
			`{{ end }}`,
			`{{ if .Excerpt }}<ac:parameter ac:name="excerptType">{{ .Excerpt | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .First }}<ac:parameter ac:name="first">{{ .First | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Depth }}<ac:parameter ac:name="depth">{{ .Depth | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .All }}<ac:parameter ac:name="all">{{ .All | xmlesc }}</ac:parameter>{{ end }}`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html */

		// The optional attributes are what Confluence Cloud writes beside the
		// name, and what it reads the glyph from; Data Center understands none
		// of them and goes by ac:name alone. A caller that has only a name --
		// every use of this template that predates the emoji feature -- still
		// gets exactly the tag it got before.
		`ac:emoticon`: text(
			`<ac:emoticon ac:name="{{ .Name | xmlesc }}"`,
			`{{ with .ShortName }} ac:emoji-shortname="{{ . | xmlesc }}"{{ end }}`,
			`{{ with .ID }} ac:emoji-id="{{ . | xmlesc }}"{{ end }}`,
			`{{ with .Fallback }} ac:emoji-fallback="{{ . | xmlesc }}"{{ end }}`,
			`/>`,
		),

		`ac:image`: text(
			`<ac:image`,
			`{{ if .Align }} ac:align="{{ .Align | xmlesc }}"{{ end }}`,
			`{{ if .Layout }} ac:layout="{{ .Layout | xmlesc }}"{{ end }}`,
			`{{ if .OriginalWidth }} ac:original-width="{{ .OriginalWidth | xmlesc }}"{{ end }}`,
			`{{ if .OriginalHeight }} ac:original-height="{{ .OriginalHeight | xmlesc }}"{{ end }}`,
			`{{ if .Width }} ac:custom-width="true"{{ end }}`,
			`{{ if .Width }} ac:width="{{ .Width | xmlesc }}"{{ end }}`,
			`{{ if .Height }} ac:height="{{ .Height | xmlesc }}"{{ end }}`,
			`{{ if .Title }} ac:title="{{ .Title | xmlesc }}"{{ end }}`,
			`{{ if .Alt }} ac:alt="{{ .Alt | xmlesc }}"{{ end }}>`,
			`{{ if .Attachment }}<ri:attachment ri:filename="{{ .Attachment | convertAttachment }}"/>{{ end }}`,
			`{{ if .Url }}<ri:url ri:value="{{ .Url | xmlesc }}"/>{{ end }}`,
			`</ac:image>`,
		),

		/* https://confluence.atlassian.com/doc/widget-connector-macro-171180449.html#WidgetConnectorMacro-YouTube */

		`ac:youtube`: text(
			`<ac:structured-macro ac:name="widget">`,
			`<ac:parameter ac:name="overlay">youtube</ac:parameter>`,
			`<ac:parameter ac:name="_template">com/atlassian/confluence/extra/widgetconnector/templates/youtube.vm</ac:parameter>`,
			`<ac:parameter ac:name="width">{{ or .Width "640px" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="height">{{ or .Height "360px" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="url"><ri:url ri:value="{{ .URL | xmlesc }}" /></ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://support.atlassian.com/confluence-cloud/docs/insert-the-iframe-macro/ */

		`ac:iframe`: text(
			`<ac:structured-macro ac:name="iframe">`,
			`<ac:parameter ac:name="src"><ri:url ri:value="{{ .URL | xmlesc }}" /></ac:parameter>`,
			`{{ if .Frameborder }}<ac:parameter ac:name="frameborder">{{ .Frameborder | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Scrolling }}<ac:parameter ac:name="id">{{ .Scrolling | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Align }}<ac:parameter ac:name="align">{{ .Align | xmlesc }}</ac:parameter>{{ end }}`,
			`<ac:parameter ac:name="width">{{ or .Width "640px" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="height">{{ or .Height "360px" | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/doc/blog-posts-macro-139470.html */

		`ac:blog-posts`: text(
			`<ac:structured-macro ac:name="blog-posts">`,
			`{{ if .Content }}<ac:parameter ac:name="content">{{ .Content | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Spaces }}<ac:parameter ac:name="spaces">{{ .Spaces | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Author }}<ac:parameter ac:name="author">{{ .Author | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Time }}<ac:parameter ac:name="time">{{ .Time | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Reverse }}<ac:parameter ac:name="reverse">{{ .Reverse | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Sort }}<ac:parameter ac:name="sort">{{ .Sort | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Max }}<ac:parameter ac:name="max">{{ .Max | xmlesc }}</ac:parameter>{{ end }}`,
			`{{ if .Label }}<ac:parameter ac:name="label">{{ .Label | xmlesc }}</ac:parameter>{{ end }}`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/include-page-macro-792499125.html */

		`ac:include`: text(
			`<ac:structured-macro ac:name="include">`,
			`<ac:parameter ac:name="">`,
			`<ac:link>`,
			`<ri:page ri:content-title="{{ .Page | xmlesc }}" {{if .Space }}ri:space-key="{{ .Space | xmlesc }}"{{ end }}/>`,
			`</ac:link>`,
			`</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/excerpt-include-macro-792499101.html */
		/* https://support.atlassian.com/confluence-cloud/docs/insert-the-excerpt-include-macro/ */

		`ac:excerpt-include`: text(
			`<ac:macro ac:name="excerpt-include">`,
			`{{ if .Name }}<ac:parameter ac:name="name">{{ .Name | xmlesc }}</ac:parameter>{{ end }}`,
			`<ac:parameter ac:name="nopanel">{{ if .NoPanel }}{{ .NoPanel | xmlesc }}{{ else }}false{{ end }}</ac:parameter>`,
			`<ac:default-parameter>{{ .Page | xmlesc }}</ac:default-parameter>`,
			`</ac:macro>`,
		),

		/* https://confluence.atlassian.com/conf59/excerpt-macro-792499102.html */
		/* https://support.atlassian.com/confluence-cloud/docs/insert-the-excerpt-macro/ */

		`ac:excerpt`: text(
			`<ac:structured-macro ac:name="excerpt">`,
			`{{ if .Name }}<ac:parameter ac:name="name">{{ .Name | xmlesc }}</ac:parameter>{{ end }}`,
			`<ac:parameter ac:name="hidden">{{ if .Hidden }}{{ .Hidden | xmlesc }}{{ else }}false{{ end }}</ac:parameter>`,
			`<ac:parameter ac:name="atlassian-macro-output-type">{{ if .OutputType }}{{ .OutputType | xmlesc }}{{ else }}BLOCK{{ end }}</ac:parameter>`,
			`<ac:rich-text-body>`,
			`{{ .Excerpt | xmlesc }}`,
			`</ac:rich-text-body>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/anchor-macro-792499068.html */

		`ac:anchor`: text(
			`<ac:structured-macro ac:name="anchor">`,
			`<ac:parameter ac:name="">{{ .Anchor | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/*
			Footnotes. Confluence has no footnote of its own outside the
			marketplace, so a footnote is assembled from two things it does
			have: the bundled anchor macro, and a link that names an anchor
			instead of a page.

			An id attribute is not an option -- Confluence discards the ids in
			the storage format and generates its own from the element's text --
			so every jump target here has to be a real anchor macro. The macro
			renders nothing, which is what lets one sit in the middle of a
			sentence next to the marker it belongs to.

			A link with ac:anchor and no ri:* resource is a link within the
			current page; the same tag with an ri:page would leave the page
			first, which reloads it and loses the reader's place.
		*/

		`ac:footnote:anchor`: text(
			`<ac:structured-macro ac:name="anchor">`,
			`<ac:parameter ac:name="">{{ .Anchor | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		// A link to an anchor on the same page. Confluence keeps no id on a
		// heading and generates its own from the element's text, so the HTML
		// idiom -- id="X" on the heading, href="#X" on the link -- renders,
		// looks right, and does nothing when clicked. ac:link with ac:anchor
		// and no ri:page is the storage format's own way of saying it.
		`ac:link:anchor`: text(
			`<ac:link ac:anchor="{{ .Anchor | xmlesc }}">`,
			`<ac:link-body>`,
		),

		// The marker is superscript inside the link body rather than around
		// the whole link: ac:link-body is documented to take sup, while a
		// storage-format sup wrapping a macro is not, and the editor is free
		// to normalise what it was not promised.
		`ac:footnote:ref`: text(
			`<ac:link ac:anchor="{{ .Anchor | xmlesc }}">`,
			`<ac:link-body><sup>[{{ .Number | xmlesc }}]</sup></ac:link-body>`,
			`</ac:link>`,
		),

		// U+21A9 followed by U+FE0E: the variation selector asks for the text
		// glyph, without which the arrow is drawn as a colour emoji.
		// .Number is zero unless the note is cited more than once, in which
		// case each way back has to be told apart.
		`ac:footnote:backref`: text(
			`&#160;<ac:link ac:anchor="{{ .Anchor | xmlesc }}">`,
			`<ac:link-body>&#x21a9;&#xfe0e;{{ if .Number }}<sup>{{ .Number | xmlesc }}</sup>{{ end }}</ac:link-body>`,
			`</ac:link>`,
		),

		/* https://confluence.atlassian.com/conf59/expand-macro-792499106.html */

		// The body is separated from the wrapper tags by blank lines; see
		// ac:details. A body ending in a list or table would otherwise absorb
		// the closing tags as content.
		`ac:expand`: text(
			`<ac:structured-macro ac:name="expand">`,
			`<ac:parameter ac:name="title">{{ .Title | xmlesc }}</ac:parameter>`,
			"<ac:rich-text-body>\n\n{{ .Body }}\n\n</ac:rich-text-body>",
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/user-profile-macro-792499223.html */

		`ac:profile`: text(
			`{{ with .Name | user }}`,
			`<ac:structured-macro ac:name="profile">`,
			`<ac:parameter ac:name="user">`,
			`{{ if .AccountID }}`,
			/**/ `<ri:user ri:account-id="{{ .AccountID | xmlesc }}" />`,
			`{{ else }}`,
			/**/ `<ri:user ri:userkey="{{ .UserKey | xmlesc }}" />`,
			`{{ end }}`,
			`</ac:parameter>`,
			`</ac:structured-macro>`,
			`{{ end }}`,
		),

		/* https://confluence.atlassian.com/conf59/content-by-label-macro-792499087.html */

		`ac:contentbylabel`: text(
			`<ac:structured-macro ac:name="contentbylabel" ac:schema-version="3">`,
			`<ac:parameter ac:name="cql">{{ .CQL | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/page-properties-report-macro-792499165.html */

		`ac:detailssummary`: text(
			`<ac:structured-macro ac:name="detailssummary" ac:schema-version="2">`,
			`<ac:parameter ac:name="headings">{{ .Headings | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="firstcolumn">{{ .FirstColumn | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="sortBy">{{ .SortBy | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="cql">{{ .CQL | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/page-properties-macro-792499154.html */

		// The body is separated from the wrapper tags by blank lines. A body
		// ending in a table would otherwise put </ac:rich-text-body> on the line
		// straight after the final row, where GFM reads it as one more row and
		// absorbs the closing tags into a table cell.
		`ac:details`: text(
			`<ac:structured-macro ac:name="details" ac:schema-version="1">`,
			"<ac:rich-text-body>\n\n{{ .Body }}\n\n</ac:rich-text-body>",
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/page-tree-macro-792499177.html */

		`ac:pagetree`: text(
			`<ac:structured-macro ac:name="pagetree" ac:schema-version="1">`,
			`<ac:parameter ac:name="root">`,
			`<ac:link>`,
			`<ri:page ri:content-title="{{ or .Title "@self" | xmlesc }}"/>`,
			`</ac:link>`,
			`</ac:parameter>`,
			`<ac:parameter ac:name="sort">{{ or .Sort "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="excerpt">{{ or .Excerpt "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="reverse">{{ or .Reverse "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="searchBox">{{ or .SearchBox "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="expandCollapseAll">{{ or .ExpandCollapseAll "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="startDepth">{{ or .StartDepth "" | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/page-tree-search-macro-792499178.html */

		`ac:pagetreesearch`: text(
			`<ac:structured-macro ac:name="pagetreesearch">`,
			`{{ if .Root }}<ac:parameter ac:name="root">{{ .Root | xmlesc }}</ac:parameter>{{ end }}`,
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/panel-macro-792499179.html */

		// The body is separated from the wrapper tags by blank lines; see
		// ac:details. A body ending in a list or table would otherwise absorb
		// the closing tags as content.
		`ac:panel`: text(
			`<ac:structured-macro ac:name="panel">`,
			`<ac:parameter ac:name="bgColor">{{ or .BGColor "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="titleBGColor">{{ or .TitleBGColor "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="title">{{ or .Title "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="borderStyle">{{ or .BorderStyle "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="borderColor">{{ or .BorderColor "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="titleColor">{{ or .TitleColor "" | xmlesc }}</ac:parameter>`,
			"<ac:rich-text-body>\n\n{{ .Body }}\n\n</ac:rich-text-body>",
			`</ac:structured-macro>`,
		),

		/* https://confluence.atlassian.com/conf59/recently-updated-macro-792499187.html */
		`ac:recently-updated`: text(
			`<ac:structured-macro ac:name="recently-updated">`,
			`{{ if .Spaces }}<ac:parameter ac:name="spaces"><ri:space ri:space-key="{{ .Spaces | xmlesc }}"/></ac:parameter>{{ end }}`,
			`<ac:parameter ac:name="showProfilePic">{{ or .ShowProfilePic "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="types">{{ or .Types "page, comment, blogpost" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="max">{{ or .Max "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="labels">{{ or .Labels "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="hideHeading">{{ or .HideHeading "" | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="theme">{{ or .Theme "" | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),
		/* https://confluence.atlassian.com/conf59/column-macro-792499085.html */
		// The body is separated from the wrapper tags by blank lines; see
		// ac:details. A body ending in a list or table would otherwise absorb
		// the closing tags as content.
		`ac:column`: text(
			`<ac:structured-macro ac:name="column">`,
			`<ac:parameter ac:name="width">{{ or .Width "" | xmlesc }}</ac:parameter>`,
			"<ac:rich-text-body>\n\n{{ or .Body \"\" | xmlesc }}\n\n</ac:rich-text-body>",
			`</ac:structured-macro>`,
		),
		/* https://confluence.atlassian.com/conf59/multimedia-macro-792499140.html */
		`ac:multimedia`: text(
			`<ac:structured-macro ac:name="multimedia">`,
			`<ac:parameter ac:name="width">{{ or .Width 500 | xmlesc }}</ac:parameter>`,
			`<ac:parameter ac:name="name">`,
			`<ri:attachment ri:filename="{{ .Name | convertAttachment }}"/>`,
			`</ac:parameter>`,
			`<ac:parameter ac:name="autoplay">{{ or .AutoPlay "false" | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),
		/* https://confluence.atlassian.com/conf59/view-file-macro-792499226.html */
		`ac:view-file`: text(
			`<ac:structured-macro ac:name="view-file">`,
			`<ac:parameter ac:name="name">`,
			`<ri:attachment ri:filename="{{ .Name | convertAttachment }}"/>`,
			`</ac:parameter>`,
			`<ac:parameter ac:name="height">{{ or .Height 250 | xmlesc }}</ac:parameter>`,
			`</ac:structured-macro>`,
		),

		`ac:plantuml`: text(
			`<ac:structured-macro ac:name="plantuml">`,
			`<ac:plain-text-body><![CDATA[{{ .Text | cdata }}]]></ac:plain-text-body>`,
			`</ac:structured-macro>`,
		),

		// TODO(seletskiy): more templates here
	} {
		templates, err = templates.New(name).Parse(body)
		if err != nil {
			return nil, fmt.Errorf("unable to parse template %q (body=%s): %w", name, body, err)
		}
	}

	return templates, nil
}
