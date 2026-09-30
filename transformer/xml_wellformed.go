package transformer

import (
	"bytes"
	"encoding/xml"
	stdhtml "html"

	"github.com/kovetskiy/mark/v16/stdlib"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
)

// XMLWellFormedTransformer rewrites the raw HTML an author wrote so that it is
// well-formed XML, which is what Confluence storage format is.
//
// Several shapes of hand-written HTML are legal in Markdown and rejected by
// Confluence, and it rejects the whole page rather than the element:
//
//   - a void element left open, `<br>` rather than `<br />`. Writing `<br>` in
//     a table cell is the standard way to get more than one line into one, so
//     this took out pages that were otherwise plain Markdown.
//   - an HTML comment containing `--`, which XML does not allow anywhere in a
//     comment body.
//   - a bare `&`, which HTML reads as itself when it starts no reference:
//     `<a href="https://x.com/?a=1&b=2">` or `<div>AT&T</div>`.
//   - an attribute value written without quotes, `<p align=center>`, or with
//     no value at all, `<td nowrap>`.
//
// Only the fragments that need it are rewritten; a node whose bytes are already
// well-formed is left exactly as the author wrote it.
type XMLWellFormedTransformer struct{}

// NewXMLWellFormedTransformer creates a new instance of XMLWellFormedTransformer.
func NewXMLWellFormedTransformer() *XMLWellFormedTransformer {
	return &XMLWellFormedTransformer{}
}

// Transform implements the parser.ASTTransformer interface.
func (t *XMLWellFormedTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	// Collect first, mutate after: replacing a node clears its NextSibling and
	// ast.Walk is iterating over that pointer, so mutating mid-walk abandons
	// every remaining sibling under the same parent.
	var nodes []ast.Node

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch node.(type) {
		case *ast.HTMLBlock, *ast.RawHTML:
			nodes = append(nodes, node)

		case *ast.Text:
			// A transformer that ran earlier -- Details, Layout -- replaces the
			// HTML block it rewrote with a Text node carrying the result, so by
			// the time this one walks the tree those bytes are no longer in a
			// node this switch would otherwise recognise. They are still the
			// author's markup and still have to be well-formed.
			if _, ok := node.Attribute(replacementContent); ok {
				nodes = append(nodes, node)
			}
		}

		return ast.WalkContinue, nil
	})

	source := reader.Source()

	for _, node := range nodes {
		// Repaired in place: the bytes are held on the attribute rather than in
		// the source, and the node is already what the renderer expects.
		if existing, ok := node.Attribute(replacementContent); ok {
			raw, ok := existing.([]byte)
			if !ok {
				continue
			}

			if fixed, changed := wellFormedHTML(raw); changed {
				node.SetAttribute(replacementContent, fixed)
			}

			continue
		}

		parent := node.Parent()
		if parent == nil {
			continue
		}

		fixed, changed := wellFormedHTML(ExtractNodeRawContent(node, source))
		if !changed {
			continue
		}

		// SetCode, because the replacement is storage format already: a plain
		// or "raw" string goes through Writer.RawWrite, which would escape the
		// very markup being repaired.
		replacement := ast.NewString(fixed)
		replacement.SetCode(true)

		parent.InsertBefore(parent, node, replacement)
		parent.RemoveChild(parent, node)
	}
}

var (
	cdataOpen  = []byte("<![CDATA[")
	cdataClose = []byte("]]>")

	// replacementContent is the attribute an earlier transformer leaves its
	// rewritten markup on. See renderer/text.go, which writes it out as-is.
	replacementContent = []byte("replacement-content")
)

// wellFormedHTML returns raw with void elements closed and comment bodies made
// legal, and reports whether anything had to change. Every other byte, tag and
// attribute is passed through untouched -- the input is the author's markup,
// not something to normalise.
//
// CDATA sections are cut out and copied through before any of that happens.
// html.NewTokenizer has no notion of CDATA: it reports "<![CDATA[" as a bogus
// comment that ends at the first ">", and then reads everything after it as
// ordinary markup -- so a "<br>" inside a code sample was rewritten to "<br />"
// and the sample silently changed, which is the one thing CDATA is there to
// prevent.
func wellFormedHTML(raw []byte) ([]byte, bool) {
	if len(raw) == 0 {
		return raw, false
	}

	if !bytes.Contains(raw, cdataOpen) {
		return wellFormedMarkup(raw)
	}

	var buf bytes.Buffer
	changed := false
	rest := raw

	for len(rest) > 0 {
		start := bytes.Index(rest, cdataOpen)
		if start == -1 {
			out, outChanged := wellFormedMarkup(rest)
			buf.Write(out)
			changed = changed || outChanged

			break
		}

		out, outChanged := wellFormedMarkup(rest[:start])
		buf.Write(out)
		changed = changed || outChanged

		end := bytes.Index(rest[start:], cdataClose)
		if end == -1 {
			// Unterminated. Everything left is inside the section as far as
			// anything downstream can tell, so it is copied out as written.
			buf.Write(rest[start:])

			break
		}

		stop := start + end + len(cdataClose)
		buf.Write(rest[start:stop])
		rest = rest[stop:]
	}

	if !changed {
		return raw, false
	}

	return buf.Bytes(), true
}

// wellFormedMarkup does the work on a span known to hold no CDATA section.
func wellFormedMarkup(raw []byte) ([]byte, bool) {
	if len(raw) == 0 {
		return raw, false
	}

	var buf bytes.Buffer
	changed := false

	tokenizer := html.NewTokenizer(bytes.NewReader(raw))

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}

		// Raw is only valid until the next call to Next, and every token's Raw
		// concatenated is the input, so writing it is a faithful passthrough.
		token := tokenizer.Raw()

		switch tokenType {
		case html.StartTagToken, html.SelfClosingTagToken:
			tag, tagChanged := wellFormedTag(token)
			changed = changed || tagChanged

			name, _ := tokenizer.TagName()
			if tokenType == html.StartTagToken && isVoidElement(string(name)) && bytes.HasSuffix(tag, []byte(">")) {
				buf.Write(tag[:len(tag)-1])
				buf.WriteString(" />")
				changed = true
				continue
			}

			buf.Write(tag)

			continue

		case html.TextToken:
			text, textChanged := escapeBareAmpersands(token)
			if textChanged {
				buf.Write(text)
				changed = true
				continue
			}

		case html.CommentToken:
			// A bogus comment is how the tokenizer reports <![CDATA[...]]>,
			// which stdlib templates emit on purpose and which is not a
			// comment at all.
			if bytes.HasPrefix(token, []byte("<!--")) && bytes.HasSuffix(token, []byte("-->")) && len(token) >= 7 {
				body, bodyChanged := wellFormedCommentBody(token[4 : len(token)-3])
				if bodyChanged {
					buf.WriteString("<!--")
					buf.Write(body)
					buf.WriteString("-->")
					changed = true
					continue
				}
			}
		}

		buf.Write(token)
	}

	if !changed {
		return raw, false
	}

	return buf.Bytes(), true
}

// wellFormedCommentBody removes the runs of hyphens that XML forbids inside a
// comment: `--` anywhere in the body, and a trailing `-` that would run into
// the closing `-->`. A comment is invisible on the page, so collapsing the
// hyphens costs the author nothing and keeps the page publishable.
func wellFormedCommentBody(body []byte) ([]byte, bool) {
	var buf bytes.Buffer

	hyphens := 0
	for _, c := range body {
		if c == '-' {
			hyphens++
			if hyphens > 1 {
				continue
			}
		} else {
			hyphens = 0
		}
		buf.WriteByte(c)
	}

	clean := bytes.TrimRight(buf.Bytes(), "-")
	if bytes.Equal(clean, body) {
		return body, false
	}

	return clean, true
}

// wellFormedTag returns a start tag with every attribute given a quoted value,
// and reports whether anything had to change. HTML allows a value without
// quotes, `align=center`, and an attribute with no value at all, `nowrap`;
// XML allows neither. A value already quoted keeps its quotes and has only what
// XML forbids inside one escaped: a bare `&`, and `<`.
//
// The tag is read from its bytes rather than rebuilt from the tokenizer's
// attributes, which come back decoded and lower-cased: `ac:name` and
// `&nbsp;` would not survive the round trip as the author wrote them.
func wellFormedTag(tag []byte) ([]byte, bool) {
	if len(tag) < 2 || tag[0] != '<' || tag[len(tag)-1] != '>' {
		return tag, false
	}

	// The tag name, up to the first space, slash or the end.
	i := 1
	for i < len(tag) && !isTagSpace(tag[i]) && tag[i] != '/' && tag[i] != '>' {
		i++
	}

	var buf bytes.Buffer
	buf.Write(tag[:i])
	changed := false

	for i < len(tag) {
		c := tag[i]

		// Space between attributes, the closing ">" or "/>", and a stray
		// "/", which HTML reads as space, are copied as they are.
		if isTagSpace(c) || c == '/' || c == '>' {
			buf.WriteByte(c)
			i++

			continue
		}

		start := i
		for i < len(tag) && !isTagSpace(tag[i]) && tag[i] != '/' && tag[i] != '>' && tag[i] != '=' {
			i++
		}
		// HTML takes a "=" that opens a name as part of it; there is no
		// spelling of that in XML, so it is left for the check to report.
		if i == start {
			i++
		}
		name := tag[start:i]
		buf.Write(name)

		// Space may stand either side of the "=", in both languages.
		j := i
		for j < len(tag) && isTagSpace(tag[j]) {
			j++
		}

		if j >= len(tag) || tag[j] != '=' {
			// No value: XHTML's spelling of a boolean attribute is its own
			// name, which HTML reads the same way as the bare name.
			buf.WriteString(`="`)
			buf.Write(escapeAttributeValue(name, '"'))
			buf.WriteByte('"')
			changed = true

			continue
		}

		j++
		for j < len(tag) && isTagSpace(tag[j]) {
			j++
		}
		buf.Write(tag[i:j])
		i = j

		if i < len(tag) && (tag[i] == '"' || tag[i] == '\'') {
			quote := tag[i]
			end := bytes.IndexByte(tag[i+1:], quote)
			if end == -1 {
				// Unterminated; what is left is not a tag XML can read
				// however it is patched, so it goes out as written.
				buf.Write(tag[i:])

				break
			}

			value := tag[i+1 : i+1+end]
			escaped := escapeAttributeValue(value, quote)
			changed = changed || !bytes.Equal(escaped, value)

			buf.WriteByte(quote)
			buf.Write(escaped)
			buf.WriteByte(quote)
			i += end + 2

			continue
		}

		// Unquoted: the value runs to the next space or the end of the tag.
		start = i
		for i < len(tag) && !isTagSpace(tag[i]) && tag[i] != '>' {
			i++
		}

		buf.WriteByte('"')
		buf.Write(escapeAttributeValue(tag[start:i], '"'))
		buf.WriteByte('"')
		changed = true
	}

	if !changed {
		return tag, false
	}

	return buf.Bytes(), true
}

// escapeAttributeValue escapes what XML does not allow in an attribute value
// quoted with quote: a bare `&`, a `<`, and the quote itself.
func escapeAttributeValue(value []byte, quote byte) []byte {
	value, _ = escapeBareAmpersands(value)

	if bytes.IndexByte(value, '<') == -1 && bytes.IndexByte(value, quote) == -1 {
		return value
	}

	escapedQuote := "&quot;"
	if quote == '\'' {
		escapedQuote = "&apos;"
	}

	var buf bytes.Buffer
	for _, c := range value {
		switch c {
		case '<':
			buf.WriteString("&lt;")
		case quote:
			buf.WriteString(escapedQuote)
		default:
			buf.WriteByte(c)
		}
	}

	return buf.Bytes()
}

// escapeBareAmpersands returns raw with every `&` that does not begin a
// reference XML can read written as `&amp;`, and reports whether anything had
// to change.
//
// A numeric reference is left as it is, whatever it names: one to a character
// XML has no spelling for is replaced by the pass over the whole compiled body,
// as the same reference written anywhere else on the page is. A named one is
// left when it is a name Confluence accepts; one only HTML5 knows is replaced by
// the character it names, which is what a browser would have shown.
func escapeBareAmpersands(raw []byte) ([]byte, bool) {
	if bytes.IndexByte(raw, '&') == -1 {
		return raw, false
	}

	var buf bytes.Buffer
	changed := false

	for i := 0; i < len(raw); i++ {
		if raw[i] != '&' {
			buf.WriteByte(raw[i])

			continue
		}

		size, keep := referenceAt(raw[i:])
		switch {
		case keep:
			buf.Write(raw[i : i+size])
		case size > 0:
			buf.WriteString(stdlib.XMLEscape(stdhtml.UnescapeString(string(raw[i : i+size]))))
			changed = true
		default:
			buf.WriteString("&amp;")
			changed = true

			continue
		}

		i += size - 1
	}

	if !changed {
		return raw, false
	}

	return buf.Bytes(), true
}

// referenceAt reports how long the reference s opens with is, and whether it is
// one to keep as written. A size with keep false is a named reference HTML
// knows and XML does not; a size of 0 means s opens with no reference at all.
func referenceAt(s []byte) (int, bool) {
	end := bytes.IndexByte(s, ';')
	if len(s) < 3 || s[0] != '&' || end < 2 {
		return 0, false
	}

	body := s[1:end]

	if body[0] == '#' {
		digits := body[1:]
		isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
		if len(digits) > 0 && (digits[0] == 'x' || digits[0] == 'X') {
			digits = digits[1:]
			isDigit = func(c byte) bool {
				return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			}
		}

		if len(digits) == 0 {
			return 0, false
		}
		for _, c := range digits {
			if !isDigit(c) {
				return 0, false
			}
		}

		return end + 1, true
	}

	for k, c := range body {
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (k == 0 || c < '0' || c > '9') {
			return 0, false
		}
	}

	name := string(body)
	if _, ok := xml.HTMLEntity[name]; ok || name == "apos" {
		return end + 1, true
	}

	if decoded := stdhtml.UnescapeString(string(s[:end+1])); decoded != string(s[:end+1]) {
		return end + 1, false
	}

	return 0, false
}

// isTagSpace reports whether c is space as HTML reads it inside a tag.
func isTagSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}
