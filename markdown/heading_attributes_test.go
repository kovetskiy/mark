package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHeadingAttributesTakeNoBareName: an attribute block is "#id", ".class"
// and "key=value" only. A bare name such as the "{id}" of "GET /users/{id}" is
// heading text, and the anchor is made from it.
//
// Every form is pinned, unquoted, single- and double-quoted values included:
// which block is taken is decided from how goldmark stores each value, so a
// goldmark release that stores them differently fails here, not on a page.
func TestHeadingAttributesTakeNoBareName(t *testing.T) {
	for source, want := range map[string]string{
		"## GET /users/{id}\n":          `<h2 id="GET-/users/id">GET /users/{id}</h2>`,
		"# Title {draft}\n":             `<h1 id="Title-draft">Title {draft}</h1>`,
		"## Set {a, b}\n":               `<h2 id="Set-a-b">Set {a, b}</h2>`,
		"## Heading{x}\n":               `<h2 id="Headingx">Heading{x}</h2>`,
		"Setext {id}\n---\n":            `<h2 id="Setext-id">Setext {id}</h2>`,
		"## Other {#custom}\n":          `<h2 id="custom">Other</h2>`,
		"## C {.cls #i}\n":              `<h2 class="cls" id="i">C</h2>`,
		"## K {data-x=\"1\"}\n":         `<h2 data-x="1" id="K">K</h2>`,
		"Setext {#sid}\n===\n":          `<h1 id="sid">Setext</h1>`,
		"## T {#x} ##\n":                `<h2 id="T-x">T {#x}</h2>`,
		"## Same\n\n## Same\n":          "<h2 id=\"Same\">Same</h2>\n<h2 id=\"Same-1\">Same</h2>",
		"## Escaped \\{#x}\n":           `<h2 id="Escaped-x">Escaped {#x}</h2>`,
		"## U {data-k=v}\n":             `<h2 data-k="v" id="U">U</h2>`,
		"## S {data-k='v w'}\n":         `<h2 data-k="v w" id="S">S</h2>`,
		"## D {data-k=\"v w\"}\n":       `<h2 data-k="v w" id="D">D</h2>`,
		"## E {data-k = \"v\"}\n":       `<h2 data-k="v" id="E">E</h2>`,
		"## Classes {.a .b}\n":          `<h2 class="a b" id="Classes">Classes</h2>`,
		"## All {#i .c data-k=v}\n":     `<h2 id="i" class="c" data-k="v">All</h2>`,
		"## Then bare {k=v x}\n":        `<h2 id="Then-bare-kv-x">Then bare {k=v x}</h2>`,
		"## Bare first {x k=v}\n":       `<h2 id="Bare-first-x-kv">Bare first {x k=v}</h2>`,
		"## Bare then id {x #i}\n":      `<h2 id="Bare-then-id-x-i">Bare then id {x #i}</h2>`,
		"## Quoted bare {\"x\"}\n":      `<h2 id="Quoted-bare-x">Quoted bare {&quot;x&quot;}</h2>`,
		"Setext {.c data-k='v'}\n---\n": `<h2 class="c" data-k="v" id="Setext">Setext</h2>`,
		"Setext {k=v x}\n---\n":         `<h2 id="Setext-kv-x">Setext {k=v x}</h2>`,
	} {
		for name, compile := range map[string]func([]byte, *stdlib.Lib, string, types.MarkConfig) (string, []attachment.Attachment, error){
			"default": CompileMarkdown,
			"legacy":  CompileMarkdownLegacy,
		} {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := compile([]byte(source), std, "test.md", types.MarkConfig{})
			require.NoError(t, err, "%s: %q", name, source)
			assert.Equal(t, want+"\n", out, "%s: %q", name, source)
		}
	}
}
