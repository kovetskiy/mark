package export

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kovetskiy/mark/v16/transformer"
)

type inlineContext struct {
	// singleLine is a heading or a table cell, which cannot break a line: a
	// soft break is written as a space, and a hard one as <br/>.
	singleLine bool
	// table is a GFM table cell, where a pipe has to be escaped.
	table bool
}

// inlineWriter renders a run of inline nodes.
type inlineWriter struct {
	c   *converter
	ctx inlineContext
	b   strings.Builder

	// afterBreak is set by a hard line break, whose own newline replaces the
	// one the text after it starts with.
	afterBreak bool
}

func (c *converter) inline(ctx inlineContext) *inlineWriter {
	return &inlineWriter{c: c, ctx: ctx}
}

func (w *inlineWriter) finish() string {
	out := w.b.String()
	// A hard break at the very end breaks nothing, and Markdown would read
	// the backslash as text.
	return strings.TrimSuffix(strings.TrimRight(out, " \n"), "\\")
}

func (w *inlineWriter) write(s string) {
	if s == "" {
		return
	}
	w.b.WriteString(s)
	w.afterBreak = false
}

func (w *inlineWriter) lastRune() rune {
	s := w.b.String()
	if s == "" {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s)

	return r
}

func (w *inlineWriter) nodes(nodes []*node) {
	for _, n := range nodes {
		w.node(n)
	}
}

func (w *inlineWriter) node(n *node) {
	if n.kind == textNode {
		w.text(n.text)
		return
	}

	switch n.name {
	case "strong", "b":
		w.emphasis(n, "**", "strong")
	case "em", "i":
		w.emphasis(n, "*", "em")
	case "s", "del", "strike":
		w.emphasis(n, "~~", "del")
	case "code", "tt":
		w.codeSpan(n)
	case "a":
		w.link(n)
	case "br":
		w.lineBreak()
	case "ac:link":
		w.acLink(n)
	case "ac:image":
		w.image(n)
	case "ac:inline-comment-marker":
		// Inline comments belong to Confluence, which keeps them, and to
		// --preserve-comments, which puts their markers back on publish.
		w.nodes(n.children)
	case "p":
		// A paragraph inside something that holds only inline content -- a
		// task body, a table cell -- is its text.
		w.nodes(n.children)
	default:
		w.raw(n)
	}
}

// rendersRaw reports whether an element is written as storage format rather
// than as Markdown.
func (c *converter) rendersRaw(n *node) bool {
	w := c.inline(inlineContext{})
	switch n.name {
	case "strong", "b", "em", "i", "s", "del", "strike", "code", "tt", "br", "ac:inline-comment-marker":
		return false
	case "a":
		return !w.plainLink(n)
	case "ac:link":
		_, ok := w.acLinkMarkdown(n)
		return !ok
	case "ac:image":
		_, ok := w.imageMarkdown(n, false)
		return !ok
	}

	return true
}

// raw writes an element as storage format. An HTML element Markdown has no
// syntax for -- <span style>, <u>, <sup> -- keeps its tags, and what it holds
// is still converted; anything Confluence's own is written whole.
func (w *inlineWriter) raw(n *node) {
	if !strings.Contains(n.name, ":") && !isBlock(n) && len(n.children) > 0 {
		w.write(w.c.openTag(n))
		w.nodes(n.children)
		w.write("</" + n.name + ">")
		return
	}

	w.write(w.c.raw(n, rawInline))
}

func (w *inlineWriter) text(text string) {
	if w.ctx.singleLine {
		text = softBreakSpaces.ReplaceAllString(text, " ")
	} else {
		text = softBreakSpaces.ReplaceAllString(text, "\n")
	}

	if w.afterBreak {
		text = strings.TrimLeft(text, " \t\n")
	}

	w.write(escapeInline(text, inlineOptions{table: w.ctx.table}))
}

func (w *inlineWriter) lineBreak() {
	if w.ctx.singleLine {
		if w.ctx.table {
			w.write("<br/>")
		} else {
			w.write(" ")
		}
		return
	}

	w.write("\\\n")
	w.afterBreak = true
}

// emphasis writes strong, emphasis and strikethrough with Markdown's
// delimiters where they are sure to be read back as such, and as the HTML
// element where they might not be: "**" has to touch the text it wraps, and
// next to punctuation it only counts when the other side is space or
// punctuation too.
func (w *inlineWriter) emphasis(n *node, delimiter, tag string) {
	sub := w.c.inline(w.ctx)
	sub.nodes(n.children)
	inner := sub.b.String()

	core := strings.TrimLeftFunc(inner, unicode.IsSpace)
	lead := inner[:len(inner)-len(core)]
	trimmed := strings.TrimRightFunc(core, unicode.IsSpace)
	trail := core[len(trimmed):]
	core = trimmed

	w.write(lead)
	if core == "" {
		w.write(trail)
		return
	}

	first, _ := utf8.DecodeRuneInString(core)
	last, _ := utf8.DecodeLastRuneInString(core)

	next := rune(' ')
	if trail == "" {
		next = followingRune(n)
	}

	opens := !isPunct(first) || isSpaceOrPunct(w.lastRune())
	closes := !isPunct(last) || isSpaceOrPunct(next)

	if opens && closes && !strings.Contains(core, "\n") {
		w.write(delimiter + core + delimiter)
	} else {
		w.write("<" + tag + ">" + core + "</" + tag + ">")
	}
	w.write(trail)
}

func isPunct(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func isSpaceOrPunct(r rune) bool {
	return r == 0 || unicode.IsSpace(r) || isPunct(r)
}

// followingRune is the first character after n, as far as can be told from
// here: the text of what follows it, up through the inline elements it sits
// in. Nothing after it reads as a space.
func followingRune(n *node) rune {
	for current := n; current != nil && current.parent != nil; current = current.parent {
		siblings := current.parent.children
		found := false
		for _, sibling := range siblings {
			if found {
				text := sibling.textContent()
				if sibling.kind == elementNode && text == "" {
					// An element with no text of its own is written as
					// markup, which starts with punctuation.
					return '<'
				}
				if text != "" {
					r, _ := utf8.DecodeRuneInString(text)
					return r
				}
			}
			if sibling == current {
				found = true
			}
		}

		if isBlock(current.parent) || current.parent.name == storageRoot {
			break
		}
	}

	return ' '
}

// codeSpan writes code as a code span, with a run of backticks longer than
// any inside it.
func (w *inlineWriter) codeSpan(n *node) {
	content := strings.ReplaceAll(n.textContent(), "\n", " ")
	if content == "" {
		return
	}

	if w.ctx.table {
		content = strings.ReplaceAll(content, "|", "\\|")
	}

	f := fence(content, 1)
	pad := ""
	if strings.HasPrefix(content, "`") || strings.HasSuffix(content, "`") ||
		(len(content) > 1 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.TrimSpace(content) != "") {
		pad = " "
	}

	w.write(f + pad + content + pad + f)
}

// plainAttrs reports whether an element carries only the attributes named.
func plainAttrs(n *node, allowed ...string) bool {
	for _, a := range n.attrs {
		ok := false
		for _, name := range allowed {
			if a.name == name {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}

	return true
}

func (w *inlineWriter) plainLink(n *node) bool {
	return plainAttrs(n, "href", "title")
}

// link writes an <a>, as an autolink when it shows its own address.
func (w *inlineWriter) link(n *node) {
	if !w.plainLink(n) {
		w.raw(n)
		return
	}

	href := n.attr("href")
	title := n.attr("title")
	text := n.textContent()

	if title == "" && len(n.elements()) == 0 && !strings.ContainsAny(href, " <>\n") {
		if (strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "http://")) && text == href {
			w.write("<" + href + ">")
			return
		}
		if address, ok := strings.CutPrefix(href, "mailto:"); ok && text == address && strings.Contains(address, "@") {
			w.write("<" + address + ">")
			return
		}
	}

	sub := w.c.inline(w.ctx)
	sub.nodes(n.children)

	w.write("[" + strings.TrimSpace(sub.b.String()) + "](" + destination(href) + linkTitle(title) + ")")
}

// destination writes a link destination: as it is where it can be, and in
// angle brackets where it holds a space or a parenthesis.
func destination(href string) string {
	if href == "" {
		return ""
	}

	if !strings.ContainsAny(href, " ()<>\\\t\n") {
		return href
	}

	href = strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A", "\\", "\\\\").Replace(href)

	return "<" + href + ">"
}

func linkTitle(title string) string {
	if title == "" {
		return ""
	}

	return ` "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(title) + `"`
}

// pathDestination writes the path of a downloaded attachment as a link
// destination, percent-encoding what a URL cannot hold as it is: mark decodes
// it again when it looks for the file.
func pathDestination(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		ch := path[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '-', ch == '.', ch == '_', ch == '~', ch == '/', ch >= 0x80:
			b.WriteByte(ch)
		default:
			b.WriteString("%" + strings.ToUpper(hex(ch)))
		}
	}

	return b.String()
}

func hex(ch byte) string {
	const digits = "0123456789abcdef"

	return string([]byte{digits[ch>>4], digits[ch&0xf]})
}

// linkBody is what an ac:link shows, and whether it has anything to show.
func (w *inlineWriter) linkBody(n *node) (string, bool) {
	if body := n.child("ac:plain-text-link-body"); body != nil {
		text := body.textContent()
		if strings.TrimSpace(text) != "" {
			return escapeInline(strings.Join(strings.Fields(text), " "), inlineOptions{table: w.ctx.table}), true
		}
	}

	if body := n.child("ac:link-body"); body != nil {
		sub := w.c.inline(inlineContext{singleLine: true, table: w.ctx.table})
		sub.nodes(body.children)
		if text := strings.TrimSpace(sub.b.String()); text != "" {
			return text, true
		}
	}

	return "", false
}

// literalLinkText is the text of a link to a page or an attachment, which mark
// publishes from the characters of [text] as they are written -- escapes and
// emphasis markers included, not what they mean. Only text that needs no
// escaping can be written there; fallback is shown when the link has none.
func (w *inlineWriter) literalLinkText(n *node, fallback string) (string, bool) {
	text := ""
	if body := n.child("ac:plain-text-link-body"); body != nil {
		text = body.textContent()
	} else if body := n.child("ac:link-body"); body != nil {
		if len(body.elements()) > 0 {
			return "", false
		}
		text = body.textContent()
	}

	if strings.TrimSpace(text) == "" {
		text = fallback
	}

	if strings.ContainsAny(text, "\n\r") || (w.ctx.table && strings.Contains(text, "|")) {
		return "", false
	}

	escaped := strings.NewReplacer(`\[`, "[", `\]`, "]").Replace(escapeInline(text, inlineOptions{}))
	if escaped != text || strings.TrimSpace(text) != text {
		return "", false
	}

	depth := 0
	for _, r := range text {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
			if depth < 0 {
				return "", false
			}
		}
	}

	return text, depth == 0
}

func (w *inlineWriter) acLink(n *node) {
	if text, ok := w.acLinkMarkdown(n); ok {
		w.write(text)
		return
	}

	w.write(w.c.raw(n, rawInline))
}

// titleDestination matches a page title that can be written after "ac:" as it
// is.
var titleDestination = regexp.MustCompile(`^[^\s()<>\\]+$`)

// acLinkMarkdown writes an ac:link the way mark writes one in Markdown, if it
// can: to an anchor on the page, to a page in the same space by title, or to
// an attachment of the page.
func (w *inlineWriter) acLinkMarkdown(n *node) (string, bool) {
	anchor := n.attr("ac:anchor")
	for _, a := range n.attrs {
		if a.name != "ac:anchor" {
			return "", false
		}
	}

	var target *node
	for _, child := range n.elements() {
		if strings.HasPrefix(child.name, "ri:") {
			if target != nil {
				return "", false
			}
			target = child
		}
	}

	text, hasText := w.linkBody(n)

	switch {
	case target == nil:
		if anchor == "" || !hasText || strings.ContainsFunc(anchor, unicode.IsSpace) {
			return "", false
		}

		return "[" + text + "](" + destination("#"+anchor) + ")", true

	case target.is("ri:page"):
		title := target.attr("ri:content-title")
		if title == "" || strings.ContainsAny(title, "<>\n") || !plainAttrs(target, "ri:content-title", "ri:space-key", "ri:version-at-save") {
			return "", false
		}
		if space := target.attr("ri:space-key"); space != "" && space != w.c.opts.Space {
			return "", false
		}

		// mark reads the part after the last "#" as an anchor when it looks
		// like one, so a title that ends in such a part cannot be written.
		if at := strings.LastIndex(title, "#"); at > 0 && at < len(title)-1 &&
			!strings.ContainsFunc(title[at+1:], unicode.IsSpace) {
			return "", false
		}
		if strings.ContainsFunc(anchor, unicode.IsSpace) {
			return "", false
		}

		dest := "ac:" + title
		if anchor != "" {
			dest += "#" + anchor
		}
		if titleDestination.MatchString(dest) {
			dest = "(" + dest + ")"
		} else {
			dest = "(<" + dest + ">)"
		}

		text, ok := w.literalLinkText(n, title)
		if !ok {
			return "", false
		}

		return "[" + text + "]" + dest, true

	case target.is("ri:attachment"):
		if anchor != "" || len(target.elements()) > 0 || !plainAttrs(target, "ri:filename", "ri:version-at-save") {
			return "", false
		}

		filename := target.attr("ri:filename")
		if filename == "" {
			return "", false
		}
		text, ok := w.literalLinkText(n, filename)
		if !ok {
			return "", false
		}

		return "[" + text + "](" + pathDestination(w.c.useAttachment(filename, false)) + ")", true
	}

	return "", false
}

func (w *inlineWriter) image(n *node) {
	if text, ok := w.imageMarkdown(n, true); ok {
		w.write(text)
		return
	}

	w.write(w.c.raw(n, rawInline))
}

// imageAttrs are the attributes of ac:image publishing the image again sets
// or does not need: the dimensions mark reads from the file itself, and the
// placement Confluence's editor adds.
var imageAttrs = []string{
	"ac:alt", "ac:title", "ac:width", "ac:height", "ac:original-width", "ac:original-height",
	"ac:custom-width", "ac:align", "ac:layout", "ac:thumbnail", "ac:queryparams", "ac:src",
}

// imageMarkdown writes an image: as ![alt](path) where that is all it says,
// and as <img> -- which mark reads too -- where it is shown at a size of its
// own. use says whether to record the attachment it shows.
func (w *inlineWriter) imageMarkdown(n *node, use bool) (string, bool) {
	if !plainAttrs(n, imageAttrs...) {
		return "", false
	}

	var target *node
	for _, child := range n.elements() {
		if target != nil {
			return "", false
		}
		target = child
	}
	if target == nil {
		return "", false
	}

	var src string
	switch {
	case target.is("ri:attachment") && len(target.elements()) == 0 && plainAttrs(target, "ri:filename", "ri:version-at-save"):
		filename := target.attr("ri:filename")
		if filename == "" {
			return "", false
		}
		if use {
			src = pathDestination(w.c.useAttachment(filename, false))
		} else {
			src = pathDestination(w.c.attachmentPath(filename))
		}
	case target.is("ri:url") && plainAttrs(target, "ri:value"):
		src = target.attr("ri:value")
		if src == "" {
			return "", false
		}
	default:
		return "", false
	}

	alt := n.attr("ac:alt")
	title := n.attr("ac:title")
	width := n.attr("ac:width")
	height := n.attr("ac:height")

	sized := height != "" || (width != "" && width != n.attr("ac:original-width"))
	if sized {
		var b strings.Builder
		b.WriteString(`<img src="` + escapeAttr(src) + `"`)
		if width != "" {
			b.WriteString(` width="` + escapeAttr(width) + `"`)
		}
		if height != "" {
			b.WriteString(` height="` + escapeAttr(height) + `"`)
		}
		if alt != "" {
			b.WriteString(` alt="` + escapeAttr(alt) + `"`)
		}
		if title != "" {
			b.WriteString(` title="` + escapeAttr(title) + `"`)
		}
		b.WriteString(" />")

		return b.String(), true
	}

	return "![" + escapeInline(alt, inlineOptions{table: w.ctx.table}) + "](" + destination(src) + linkTitle(title) + ")", true
}

func anchorKey(s string) string {
	return transformer.AnchorKey(s)
}
