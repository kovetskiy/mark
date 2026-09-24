package export

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// blockContext is where a run of blocks is written.
type blockContext struct {
	// root is the top level of the Markdown document -- including the bodies
	// written between the tags of a macro kept as storage format, and the
	// cells of a layout, which are top level too as far as Markdown is
	// concerned. GitHub alerts and layout markers only work there.
	root bool
}

// blockElements are the elements that stand on their own rather than inside a
// paragraph.
var blockElements = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "table": true, "blockquote": true, "pre": true, "hr": true,
	"div": true, "section": true, "dl": true, "figure": true, "center": true, "address": true,
	"ac:task-list": true, "ac:layout": true, "ac:layout-section": true, "ac:layout-cell": true,
	"ac:adf-extension": true,
}

func isBlock(n *node) bool {
	if n.kind != elementNode {
		return false
	}

	if n.is("ac:structured-macro") {
		return n.child("ac:rich-text-body") != nil || n.child("ac:plain-text-body") != nil
	}

	return blockElements[n.name]
}

// block is one rendered block, and what kind of list it is, if one.
type block struct {
	text string
	// list is "-" or "*" for a bullet or task list, "." or ")" for an
	// ordered one, with the marker it was written with; "" otherwise.
	list string
	// paragraph is prose, which a tight list item may hold next to a
	// nested list without a blank line.
	paragraph bool
}

// blocks renders a run of nodes as Markdown blocks separated by blank lines.
func (c *converter) blocks(nodes []*node, ctx blockContext) string {
	rendered := c.renderBlocks(nodes, ctx)

	texts := make([]string, 0, len(rendered))
	for _, b := range rendered {
		texts = append(texts, b.text)
	}

	return strings.Join(texts, "\n\n")
}

func (c *converter) renderBlocks(nodes []*node, ctx blockContext) []block {
	var out []block
	var run []*node

	flush := func() {
		if len(run) == 0 {
			return
		}
		if text := c.bareInline(run); text != "" {
			out = append(out, block{text: text, paragraph: true})
		}
		run = nil
	}

	for _, n := range nodes {
		if !isBlock(n) {
			run = append(run, n)
			continue
		}

		flush()

		// Two lists in a row are one list to Markdown unless they are
		// written with different markers.
		previous := ""
		if len(out) > 0 {
			previous = out[len(out)-1].list
		}

		if b := c.block(n, ctx, previous); b.text != "" {
			out = append(out, b)
		}
	}

	flush()

	return out
}

func (c *converter) block(n *node, ctx blockContext, previousList string) block {
	switch n.name {
	case "p":
		return block{text: c.paragraphElement(n), paragraph: true}

	case "h1", "h2", "h3", "h4", "h5", "h6":
		return block{text: c.heading(n)}

	case "ul":
		marker := "-"
		if previousList == "-" {
			marker = "*"
		}
		return block{text: c.list(n, marker, false), list: marker}

	case "ac:task-list":
		marker := "-"
		if previousList == "-" {
			marker = "*"
		}
		return block{text: c.taskList(n, marker), list: marker}

	case "ol":
		delimiter := "."
		if previousList == "." {
			delimiter = ")"
		}
		return block{text: c.list(n, delimiter, true), list: delimiter}

	case "blockquote":
		return block{text: c.blockquote(n)}

	case "pre":
		return block{text: codeBlock("", n.textContent())}

	case "hr":
		return block{text: "---"}

	case "table":
		return block{text: c.table(n)}

	case "ac:structured-macro":
		return block{text: c.macro(n, ctx)}

	case "ac:layout":
		return block{text: c.layout(n, ctx)}

	case "ac:layout-section", "ac:layout-cell":
		return block{text: c.container(n, n, ctx)}
	}

	if strings.Contains(n.name, ":") {
		return block{text: c.raw(n, rawInline)}
	}

	return block{text: c.htmlBlock(n)}
}

// htmlBlock writes an element as an HTML block, which goldmark passes through
// as it is.
//
// A block opens with a line that names one of the HTML block elements
// CommonMark knows, and goes on to the next blank line; raw never writes one.
func (c *converter) htmlBlock(n *node) string {
	return c.raw(n, rawBlock)
}

// paragraphElement renders a <p>.
//
// A paragraph that is a single element written out as storage format has to
// be kept as HTML: mark publishes a paragraph holding nothing but markup
// without a <p> around it, which is how a macro on its own line reaches the
// page as the macro itself. So is one whose attributes -- an alignment, say --
// Markdown has no way of saying.
func (c *converter) paragraphElement(n *node) string {
	for _, a := range n.attrs {
		if !ignorableAttr(a.name) {
			return c.htmlBlock(n)
		}
	}

	parts := significant(n.children)
	if len(parts) == 1 && parts[0].kind == elementNode && c.rendersRaw(parts[0]) {
		return c.htmlBlock(n)
	}

	if holdsHTMLOnly(n) {
		return c.htmlBlock(n)
	}

	return c.paragraph(n.children)
}

// htmlOnly are HTML elements storage format has no place for, which reach a
// page only by being published as written. A paragraph holding one is kept
// that way.
var htmlOnly = map[string]bool{
	"img": true, "picture": true, "source": true, "video": true, "audio": true,
	"iframe": true, "input": true, "svg": true, "object": true, "embed": true,
}

func holdsHTMLOnly(n *node) bool {
	for _, child := range n.children {
		if child.kind == elementNode && (htmlOnly[child.name] || holdsHTMLOnly(child)) {
			return true
		}
	}

	return false
}

// bareInline renders inline content that stands among blocks without a <p>
// around it -- markup mark itself publishes that way, a <b> or a macro on a
// line of its own. A single element is written as markup, which mark
// publishes without a <p> again; anything else becomes a paragraph.
func (c *converter) bareInline(run []*node) string {
	parts := significant(run)
	if len(parts) == 1 && parts[0].kind == elementNode && !parts[0].is("a", "ac:link", "ac:image", "br") {
		w := c.inline(inlineContext{})
		w.raw(parts[0])
		return strings.TrimSpace(w.finish())
	}

	return c.paragraph(run)
}

// ignorableAttr are attributes an element carries that publishing it again
// has no need of: identifiers the editor assigns, and its own bookkeeping.
func ignorableAttr(name string) bool {
	return name == "local-id" || name == "id" || name == "class" || strings.HasPrefix(name, "data-")
}

// paragraph renders a run of inline nodes as a paragraph.
func (c *converter) paragraph(nodes []*node) string {
	w := c.inline(inlineContext{})
	w.nodes(nodes)

	return escapeLineStarts(strings.TrimSpace(w.finish()))
}

// heading renders h1 to h6.
func (c *converter) heading(n *node) string {
	level, _ := strconv.Atoi(n.name[1:])

	// mark puts an anchor macro at the front of a heading when a link on the
	// page points at it, named after the heading or after the id a {#id}
	// gives it. One the page's own links account for is left for mark to put
	// back.
	children := n.children
	customID := ""
	if anchor := leadingAnchor(n); anchor != nil {
		params, _ := anchor.parameters()
		name := params[""]
		if c.anchors[anchorKey(name)] {
			if anchorKey(name) != anchorKey(headingText(n, anchor)) {
				customID = name
			}
			if customID == "" || customHeadingID.MatchString(customID) {
				children = without(children, anchor)
			} else {
				customID = ""
			}
		}
	}

	w := c.inline(inlineContext{singleLine: true})
	w.nodes(children)
	text := strings.TrimSpace(w.finish())
	if text == "" {
		return ""
	}

	// A closing sequence of #s would be read as the end of the heading, and
	// a closing brace as the end of an id.
	if strings.HasSuffix(text, "#") {
		text = text[:len(text)-1] + "\\#"
	}
	if strings.HasSuffix(text, "}") {
		text = text[:len(text)-1] + "\\}"
	}

	if customID != "" {
		text += " {#" + customID + "}"
	}

	return strings.Repeat("#", level) + " " + text
}

// customHeadingID is an id a heading can be given with {#id}.
var customHeadingID = regexp.MustCompile(`^[^\s{}#]+$`)

// leadingAnchor is the anchor macro mark puts at the front of a heading that a
// link on the page points at.
func leadingAnchor(heading *node) *node {
	for _, child := range heading.children {
		if child.isBlank() {
			continue
		}
		if child.is("ac:structured-macro") && child.macroName() == "anchor" {
			return child
		}
		return nil
	}

	return nil
}

func headingText(heading, skip *node) string {
	var b strings.Builder
	for _, child := range heading.children {
		if child != skip {
			b.WriteString(child.textContent())
		}
	}

	return strings.TrimSpace(b.String())
}

func without(nodes []*node, skip *node) []*node {
	out := make([]*node, 0, len(nodes))
	for _, n := range nodes {
		if n != skip {
			out = append(out, n)
		}
	}

	return out
}

// list renders ul and ol.
func (c *converter) list(n *node, marker string, ordered bool) string {
	var items []*node
	for _, child := range n.elements() {
		if child.is("li") {
			items = append(items, child)
		}
	}

	start := 1
	if value, err := strconv.Atoi(n.attr("start")); ordered && err == nil && value >= 0 {
		start = value
	}

	loose := false
	for _, item := range items {
		if item.child("p") != nil {
			loose = true
		}
	}

	rendered := make([]string, 0, len(items))
	for i, item := range items {
		prefix := marker
		if ordered {
			prefix = strconv.Itoa(start+i) + marker
		}
		rendered = append(rendered, listItem(prefix, c.itemBlocks(item.children, loose)))
	}

	if loose {
		return strings.Join(rendered, "\n\n")
	}

	return strings.Join(rendered, "\n")
}

// taskList renders an ac:task-list as a GFM task list.
func (c *converter) taskList(n *node, marker string) string {
	var rendered []string

	for _, task := range n.elements() {
		if !task.is("ac:task") {
			continue
		}

		box := "[ ]"
		if status := task.child("ac:task-status"); status != nil && strings.TrimSpace(status.textContent()) == "complete" {
			box = "[x]"
		}

		var content string
		if body := task.child("ac:task-body"); body != nil {
			content = c.itemBlocks(body.children, false)
		}

		rendered = append(rendered, listItem(marker+" "+box, content))
	}

	return strings.Join(rendered, "\n")
}

// itemBlocks renders what a list item holds. In a tight list, prose and a
// nested list sit on adjacent lines; anything else needs a blank line, which
// makes the list loose, but is rare enough in a tight one to be worth it.
func (c *converter) itemBlocks(nodes []*node, loose bool) string {
	rendered := c.renderBlocks(nodes, blockContext{})

	var b strings.Builder
	for i, current := range rendered {
		if i > 0 {
			previous := rendered[i-1]
			if !loose && ((previous.paragraph && current.list != "") || (previous.list != "" && current.list != "")) {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(current.text)
	}

	return b.String()
}

// listItem writes content after a list marker, with the lines after the first
// indented to belong to the item.
func listItem(marker, content string) string {
	if content == "" {
		return marker
	}

	indent := strings.Repeat(" ", len(marker)+1)
	lines := strings.Split(content, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}

	return marker + " " + strings.Join(lines, "\n")
}

// quoteMarker is what makes mark publish a blockquote as an info, note, tip or
// warning macro: the words renderer/blockquote.go looks for, or a GitHub
// alert.
var quoteMarker = regexp.MustCompile(
	`^(?i)(?:(?:info|infos|note|notes|warn|warns|warning|warnings|tip|tips)\b[[:punct:]]*(?:\s|$)|\[!)`,
)

// blockquote renders a blockquote -- as HTML when it opens with a word mark
// would publish it as a macro for.
func (c *converter) blockquote(n *node) string {
	if quoteMarker.MatchString(strings.TrimSpace(n.textContent())) {
		return c.htmlBlock(n)
	}

	inner := c.blocks(n.children, blockContext{})
	if inner == "" {
		return ""
	}

	return quote(inner)
}

func quote(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + line
		}
	}

	return strings.Join(lines, "\n")
}

// codeBlock writes a fenced code block.
//
// mark takes the newline that ends the last line off what the block holds, so
// one the code itself ends with is kept by writing it out as an empty line.
func codeBlock(info, text string) string {
	f := fence(text+info, 3)

	return f + info + "\n" + text + "\n" + f
}

// container writes an element holding blocks as its opening tag, the blocks
// as Markdown, and its closing tag, each separated by a blank line.
//
// mark publishes a paragraph that is only the opening or the closing half of a
// Confluence element as the tag itself, without a <p>, which is what lets the
// content between them be Markdown. open is the element whose children are the
// content; everything of outer around it is written as it was.
//
// A body holding only inline content is written as it is, since Markdown
// would put it in a paragraph.
func (c *converter) container(outer, body *node, ctx blockContext) string {
	hasBlocks := false
	for _, child := range body.children {
		if isBlock(child) {
			hasBlocks = true
		}
	}

	inner := c.blocks(body.children, ctx)
	if inner == "" || !hasBlocks {
		return c.raw(outer, rawInline)
	}

	var before, after strings.Builder
	before.WriteString(c.openTag(withChildren(outer)))

	if outer != body {
		write := &before
		for _, child := range outer.children {
			if child == body {
				before.WriteString(c.openTag(withChildren(body)))
				after.WriteString("</" + body.name + ">")
				write = &after
				continue
			}
			c.writeRaw(write, child, rawInline)
		}
	}

	after.WriteString("</" + outer.name + ">")

	return before.String() + "\n\n" + inner + "\n\n" + after.String()
}

// withChildren is n as openTag sees an element that has content, so that the
// tag it writes is not self-closed.
func withChildren(n *node) *node {
	if len(n.children) > 0 {
		return n
	}

	cp := *n
	cp.children = []*node{{kind: textNode}}

	return &cp
}

// layoutTypes are the section types mark's layout markers can name.
var layoutTypes = map[string]bool{
	"single": true, "two_equal": true, "two_left_sidebar": true, "two_right_sidebar": true,
	"three": true, "three_with_sidebars": true,
}

// layout writes a page layout with mark's layout markers, which only work at
// the top level of a document; elsewhere, and for a section the markers have
// no name for, the tags themselves are written instead.
func (c *converter) layout(n *node, ctx blockContext) string {
	if !ctx.root || len(n.attrs) > 0 {
		return c.container(n, n, ctx)
	}

	var sections []string
	for _, section := range significant(n.children) {
		if !section.is("ac:layout-section") || len(section.attrs) != 1 || !layoutTypes[section.attr("ac:type")] {
			return c.container(n, n, ctx)
		}

		var cells []string
		for _, cell := range significant(section.children) {
			if !cell.is("ac:layout-cell") || len(cell.attrs) > 0 {
				return c.container(n, n, ctx)
			}

			content := c.blocks(cell.children, ctx)
			if content == "" {
				cells = append(cells, "<!-- ac:layout-cell -->\n<!-- ac:layout-cell end -->")
			} else {
				cells = append(cells, "<!-- ac:layout-cell -->\n\n"+content+"\n\n<!-- ac:layout-cell end -->")
			}
		}

		sections = append(sections, fmt.Sprintf("<!-- ac:layout-section type:%s -->\n%s\n<!-- ac:layout-section end -->",
			section.attr("ac:type"), strings.Join(cells, "\n")))
	}

	return "<!-- ac:layout -->\n\n" + strings.Join(sections, "\n\n") + "\n\n<!-- ac:layout end -->"
}
