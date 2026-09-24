// Package export turns a Confluence page back into a Markdown document that
// mark can publish again.
//
// Convert is the pure part: storage format in, Markdown out. Page does the
// rest -- reads a page, its labels, ancestry and attachments through the API,
// and writes the document and the files it refers to.
//
// What Markdown can say, and mark reads back into the same storage format, is
// written as Markdown. Everything else is written as the storage format itself:
// mark hands <ac:*> and <ri:*> markup through untouched, so a macro the
// converter knows nothing about still comes back as it was.
package export

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v16/transformer"
)

// Options configures Convert.
type Options struct {
	// Space is the key of the space the page is in. A link to a page in the
	// same space is written as mark's [text](ac:Title); one to a page in
	// another space, which that syntax cannot name, is kept as storage format.
	Space string

	// AttachmentPath maps the name of an attachment of the page to the path,
	// relative to the Markdown file and separated by slashes, that it is
	// written to. Nil keeps the name as it is.
	AttachmentPath func(filename string) string
}

// Document is a converted page body.
type Document struct {
	// Markdown is the body, ending in a newline, or empty for an empty page.
	Markdown string

	// Layout is "article" when the page is laid out the way mark's
	// <!-- Layout: article --> lays it out, and Sidebar the storage format of
	// the sidebar that layout has.
	Layout  string
	Sidebar string

	// Attachments are the attachments of the page the body refers to, by
	// name, in the order it first does.
	Attachments []string

	// Declared are the paths, as the document refers to them, of the
	// attachments that have to be named in an Attachment header to be
	// uploaded again: those referred to from storage format kept as it was,
	// which mark does not look inside.
	Declared []string
}

// Convert turns a page body in Confluence storage format into Markdown.
func Convert(storage string, opts Options) (*Document, error) {
	root, err := parseStorage(storage)
	if err != nil {
		return nil, err
	}

	c := &converter{
		opts:     opts,
		anchors:  map[string]bool{},
		used:     map[string]bool{},
		declared: map[string]bool{},
	}
	c.collectAnchors(root)

	doc := &Document{}

	children := root.children
	if body, sidebar, ok := articleLayout(root); ok {
		// The sidebar travels in a header, which cannot hold the end of the
		// comment it is written in.
		if text := strings.TrimSpace(c.rawChildren(sidebar, rawLine)); !strings.Contains(text, "-->") {
			doc.Layout = "article"
			doc.Sidebar = text
			children = body.children
		}
	}

	markdown := strings.TrimSpace(c.blocks(children, blockContext{root: true}))
	if markdown != "" {
		doc.Markdown = markdown + "\n"
	}

	doc.Attachments = c.order
	for _, name := range c.order {
		if c.declared[name] {
			doc.Declared = append(doc.Declared, c.attachmentPath(name))
		}
	}

	return doc, nil
}

type converter struct {
	opts Options

	// anchors are the targets of the page's links to itself, by AnchorKey.
	anchors map[string]bool

	order    []string
	used     map[string]bool
	declared map[string]bool
}

// collectAnchors records every anchor a link on the page points at. mark puts
// an anchor macro into a heading only when a link on the page points at it,
// so one the page's own links account for is left for mark to put back.
func (c *converter) collectAnchors(n *node) {
	if n.is("ac:link") {
		if anchor := n.attr("ac:anchor"); anchor != "" && n.child("ri:page") == nil {
			c.anchors[transformer.AnchorKey(anchor)] = true
		}
	}

	for _, child := range n.children {
		c.collectAnchors(child)
	}
}

// useAttachment records that the body refers to an attachment and returns the
// path it is written to. declare says the reference is one mark will not see.
func (c *converter) useAttachment(filename string, declare bool) string {
	if !c.used[filename] {
		c.used[filename] = true
		c.order = append(c.order, filename)
	}
	if declare {
		c.declared[filename] = true
	}

	return c.attachmentPath(filename)
}

func (c *converter) attachmentPath(filename string) string {
	if c.opts.AttachmentPath == nil {
		return filename
	}

	return c.opts.AttachmentPath(filename)
}

// articleLayout recognises the layout mark wraps a page in for
// <!-- Layout: article -->: a single section with the page in one cell and the
// sidebar in the other.
func articleLayout(root *node) (body, sidebar *node, ok bool) {
	var layout *node
	for _, child := range root.children {
		switch {
		case child.isBlank():
		case child.is("ac:layout") && layout == nil:
			layout = child
		default:
			return nil, nil, false
		}
	}

	if layout == nil || len(layout.attrs) > 0 {
		return nil, nil, false
	}

	sections := significant(layout.children)
	if len(sections) != 1 || !sections[0].is("ac:layout-section") ||
		len(sections[0].attrs) != 1 || sections[0].attr("ac:type") != "two_right_sidebar" {
		return nil, nil, false
	}

	cells := significant(sections[0].children)
	if len(cells) != 2 || !cells[0].is("ac:layout-cell") || !cells[1].is("ac:layout-cell") ||
		len(cells[0].attrs) > 0 || len(cells[1].attrs) > 0 {
		return nil, nil, false
	}

	return cells[0], cells[1], true
}

// significant drops the whitespace between elements.
func significant(nodes []*node) []*node {
	var out []*node
	for _, n := range nodes {
		if !n.isBlank() {
			out = append(out, n)
		}
	}

	return out
}

// fence returns a run of backticks long enough to hold text, which is at least
// three and longer than any run inside it.
func fence(text string, minimum int) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}

	return strings.Repeat("`", max(minimum, longest+1))
}

// headerValue checks that a value can be written in a metadata header, which
// is a single line inside a comment.
func headerValue(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "\r\n") || strings.Contains(value, "-->") {
		return "", fmt.Errorf("the %s %q cannot be written in a metadata header", strings.ToLower(name), value)
	}

	return value, nil
}
