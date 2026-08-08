package stdlib

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewParsesAllTemplates guards against a template in templates() failing to
// parse, which New reports as an error rather than a panic and which no other
// test in the repository would catch.
func TestNewParsesAllTemplates(t *testing.T) {
	lib, err := New(nil)
	require.NoError(t, err)
	require.NotNil(t, lib)
	require.NotNil(t, lib.Templates)

	// A representative sample across the macro families; if templates() is
	// rewritten these must all still be defined.
	for _, name := range []string{
		"ac:layout",
		"ac:code",
		"ac:status",
		"ac:link:user",
		"ac:image",
		"ac:toc",
		"ac:children",
		"ac:emoticon",
	} {
		assert.NotNil(t, lib.Templates.Lookup(name), "template %q is not defined", name)
	}
}

// TestTemplatesEscapeInterpolatedValues covers the escaping helpers that the
// storage-format output depends on: a quote in a code block title must not be
// able to close the ac:parameter attribute, and a CDATA end marker in the body
// must be split across two sections.
func TestTemplatesEscapeInterpolatedValues(t *testing.T) {
	lib, err := New(nil)
	require.NoError(t, err)

	var out strings.Builder
	err = lib.Templates.ExecuteTemplate(&out, "ac:code", struct {
		Language    string
		Collapse    bool
		Theme       string
		Linenumbers bool
		Firstline   int
		Title       string
		Text        string
	}{
		Language: "go",
		Title:    `a "quoted" <title> & more`,
		Text:     "before ]]> after",
	})
	require.NoError(t, err)

	rendered := out.String()
	assert.NotContains(t, rendered, `"quoted"`, "title quotes must be escaped")
	assert.Contains(t, rendered, "&#34;quoted&#34;")
	assert.Contains(t, rendered, "&lt;title&gt;")
	assert.NotContains(
		t,
		rendered,
		"before ]]> after",
		"a raw ]]> would terminate the CDATA section early",
	)
}

// BenchmarkNew measures the cost of building the standard library. mark calls
// New once per processed file, so this is the per-file floor for template
// parsing.
func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		if _, err := New(nil); err != nil {
			b.Fatal(err)
		}
	}
}
