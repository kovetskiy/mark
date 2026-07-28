package transformer

import (
	"bytes"
	"strings"

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
// Unlike transformDetails this never builds a DOM, so it does not require the
// fragment to be well-formed -- which is the point: the fragments produced by a
// blank-line split are well-formed only once concatenated.
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
			// what transformDetails produces for the balanced case. The <summary>
			// normally sits in the same fragment as its <details>; if it does not,
			// the expand simply renders without a title.
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
