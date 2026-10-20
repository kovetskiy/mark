package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHeadingAttributesTakeNoBareName: goldmark v2 reads "{id}" and "{a, b}"
// as attributes named id, a and b, so a heading that ended in braces lost them
// from its text and took its anchor from the remains: every "/{id}" endpoint
// heading was called "id". v1 took only "#id", ".class" and "key=value", and
// so do both compile paths here.
func TestHeadingAttributesTakeNoBareName(t *testing.T) {
	for source, want := range map[string]string{
		"## GET /users/{id}\n":  `<h2 id="GET-/users/id">GET /users/{id}</h2>`,
		"# Title {draft}\n":     `<h1 id="Title-draft">Title {draft}</h1>`,
		"## Set {a, b}\n":       `<h2 id="Set-a-b">Set {a, b}</h2>`,
		"## Heading{x}\n":       `<h2 id="Headingx">Heading{x}</h2>`,
		"Setext {id}\n---\n":    `<h2 id="Setext-id">Setext {id}</h2>`,
		"## Other {#custom}\n":  `<h2 id="custom">Other</h2>`,
		"## C {.cls #i}\n":      `<h2 class="cls" id="i">C</h2>`,
		"## K {data-x=\"1\"}\n": `<h2 data-x="1" id="K">K</h2>`,
		"Setext {#sid}\n===\n":  `<h1 id="sid">Setext</h1>`,
		"## T {#x} ##\n":        `<h2 id="T-x">T {#x}</h2>`,
		"## Same\n\n## Same\n":  "<h2 id=\"Same\">Same</h2>\n<h2 id=\"Same-1\">Same</h2>",
		"## Escaped \\{#x}\n":   `<h2 id="Escaped-x">Escaped {#x}</h2>`,
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
