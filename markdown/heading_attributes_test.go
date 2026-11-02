package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHeadingAttributesTakeNoBareName: an attribute block is "#id", ".class"
// and "key=value" only, a comma between two of them allowed as goldmark v1
// allowed it. A bare name such as the "{id}" of "GET /users/{id}" is
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
		"## S {data-k='v w'}\n":                  `<h2 data-k="v w" id="S">S</h2>`,
		"## D {data-k=\"v w\"}\n":                `<h2 data-k="v w" id="D">D</h2>`,
		"## E {data-k = \"v\"}\n":                `<h2 data-k="v" id="E">E</h2>`,
		"## Classes {.a .b}\n":                   `<h2 class="a b" id="Classes">Classes</h2>`,
		"## All {#i .c data-k=v}\n":              `<h2 id="i" class="c" data-k="v">All</h2>`,
		"## Then bare {k=v x}\n":                 `<h2 id="Then-bare-kv-x">Then bare {k=v x}</h2>`,
		"## Bare first {x k=v}\n":                `<h2 id="Bare-first-x-kv">Bare first {x k=v}</h2>`,
		"## Bare then id {x #i}\n":               `<h2 id="Bare-then-id-x-i">Bare then id {x #i}</h2>`,
		"## Quoted bare {\"x\"}\n":               `<h2 id="Quoted-bare-x">Quoted bare {&quot;x&quot;}</h2>`,
		"Setext {.c data-k='v'}\n---\n":          `<h2 class="c" data-k="v" id="Setext">Setext</h2>`,
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
	} {
		out := compileDoc(t, source)
		assert.Equal(t, want+"\n", out, "%q", source)
	}
}
