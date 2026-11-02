package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHeadingAttributesTakeNoBareName: an attribute block is one in goldmark
// v1's grammar -- "#id", ".class" and "key=value", a comma allowed between
// them -- and anything else stays heading text, so a heading's id does not
// change under a link to it. A bare name such as the "{id}" of "GET /users/{id}" is
// heading text, and the anchor is made from it.
//
// Every form is pinned, unquoted, single- and double-quoted values included:
// which block is taken is decided from how goldmark stores each value, so a
// goldmark release that stores them differently fails here, not on a page.
func TestHeadingAttributesTakeNoBareName(t *testing.T) {
	for source, want := range map[string]string{
		"## GET /users/{id}\n":                   `<h2 id="GET-/users/id">GET /users/{id}</h2>`,
		"# Title {draft}\n":                      `<h1 id="Title-draft">Title {draft}</h1>`,
		"## Set {a, b}\n":                        `<h2 id="Set-a-b">Set {a, b}</h2>`,
		"## Heading{x}\n":                        `<h2 id="Headingx">Heading{x}</h2>`,
		"Setext {id}\n---\n":                     `<h2 id="Setext-id">Setext {id}</h2>`,
		"## Other {#custom}\n":                   `<h2 id="custom">Other</h2>`,
		"## C {.cls #i}\n":                       `<h2 class="cls" id="i">C</h2>`,
		"## K {data-x=\"1\"}\n":                  `<h2 data-x="1" id="K">K</h2>`,
		"Setext {#sid}\n===\n":                   `<h1 id="sid">Setext</h1>`,
		"## T {#x} ##\n":                         `<h2 id="T-x">T {#x}</h2>`,
		"## Same\n\n## Same\n":                   "<h2 id=\"Same\">Same</h2>\n<h2 id=\"Same-1\">Same</h2>",
		"## Escaped \\{#x}\n":                    `<h2 id="Escaped-x">Escaped {#x}</h2>`,
		"## U {data-k=v}\n":                      `<h2 data-k="v" id="U">U</h2>`,
		"## S {data-k='v w'}\n":                  `<h2 id="S-data-kv-w">S {data-k='v w'}</h2>`,
		"## D {data-k=\"v w\"}\n":                `<h2 data-k="v w" id="D">D</h2>`,
		"## E {data-k = \"v\"}\n":                `<h2 data-k="v" id="E">E</h2>`,
		"## Classes {.a .b}\n":                   `<h2 class="a b" id="Classes">Classes</h2>`,
		"## All {#i .c data-k=v}\n":              `<h2 id="i" class="c" data-k="v">All</h2>`,
		"## Then bare {k=v x}\n":                 `<h2 id="Then-bare-kv-x">Then bare {k=v x}</h2>`,
		"## Bare first {x k=v}\n":                `<h2 id="Bare-first-x-kv">Bare first {x k=v}</h2>`,
		"## Bare then id {x #i}\n":               `<h2 id="Bare-then-id-x-i">Bare then id {x #i}</h2>`,
		"## Quoted bare {\"x\"}\n":               `<h2 id="Quoted-bare-x">Quoted bare {&quot;x&quot;}</h2>`,
		"Setext {.c data-k='v'}\n---\n":          `<h2 id="Setext-.c-data-kv">Setext {.c data-k='v'}</h2>`,
		"Setext {k=v x}\n---\n":                  `<h2 id="Setext-kv-x">Setext {k=v x}</h2>`,
		"# Comma {#a,.b}\n":                      `<h1 id="a" class="b">Comma</h1>`,
		"## Comma space {#a, .b}\n":              `<h2 id="a" class="b">Comma space</h2>`,
		"## Quoted comma {data-k=\"x,y\", .c}\n": `<h2 data-k="x,y" class="c" id="Quoted-comma">Quoted comma</h2>`,
		"Setext {#s,.c}\n---\n":                  `<h2 id="s" class="c">Setext</h2>`,
		"## Trailing comma {#a,}\n":              `<h2 id="a">Trailing comma</h2>`,
		"## Leading comma {,#a}\n":               `<h2 id="Leading-comma-a">Leading comma {,#a}</h2>`,
		"## Two commas {#a,,.b}\n":               `<h2 id="Two-commas-a.b">Two commas {#a,,.b}</h2>`,
		"## Comma then bare {#a, x}\n":           `<h2 id="Comma-then-bare-a-x">Comma then bare {#a, x}</h2>`,
		"## Comma closed {#a,.b} ##\n":           `<h2 id="Comma-closed-a.b">Comma closed {#a,.b}</h2>`,
		"## T {key=a,b}\n":                       `<h2 id="T-keyab">T {key=a,b}</h2>`,
		"## T {k=}\n":                            `<h2 id="T-k">T {k=}</h2>`,
		"Title {k=v,b}\n===\n":                   `<h1 id="Title-kvb">Title {k=v,b}</h1>`,
		"T {k=}\n===\n":                          `<h1 id="T-k">T {k=}</h1>`,
		"## T ## {#x}\n":                         `<h2 id="x">T</h2>`,
		"## Escaped {data-s=\"a\\\"b\"}\n":       `<h2 data-s="a&quot;b" id="Escaped">Escaped</h2>`,
		"## Number {data-n=5}\n":                 `<h2 data-n="" id="Number">Number</h2>`,
		"## Class word {class=foo}\n":            `<h2 class="foo" id="Class-word">Class word</h2>`,
		"## Class number {class=1}\n":            `<h2 id="Class-number-class1">Class number {class=1}</h2>`,
		"a | b\n--|--\nc | x {#id}\n---\n":       "<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>c</td>\n<td>x {#id}</td>\n</tr>\n</tbody>\n</table>\n<hr />",
	} {
		out := compileDoc(t, source)
		assert.Equal(t, want+"\n", out, "%q", source)
	}
}
