package export

import (
	"regexp"
	"strconv"
	"strings"
)

// macro renders a structured macro that stands on its own.
func (c *converter) macro(n *node, ctx blockContext) string {
	switch n.macroName() {
	case "code":
		if text, ok := c.codeMacro(n); ok {
			return text
		}
	case "info", "tip", "note", "warning":
		if text, ok := c.alert(n, ctx); ok {
			return text
		}
	case "expand":
		if text, ok := c.expand(n, ctx); ok {
			return text
		}
	}

	if body := n.child("ac:rich-text-body"); body != nil {
		return c.container(n, body, ctx)
	}

	return c.raw(n, rawInline)
}

// codeLanguage is what mark reads as the language of a fenced code block.
var codeLanguage = regexp.MustCompile(`^[\w#+./-]+$`)

// diagramLanguages are the fences mark draws rather than shows as code, with
// the features that do: a code macro in one of them is kept as the macro.
var diagramLanguages = map[string]bool{"mermaid": true, "d2": true, "plantuml": true, "math": true}

// codeOptions are the words of an info string mark reads as options rather
// than as a theme.
var codeOptions = map[string]bool{"collapse": true, "nocollapse": true, "linenumbers": true, "title": true}

// codeMacro writes a code macro as a fenced code block, with the info string
// renderer/fencedcodeblock.go reads back into the same parameters:
// "lang [collapse] [theme] [linenumbers|N] [title T]", with "-" for the
// language when there is none and anything follows it.
//
// Parameters the info string cannot carry -- the breakout width Confluence
// Cloud's editor adds, say -- are left behind.
func (c *converter) codeMacro(n *node) (string, bool) {
	params, plain := n.parameters()
	if !plain {
		return "", false
	}

	body := n.child("ac:plain-text-body")
	text := ""
	if body != nil {
		text = body.textContent()
	}

	language := strings.TrimSpace(params["language"])
	if language != "" && (!codeLanguage.MatchString(language) || language == "-" || diagramLanguages[strings.ToLower(language)]) {
		return "", false
	}

	var options []string
	if params["collapse"] == "true" {
		options = append(options, "collapse")
	}

	if theme := strings.TrimSpace(params["theme"]); theme != "" {
		if _, err := strconv.Atoi(theme); err == nil || codeOptions[strings.ToLower(theme)] || strings.ContainsAny(theme, " \t`") {
			return "", false
		}
		options = append(options, theme)
	}

	linenumbers := params["linenumbers"] == "true"
	if first, err := strconv.Atoi(strings.TrimSpace(params["firstline"])); err == nil && first > 0 && linenumbers {
		options = append(options, strconv.Itoa(first))
	} else if linenumbers {
		options = append(options, "linenumbers")
	}

	title := strings.TrimSpace(params["title"])
	if strings.ContainsAny(title, "\n`") {
		return "", false
	}

	info := language
	if len(options) > 0 || title != "" {
		if info == "" {
			info = "-"
		}
		if len(options) > 0 {
			info += " " + strings.Join(options, " ")
		}
		if title != "" {
			info += " title " + title
		}
	}

	return codeBlock(info, text), true
}

// alerts maps the macro and title mark publishes each GitHub alert as back to
// the alert; see renderer/gh_alerts_blockquote.go.
var alerts = map[[2]string]string{
	{"info", "Note"}:       "NOTE",
	{"info", "Important"}:  "IMPORTANT",
	{"tip", "Tip"}:         "TIP",
	{"note", "Warning"}:    "WARNING",
	{"warning", "Caution"}: "CAUTION",
}

// alert writes an info, tip, note or warning macro as the GitHub alert mark
// publishes as exactly that macro.
//
// Only a macro with the title mark gives each alert qualifies. Any other --
// one without a title, which is how Confluence's editor inserts them -- would
// come back with a title it did not have, so it is kept as the macro instead,
// with its body in Markdown. Alerts are only recognised at the top level of a
// document.
func (c *converter) alert(n *node, ctx blockContext) (string, bool) {
	if !ctx.root {
		return "", false
	}

	params, plain := n.parameters()
	if !plain {
		return "", false
	}

	for name, value := range params {
		switch {
		case name == "title":
		case name == "icon" && value == "true":
		default:
			return "", false
		}
	}

	kind, ok := alerts[[2]string{n.macroName(), params["title"]}]
	if !ok {
		return "", false
	}

	body := n.child("ac:rich-text-body")
	if body == nil {
		return "", false
	}

	inner := c.blocks(body.children, blockContext{})
	if inner == "" {
		return "> [!" + kind + "]", true
	}

	return quote("[!" + kind + "]\n\n" + inner), true
}

// expand writes an expand macro as <details>, which mark publishes as one.
func (c *converter) expand(n *node, ctx blockContext) (string, bool) {
	params, plain := n.parameters()
	if !plain {
		return "", false
	}

	for name := range params {
		if name != "title" {
			return "", false
		}
	}

	title, titled := params["title"]
	if strings.Contains(title, "\n") {
		return "", false
	}

	body := n.child("ac:rich-text-body")
	if body == nil {
		return "", false
	}

	var b strings.Builder
	b.WriteString("<details>\n")
	if titled {
		b.WriteString("<summary>" + escapeXMLText(title) + "</summary>\n")
	}

	if inner := c.blocks(body.children, ctx); inner != "" {
		b.WriteString("\n" + inner + "\n")
	}

	b.WriteString("\n</details>")

	return b.String(), true
}
