package macro

import (
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A macro whose pattern embeds comment markers ("<!-- ac:details -->") contains
// more than one "-->" inside its own directive. Ending the directive at the last
// "-->" before the next macro consumed the document body too, so the content the
// macro was meant to wrap disappeared from the output entirely.
func TestExtractMacros_NestedCommentPatternKeepsBody(t *testing.T) {
	// #inline keeps the test on the extraction logic: a real "Template: ac:details"
	// would send ExtractMacros off to load a template file from disk.
	contents := []byte(`<!-- Macro: <!-- ac:details -->([^:][\s\S]*?)<!-- ac:details end -->
     Template: #inline
     inline: "WRAPPED${1}"
-->

<!-- ac:details -->
| A | B |
|---|---|
| 1 | 2 |
<!-- ac:details end -->

Body text.
`)

	macros, remaining, err := ExtractMacros("", "", contents, template.New("test"))
	require.NoError(t, err)
	require.Len(t, macros, 1, "the directive should yield exactly one macro")

	rest := string(remaining)
	assert.NotContains(t, rest, "Macro:", "the directive itself must be stripped")
	// The table and the trailing prose are the macro's input, not part of its
	// declaration, so they have to survive extraction.
	assert.Contains(t, rest, "| A | B |", "the table must not be swallowed")
	assert.Contains(t, rest, "Body text.", "text after the macro must not be swallowed")
	assert.Contains(t, rest, "<!-- ac:details -->", "the macro's own markers must remain for Apply")
}

func TestFindDirectiveEnd(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // the directive text expected up to and including "-->"
	}{
		{
			name: "plain directive ends at its own terminator",
			in:   "<!-- Macro: :a:\n Template: x\n-->\n\nlater -->\n",
			want: "<!-- Macro: :a:\n Template: x\n-->",
		},
		{
			name: "nested pattern consumes two terminators",
			in:   "<!-- Macro: <!-- d -->(x)\n Template: y\n-->\n\nbody\n",
			want: "<!-- Macro: <!-- d -->(x)\n Template: y\n-->",
		},
		{
			name: "unterminated comment reports -1",
			in:   "<!-- Macro: :a:\n Template: x\n",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findDirectiveEnd(tt.in)
			if tt.want == "" {
				assert.Equal(t, -1, got)
				return
			}
			require.NotEqual(t, -1, got, "expected a terminator")
			assert.Equal(t, tt.want, tt.in[:got+len("-->")])
			assert.True(t, strings.HasSuffix(tt.in[:got+3], "-->"))
		})
	}
}

// TestExtractMacros_InlineTemplateUsesDefaultDelims pins the fix for #362.
//
// text/template's New copies the receiver's delimiters onto the new template,
// and ProcessIncludes returns a set whose most recent member was parsed with
// whatever an include declared via `Delims:`. Handing that set to ExtractMacros
// meant an inline macro body was parsed with the include's delimiters, so its
// own {{ }} actions were left as literal text -- silently, since a template
// with no recognised actions parses perfectly well.
//
// The file-backed branch already pinned "{{" and "}}" explicitly when calling
// LoadTemplate; only the inline branch inherited.
func TestExtractMacros_InlineTemplateUsesDefaultDelims(t *testing.T) {
	// A template set as it looks after an include declaring `Delims: << >>`.
	poisoned, err := template.New("root").Delims("<<", ">>").Parse("ignored")
	require.NoError(t, err)

	contents := []byte(`<!-- Macro: ::greet::
     Template: #inline
     inline: "Hello {{ .Var }}"
-->

::greet::
`)

	macros, _, err := ExtractMacros("", "", contents, poisoned)
	require.NoError(t, err)
	require.Len(t, macros, 1)

	var out strings.Builder
	require.NoError(t, macros[0].Template.Execute(&out, map[string]any{"Var": "world"}))

	assert.Equal(t, "Hello world", out.String(),
		"an inline macro must use {{ }} regardless of any Delims: an include declared")
}
