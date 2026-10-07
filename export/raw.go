package export

import (
	"regexp"
	"strings"
)

// rawMode says where a piece of storage format written out verbatim ends up,
// which decides how its text has to be escaped.
type rawMode int

const (
	// rawInline is storage format inside Markdown prose. mark hands the
	// <ac:*> and <ri:*> tags through untouched, but the text between them is
	// still read as Markdown, so it is escaped as Markdown as well as XML.
	rawInline rawMode = iota

	// rawBlock is an HTML block -- a line opening with <table>, <p>, <div>
	// and the like -- which goldmark passes through as written, text and all.
	rawBlock

	// rawLine is storage format a metadata header carries, which mark
	// publishes as written: it is not Markdown, and has to fit on one line.
	rawLine
)

// voidElements are the HTML elements that never have content, and are written
// self-closed.
var voidElements = map[string]bool{
	"br": true, "hr": true, "img": true, "col": true, "wbr": true, "source": true,
}

// raw writes a node back out as storage format, on a single line.
//
// A single line, because either way it is read as Markdown first. A blank line
// ends a paragraph and an HTML block alike, and the lines of a paragraph lose
// their indentation, so a newline in the text is written as the character
// reference &#10; instead, which means the same thing to Confluence.
func (c *converter) raw(n *node, mode rawMode) string {
	var b strings.Builder
	c.writeRaw(&b, n, mode)

	return b.String()
}

func (c *converter) rawChildren(n *node, mode rawMode) string {
	var b strings.Builder
	for _, child := range n.children {
		c.writeRaw(&b, child, mode)
	}

	return b.String()
}

// structural are the elements whose children are elements, and the whitespace
// between them only layout.
var structural = map[string]bool{
	"ac:structured-macro": true, "ac:layout": true, "ac:layout-section": true,
	"ac:task-list": true, "ac:task": true, "ac:link": true, "ac:image": true,
	"table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true, "colgroup": true,
	"ul": true, "ol": true, "dl": true,
}

func (c *converter) writeRaw(b *strings.Builder, n *node, mode rawMode) {
	if n.isBlank() && n.parent != nil && structural[n.parent.name] {
		// A line break keeps a table readable; it cannot be a blank line,
		// which would end the block, because adjacent text is one node.
		if mode == rawBlock && strings.Contains(n.text, "\n") {
			b.WriteString("\n")
		}
		return
	}

	if n.kind == textNode {
		if n.parent != nil && n.parent.is("ac:plain-text-body", "ac:plain-text-link-body") {
			b.WriteString(cdata(n.text))
			return
		}

		b.WriteString(rawText(n.text, mode))
		return
	}

	b.WriteString(c.openTag(n))

	if len(n.children) == 0 {
		if strings.Contains(n.name, ":") || voidElements[n.name] {
			// The open tag is rewritten self-closed.
			return
		}
	}

	for _, child := range n.children {
		c.writeRaw(b, child, mode)
	}

	b.WriteString("</" + n.name + ">")
}

// openTag is the start tag of an element, self-closed when the element is
// written without content.
//
// An attachment of the page is renamed to what publishing the downloaded file
// back will call it, and recorded as one the document has to declare: mark
// uploads an image or a linked file on its own, but nothing it finds in raw
// storage format.
func (c *converter) openTag(n *node) string {
	var b strings.Builder
	b.WriteString("<" + n.name)

	for _, a := range n.attrs {
		value := a.value
		if n.is("ri:attachment") && a.name == "ri:filename" && len(n.elements()) == 0 {
			value = c.useAttachment(a.value, true)
			value = strings.ReplaceAll(value, "/", "_")
		}
		b.WriteString(" " + a.name + `="` + escapeAttr(value) + `"`)
	}

	if len(n.children) == 0 && (strings.Contains(n.name, ":") || voidElements[n.name]) {
		b.WriteString("/>")
	} else {
		b.WriteString(">")
	}

	return b.String()
}

// cdata writes text that Confluence keeps in a CDATA section -- the body of a
// code or plain-text macro -- as CDATA again, so that none of it needs
// escaping. Each line is a section of its own and the line breaks between them
// are character references; see raw.
func cdata(text string) string {
	lines := strings.Split(text, "\n")

	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("&#10;")
		}
		if line == "" {
			continue
		}
		// "]]>" is the one thing a CDATA section cannot hold; splitting it
		// across two sections is the only way to write it.
		b.WriteString("<![CDATA[" + strings.ReplaceAll(line, "]]>", "]]]]><![CDATA[>") + "]]>")
	}

	return b.String()
}

// rawText escapes text between tags written out verbatim.
func rawText(text string, mode rawMode) string {
	escaped := escapeXMLText(text)
	if mode == rawLine {
		return strings.ReplaceAll(escaped, "\n", "&#10;")
	}

	if mode == rawInline {
		escaped = escapeInline(escaped, inlineOptions{raw: true})

		return strings.ReplaceAll(escaped, "\n", "&#10;")
	}

	// An HTML block may run over several lines, as long as none is blank.
	for blankLine.MatchString(escaped) {
		escaped = blankLine.ReplaceAllString(escaped, "\n&#10;")
	}

	return escaped
}

var blankLine = regexp.MustCompile(`\n[ \t]*\n`)

func escapeXMLText(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

func escapeAttr(value string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\n", "&#10;", "\r", "&#13;", "\t", "&#9;",
	).Replace(value)
}
