package renderer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
)

// TestCodeLanguage: Confluence knows a language by one name, and Data Center
// fails to render a code macro whose language it does not know.
func TestCodeLanguage(t *testing.T) {
	for _, tt := range []struct{ fence, want string }{
		{"sh", "bash"},
		{"shell", "bash"},
		{"yml", "yaml"},
		{"py", "python"},
		{"Python", "python"},
		{"c++", "cpp"},
		{"golang", "go"},
		{"ts", "typescript"},
		{"text", "none"},
		// Spelled differently on Cloud and Data Center, so left as written.
		{"js", "js"},
		{"javascript", "javascript"},
		{"c#", "c#"},
		{"html", "html"},
		// Unknown to mark: passed through, in lower case.
		{"zig", "zig"},
		{"Zig", "zig"},
		{"", ""},
	} {
		assert.Equal(t, tt.want, crenderer.CodeLanguage(tt.fence), tt.fence)
	}
}

func TestFencedCodeBlockAlias(t *testing.T) {
	actual := fencedCode(t, "yml title config.yml")
	assertWellFormed(t, actual)
	assert.Contains(t, actual, `<ac:parameter ac:name="language">yaml</ac:parameter>`)
	assert.Contains(t, actual, `<ac:parameter ac:name="title">config.yml</ac:parameter>`)
}
