package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The info-string regex captured the language with \w, which stops at the first
// non-word character. The remainder was not rejected: it fell through to the
// options group and then the theme catch-all, so "```c#" compiled to language
// "c" plus a real ac:parameter theme of "#".
func TestFenceLanguageWithNonWordCharacters(t *testing.T) {
	tests := []struct{ info, lang string }{
		{"c#", "c#"},
		{"c++", "c++"},
		{"objective-c", "objective-c"},
		{"html/xml", "html/xml"},
		{"go", "go"},
	}

	for _, tt := range tests {
		t.Run(tt.info, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := CompileMarkdown(
				[]byte("```"+tt.info+"\nx\n```\n"), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)

			assert.Contains(t, out, `<ac:parameter ac:name="language">`+tt.lang+`</ac:parameter>`)
			assert.NotContains(t, out, `ac:name="theme"`,
				"the language remainder must not be misfiled as a theme")
		})
	}
}

// The existing option and title syntax must keep working.
func TestFenceOptionsStillParse(t *testing.T) {
	tests := []struct {
		info string
		want string
	}{
		{"go collapse", `<ac:parameter ac:name="collapse">true</ac:parameter>`},
		{"go title My Code", `<ac:parameter ac:name="title">My Code</ac:parameter>`},
		{"go linenumbers", `<ac:parameter ac:name="linenumbers">true</ac:parameter>`},
		{"go 5", `<ac:parameter ac:name="firstline">5</ac:parameter>`},
	}

	for _, tt := range tests {
		t.Run(tt.info, func(t *testing.T) {
			std, err := stdlib.New(nil)
			require.NoError(t, err)

			out, _, err := CompileMarkdown(
				[]byte("```"+tt.info+"\nx\n```\n"), std, "test.md", types.MarkConfig{})
			require.NoError(t, err)

			assert.Contains(t, out, tt.want)
		})
	}
}
