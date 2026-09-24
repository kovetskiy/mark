package export

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// node is one piece of a page's storage format: an element, or the text inside
// one.
//
// encoding/xml is used rather than an HTML parser because storage format is
// XML: an HTML5 parser does not honour a self-closing <ri:page/>, and reads
// everything after it as its content.
type node struct {
	kind     nodeKind
	name     string // "p", "ac:structured-macro"; empty for text
	attrs    []attr
	text     string
	children []*node
	parent   *node
}

type nodeKind int

const (
	elementNode nodeKind = iota
	textNode
)

type attr struct {
	name  string
	value string
}

// parseStorage reads a page body in storage format.
//
// A body is a fragment -- any number of top-level elements -- and it uses the
// ac: and ri: prefixes without declaring them, so it is parsed inside a root
// element of its own and each name is kept as its prefix and local part. HTML
// entities (&nbsp;, &rsquo;) are storage format's own and are resolved.
func parseStorage(body string) (*node, error) {
	root, err := decodeStorage(body, true)
	if err == nil {
		return root, nil
	}

	// Storage format is supposed to be XHTML, but Confluence has been seen to
	// hand back a stray <br> or an unescaped ampersand in old pages. The
	// lenient decoder reads those the way a browser would; only if that fails
	// too is the page refused.
	lenient, lenientErr := decodeStorage(body, false)
	if lenientErr != nil {
		return nil, fmt.Errorf("unable to parse the page's storage format: %w", err)
	}

	return lenient, nil
}

const storageRoot = "mark-export-root"

func decodeStorage(body string, strict bool) (*node, error) {
	decoder := xml.NewDecoder(strings.NewReader("<" + storageRoot + ">" + body + "</" + storageRoot + ">"))
	decoder.Strict = strict
	decoder.Entity = xml.HTMLEntity
	if !strict {
		decoder.AutoClose = xml.HTMLAutoClose
	}

	root := &node{kind: elementNode, name: storageRoot}
	current := root
	opened := false

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := token.(type) {
		case xml.StartElement:
			if !opened {
				opened = true
				continue
			}

			element := &node{kind: elementNode, name: qualifiedName(t.Name), parent: current}
			for _, a := range t.Attr {
				element.attrs = append(element.attrs, attr{name: qualifiedName(a.Name), value: a.Value})
			}
			current.children = append(current.children, element)
			current = element

		case xml.EndElement:
			if current.parent == nil {
				// The root's own end.
				continue
			}
			current = current.parent

		case xml.CharData:
			text := string(t)
			// Adjacent text -- a CDATA section next to ordinary text, or a
			// resolved entity -- is one run of text as far as the page is
			// concerned.
			if last := lastChild(current); last != nil && last.kind == textNode {
				last.text += text
				continue
			}
			current.children = append(current.children, &node{kind: textNode, text: text, parent: current})
		}
	}

	return root, nil
}

func qualifiedName(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}

	return name.Space + ":" + name.Local
}

func lastChild(n *node) *node {
	if len(n.children) == 0 {
		return nil
	}

	return n.children[len(n.children)-1]
}

// attr returns the value of the named attribute, or "".
func (n *node) attr(name string) string {
	for _, a := range n.attrs {
		if a.name == name {
			return a.value
		}
	}

	return ""
}

func (n *node) is(names ...string) bool {
	if n.kind != elementNode {
		return false
	}

	for _, name := range names {
		if n.name == name {
			return true
		}
	}

	return false
}

// child returns the first child element with the given name.
func (n *node) child(name string) *node {
	for _, c := range n.children {
		if c.is(name) {
			return c
		}
	}

	return nil
}

// elements returns the child elements, skipping text.
func (n *node) elements() []*node {
	var out []*node
	for _, c := range n.children {
		if c.kind == elementNode {
			out = append(out, c)
		}
	}

	return out
}

// textContent is all the text inside the node, as a reader sees it.
func (n *node) textContent() string {
	if n.kind == textNode {
		return n.text
	}

	var b strings.Builder
	for _, c := range n.children {
		b.WriteString(c.textContent())
	}

	return b.String()
}

// isBlank reports a text node holding nothing but whitespace.
func (n *node) isBlank() bool {
	return n.kind == textNode && strings.TrimSpace(n.text) == ""
}

// macroName is the ac:name of a structured macro.
func (n *node) macroName() string {
	return n.attr("ac:name")
}

// parameters returns a structured macro's parameters by name, and whether it
// has any it names twice or whose value is markup rather than text.
func (n *node) parameters() (map[string]string, bool) {
	params := map[string]string{}
	plain := true

	for _, c := range n.elements() {
		if !c.is("ac:parameter") {
			continue
		}

		name := c.attr("ac:name")
		if _, seen := params[name]; seen {
			plain = false
		}
		for _, v := range c.children {
			if v.kind == elementNode {
				plain = false
			}
		}
		params[name] = c.textContent()
	}

	return params, plain
}
