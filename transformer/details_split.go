package transformer

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
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
func detailsBalance(raw []byte) int {
	lower := bytes.ToLower(raw)
	if !bytes.Contains(lower, []byte("<details")) && !bytes.Contains(lower, []byte("</details")) {
		return 0
	}

	depth := 0
	z := html.NewTokenizer(bytes.NewReader(raw))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return depth
		case html.StartTagToken:
			if name, _ := z.TagName(); string(name) == "details" {
				depth++
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "details" {
				depth--
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

// rewriteUnbalancedDetails converts <details>/<summary>/</details> in an
// unbalanced fragment into the corresponding Confluence expand markup, passing
// every other token through verbatim.
//
// Unlike transformDetailsAt's balanced path this never builds a DOM, so it does
// not require the fragment to be well-formed -- which is the point: the
// fragments produced by a blank-line split are well-formed only once
// concatenated.
func rewriteUnbalancedDetails(raw []byte) ([]byte, bool) {
	toks := tokenizeFragment(raw)

	var buf bytes.Buffer
	var changed bool

	for i := 0; i < len(toks); i++ {
		tok := toks[i]

		if tok.kind == html.StartTagToken && tok.name == "details" {
			changed = true
			buf.WriteString(`<ac:structured-macro ac:name="expand">`)

			// Emit the title before opening the body, so element order matches
			// what the balanced DOM path produces. The <summary> normally sits in
			// the same fragment as its <details>; if it does not, the expand
			// simply renders without a title.
			if title, end, ok := summaryAt(toks, i+1); ok {
				if title != "" {
					buf.WriteString(`<ac:parameter ac:name="title">`)
					buf.WriteString(html.EscapeString(title))
					buf.WriteString(`</ac:parameter>`)
				}
				i = end
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

// summaryAt looks for a <summary> element starting at toks[from], skipping only
// whitespace. It returns the summary's text and the index of its </summary>.
// Anything else in between means this <details> has no leading summary.
func summaryAt(toks []detailsToken, from int) (title string, end int, ok bool) {
	i := from
	for i < len(toks) && toks[i].kind == html.TextToken && len(bytes.TrimSpace(toks[i].text)) == 0 {
		i++
	}
	if i >= len(toks) || toks[i].kind != html.StartTagToken || toks[i].name != "summary" {
		return "", 0, false
	}

	var sb strings.Builder
	for j := i + 1; j < len(toks); j++ {
		switch {
		case toks[j].kind == html.EndTagToken && toks[j].name == "summary":
			return strings.TrimSpace(sb.String()), j, true
		case toks[j].kind == html.TextToken:
			sb.Write(toks[j].text)
		case toks[j].kind == html.StartTagToken && toks[j].name == "details":
			// Malformed: a nested <details> opened before </summary> closed.
			// Leave the whole thing alone rather than swallow it.
			return "", 0, false
		}
	}
	return "", 0, false
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
		case *ast.RawHTML, *ast.Text, *ast.String:
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
