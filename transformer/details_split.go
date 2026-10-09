package transformer

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"golang.org/x/net/html"
)

// detailsBalance reports the net nesting change of <details> tags in raw, i.e.
// (opening tags) - (closing tags). A self-contained <details> tree yields 0; a
// fragment that only opens or only closes yields a non-zero value.
//
// A blank line terminates an HTML block in CommonMark, so a <details> element
// whose body contains blank-line-separated Markdown is split by the parser
// across several sibling nodes. Each fragment is individually unbalanced, which
// html.Parse cannot represent: it auto-closes the dangling <details>, dropping
// the body out of the macro and leaking a literal </details> into the output --
// which Confluence then rejects with "Unexpected close tag </details>".
// Detecting the imbalance lets us rewrite such fragments tag-by-tag instead.
//
// The lowest depth reached along the way is reported with it, because the net
// change alone cannot tell a self-contained element from a fragment that closes
// one and opens another. "</details><details>" nets to zero and is nothing of
// the sort: html.Parse drops the closer it has nothing to match, and the two
// sections telescope, the second ending up empty and inside the first. A
// fragment whose depth ever goes below where it started has closed something
// opened before it, whatever it does afterwards.
func detailsBalance(raw []byte) (balance, lowest int) {
	lower := bytes.ToLower(raw)
	if !bytes.Contains(lower, []byte("<details")) && !bytes.Contains(lower, []byte("</details")) {
		return 0, 0
	}

	depth := 0
	z := html.NewTokenizer(bytes.NewReader(raw))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return depth, lowest
		case html.StartTagToken:
			if name, _ := z.TagName(); string(name) == "details" {
				depth++
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "details" {
				depth--
				lowest = min(lowest, depth)
			}
		}
	}
}

// detailsToken is one tokenizer output, retained so the rewriter can look ahead
// for a <summary> before deciding what to emit for its <details>.
type detailsToken struct {
	kind html.TokenType
	name string
	raw  []byte
	text []byte
}

func tokenizeFragment(raw []byte) []detailsToken {
	var toks []detailsToken
	z := html.NewTokenizer(bytes.NewReader(raw))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return toks
		}

		tok := detailsToken{kind: tt}
		// z.Raw()/z.Text() point into the tokenizer's buffer, which is reused on
		// the next Next(); copy anything we keep.
		tok.raw = append([]byte(nil), z.Raw()...)
		switch tt {
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tok.name = string(name)
		case html.TextToken:
			tok.text = append([]byte(nil), z.Text()...)
		}
		toks = append(toks, tok)
	}
}

// rewriteDetails converts <details>/<summary>/</details> in a fragment into the
// corresponding Confluence expand markup, passing every other token through
// verbatim.
//
// It never builds a DOM, so it does not require the fragment to be
// well-formed -- the fragments produced by a blank-line split are well-formed
// only once concatenated -- and it cannot rearrange storage-format markup the
// way an HTML5 parser does.
func rewriteDetails(raw []byte) ([]byte, bool) {
	toks := tokenizeFragment(raw)

	var buf bytes.Buffer
	var changed bool

	// A <summary> written after other content in its <details>, already
	// emitted as the macro's title, is dropped from the body when reached.
	lateSummaries := map[int]int{}

	for i := 0; i < len(toks); i++ {
		if end, ok := lateSummaries[i]; ok {
			i = end
			continue
		}

		tok := toks[i]

		if tok.kind == html.StartTagToken && tok.name == "details" {
			changed = true
			buf.WriteString(`<ac:structured-macro ac:name="expand">`)

			// Emit the title before opening the body, as the macro requires. The
			// <summary> normally sits in the same fragment as its <details>; if it
			// does not, the expand simply renders without a title.
			title, start, end, ok := summaryAt(toks, i+1)
			if ok {
				if title != "" {
					buf.WriteString(`<ac:parameter ac:name="title">`)
					buf.WriteString(html.EscapeString(title))
					buf.WriteString(`</ac:parameter>`)
				}
				if leadingWhitespace(toks, i+1, start) {
					i = end
				} else {
					lateSummaries[start] = end
				}
			}

			buf.WriteString(`<ac:rich-text-body>`)
			continue
		}

		if tok.kind == html.EndTagToken && tok.name == "details" {
			changed = true
			buf.WriteString(`</ac:rich-text-body></ac:structured-macro>`)
			continue
		}

		buf.Write(tok.raw)
	}

	if !changed {
		return raw, false
	}
	return buf.Bytes(), true
}

// summaryAt looks for the <summary> belonging to a <details> whose content
// starts at toks[from]: the first one before any <details> opens or closes, so
// that one belonging to a nested element is never taken. It returns the
// summary's text and the indexes of its <summary> and </summary>.
//
// HTML wants the summary first, and it nearly always is, but a browser takes it
// from anywhere among the element's children and so does this.
func summaryAt(toks []detailsToken, from int) (title string, start, end int, ok bool) {
	start = -1
	for i := from; i < len(toks); i++ {
		if toks[i].name == "details" {
			return "", 0, 0, false
		}
		if toks[i].kind == html.StartTagToken && toks[i].name == "summary" {
			start = i
			break
		}
	}
	if start == -1 {
		return "", 0, 0, false
	}

	var sb strings.Builder
	for j := start + 1; j < len(toks); j++ {
		switch {
		case toks[j].kind == html.EndTagToken && toks[j].name == "summary":
			return strings.TrimSpace(sb.String()), start, j, true
		case toks[j].kind == html.TextToken:
			sb.Write(toks[j].text)
		case toks[j].name == "details":
			// Malformed: a nested <details> opened before </summary> closed.
			// Leave the whole thing alone rather than swallow it.
			return "", 0, 0, false
		}
	}
	return "", 0, 0, false
}

// leadingWhitespace reports whether toks[from:to] is only whitespace text.
func leadingWhitespace(toks []detailsToken, from, to int) bool {
	for _, tok := range toks[from:to] {
		if tok.kind != html.TextToken || len(bytes.TrimSpace(tok.text)) != 0 {
			return false
		}
	}
	return true
}

// coalesceInlineDetails folds the inline siblings that follow node into its raw
// fragment, up to and including the "</summary>" belonging to a "<details>" that
// node opens. It returns the combined bytes and the siblings consumed.
//
// Goldmark emits inline HTML one AST node per tag, so
// "<details><summary>s</summary>" becomes three RawHTML nodes plus text. The
// fragment that opens the macro therefore cannot see its own title, and the
// summary would be rendered as body content instead. A block context already
// arrives as a single HTMLBlock, so nothing is folded there.
func coalesceInlineDetails(node ast.Node, raw []byte, source []byte) ([]byte, []ast.Node) {
	if _, ok := node.(*ast.RawHTML); !ok {
		return raw, nil
	}
	lower := bytes.ToLower(raw)
	// Only worth folding when this fragment opens a details without closing its
	// summary; anything else is already self-contained.
	if !bytes.Contains(lower, []byte("<details")) || bytes.Contains(lower, []byte("</summary")) {
		return raw, nil
	}

	combined := append([]byte(nil), raw...)
	var folded []ast.Node
	for sib := node.NextSibling(); sib != nil; sib = sib.NextSibling() {
		switch sib.(type) {
		case *ast.RawHTML, *ast.Text, *String:
		default:
			return raw, nil
		}
		if parent := sib.Parent(); parent != nil && parent.Kind() == ast.KindCodeSpan {
			return raw, nil
		}

		sibRaw := ExtractNodeRawContent(sib, source)
		combined = append(combined, sibRaw...)
		folded = append(folded, sib)

		if bytes.Contains(bytes.ToLower(sibRaw), []byte("</summary")) {
			return combined, folded
		}
		// A summary is a short leading element; give up rather than swallow the
		// rest of the paragraph looking for one that is not there.
		if len(folded) >= 4 {
			return raw, nil
		}
	}
	return raw, nil
}
