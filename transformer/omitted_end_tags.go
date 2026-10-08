package transformer

import (
	"bytes"

	"golang.org/x/net/html"
)

// openElement is an element closeOmittedEndTags has seen start and not end.
type openElement struct {
	name string // lower-cased, for matching
	raw  []byte // as written, for the end tag: XML is case-sensitive
}

// closeOmittedEndTags writes out the end tags HTML lets an author leave off --
// `<td>a<td>b</table>`, `<p>one<p>two`, `<li>a<li>b</ul>` -- so that the
// fragment is well-formed XML, and reports whether anything had to change.
//
// It is a token pass, not a parse: every token is copied through byte for byte
// and an end tag is only ever inserted, never moved or dropped. Storage-format
// markup is therefore left exactly as written; a namespaced element such as
// <ac:link> is never closed implicitly, and a self-closed <ri:page .../> opens
// nothing. Only the elements whose end tag HTML makes optional are closed, and
// only where HTML would close them: when an element that ends them starts, or
// when an element enclosing them ends. What is still open at the end of the
// fragment is left open, since a blank line splits an element across sibling
// fragments and its end tag may well be in the next one.
//
// The input must hold no CDATA section; the caller protects them first.
func closeOmittedEndTags(raw []byte) ([]byte, bool) {
	var (
		buf     bytes.Buffer
		stack   []openElement
		changed bool
	)

	// closeTo emits end tags for stack[i:] from the top down and pops them.
	closeTo := func(i int) {
		for j := len(stack) - 1; j >= i; j-- {
			buf.WriteString("</")
			buf.Write(stack[j].raw)
			buf.WriteString(">")
		}
		stack = stack[:i]
		changed = true
	}

	z := html.NewTokenizer(bytes.NewReader(raw))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}

		token := z.Raw()

		switch tt {
		case html.StartTagToken:
			nameBytes, _ := z.TagName()
			name := string(nameBytes)

			// Close what this start tag ends, repeatedly: <li> after
			// <li><p>x first ends the <p>, then the <li> it sat in.
			for {
				i := impliedEnd(name, stack)
				if i < 0 {
					break
				}
				closeTo(i)
			}

			if !isVoidElement(name) {
				stack = append(stack, openElement{name: name, raw: append([]byte(nil), tagNameAt(token)...)})
			}

		case html.EndTagToken:
			nameBytes, _ := z.TagName()
			name := string(nameBytes)

			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name != name {
					if optionalEnd(stack[i].name) {
						continue
					}
					// An element whose end tag is required is still open:
					// the author's nesting is wrong in a way HTML does not
					// define as an omission. Leave it as written.
					break
				}

				if i < len(stack)-1 {
					closeTo(i + 1)
				}
				stack = stack[:i]

				break
			}
		}

		buf.Write(token)
	}

	if !changed {
		return raw, false
	}

	return buf.Bytes(), true
}

// impliedEnd returns the index in stack of the element that a start tag named
// name ends, or -1. Elements whose end tag is optional may sit above it and are
// ended with it; anything else in the way means name does not end it.
func impliedEnd(name string, stack []openElement) int {
	for i := len(stack) - 1; i >= 0; i-- {
		top := stack[i].name
		if endsOn(top, name) {
			return i
		}
		if !optionalEnd(top) || scopeBoundary(top, name) {
			return -1
		}
	}

	return -1
}

// endsOn reports whether an open element named open is ended by a start tag
// named start, as the HTML specification's optional end tag rules have it.
func endsOn(open, start string) bool {
	switch open {
	case "p":
		return closesP(start)
	case "li":
		return start == "li"
	case "dt", "dd":
		return start == "dt" || start == "dd"
	case "td", "th":
		return start == "td" || start == "th" || start == "tr" ||
			start == "tbody" || start == "thead" || start == "tfoot"
	case "tr":
		return start == "tr" || start == "tbody" || start == "thead" || start == "tfoot"
	case "thead", "tbody", "tfoot":
		return start == "tbody" || start == "tfoot"
	case "option":
		return start == "option" || start == "optgroup"
	case "optgroup":
		return start == "optgroup"
	case "colgroup", "caption":
		return start == "thead" || start == "tbody" || start == "tfoot" || start == "tr" ||
			(open == "colgroup" && start == "colgroup")
	}

	return false
}

// scopeBoundary reports whether an open element with an optional end tag still
// stops the search for what start ends: a <p> starting in a table cell does not
// end a <li> the table sits in, and a <li> does not end the one around the list
// holding it.
func scopeBoundary(open, start string) bool {
	switch open {
	case "td", "th", "li", "dd", "dt", "caption":
		// Content of these may start anything; whatever is open outside them
		// belongs to a different scope.
		return !endsOn(open, start)
	}

	return false
}

// optionalEnd reports whether HTML lets the end tag of name be omitted.
func optionalEnd(name string) bool {
	switch name {
	case "p", "li", "dt", "dd", "td", "th", "tr", "thead", "tbody", "tfoot",
		"option", "optgroup", "colgroup", "caption":
		return true
	}

	return false
}

// closesP reports whether a start tag named name ends an open <p>.
func closesP(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "details", "dialog", "div",
		"dl", "fieldset", "figcaption", "figure", "footer", "form",
		"h1", "h2", "h3", "h4", "h5", "h6", "header", "hgroup", "hr", "main",
		"menu", "nav", "ol", "p", "pre", "search", "section", "summary", "table", "ul":
		return true
	}

	return false
}

// tagNameAt returns the element name of a start tag as it is written.
func tagNameAt(tag []byte) []byte {
	i := 1
	for i < len(tag) && !isTagSpace(tag[i]) && tag[i] != '/' && tag[i] != '>' {
		i++
	}

	return tag[1:i]
}
