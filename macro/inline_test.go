package macro

import (
	"testing"
	"text/template"

	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyAll extracts the macros a document defines, with the stdlib template
// set a real compile uses, and applies them to what is left.
func applyAll(t *testing.T, doc string) (string, error) {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	macros, rest, err := ExtractMacros("", "", []byte(doc), std.Templates)
	require.NoError(t, err)
	require.NotEmpty(t, macros)

	for _, m := range macros {
		rest, err = m.Apply(rest)
		if err != nil {
			return string(rest), err
		}
	}

	return string(rest), nil
}

// An inline body was parsed again for every match into a set of its own, so
// the stdlib's functions and templates were missing from it: an inline macro
// could not escape through xmlesc, which is what AGENTS.md asks of every
// template, nor call a stdlib template.
func TestApply_InlineTemplateSeesStdlib(t *testing.T) {
	t.Run("functions", func(t *testing.T) {
		out, err := applyAll(t, `<!-- Macro: SAY\((.*?)\)
     Template: #inline
     inline: "<b>{{ \"${1}\" | xmlesc }}</b>" -->

SAY(a < b & c)
`)
		require.NoError(t, err)
		assert.Contains(t, out, "<b>a &lt; b &amp; c</b>")
	})

	t.Run("templates", func(t *testing.T) {
		out, err := applyAll(t, `<!-- Macro: :st:(\w+):
     Template: #inline
     Title: ${1}
     Color: Green
     inline: "{{ template \"ac:status\" . }}" -->

:st:DONE:
`)
		require.NoError(t, err)
		assert.Contains(t, out, `<ac:structured-macro ac:name="status">`)
		assert.Contains(t, out, `<ac:parameter ac:name="title">DONE</ac:parameter>`)
		assert.Contains(t, out, `<ac:parameter ac:name="colour">Green</ac:parameter>`)
	})
}

// Every match assigned the one shared error, so a later match that expanded
// cleanly reset it to nil: the failure was published as the match's own text,
// and the compile succeeded.
func TestApply_KeepsFirstError(t *testing.T) {
	out, err := applyAll(t, `<!-- Macro: X-(\w+)
     Template: #inline
     v: "${1}"
     inline: "[{{ slice .v 0 3 }}]" -->

X-ab and X-abcd
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slice")
	assert.NotContains(t, out, "[abc]", "nothing after the failure is expanded")
}

// Captured text was spliced into the inline body before it was parsed, so
// document text that happened to hold template syntax was run as template
// code.
func TestApply_CapturesAreTextNotCode(t *testing.T) {
	t.Run("in the body", func(t *testing.T) {
		out, err := applyAll(t, `<!-- Macro: SAY\((.*?)\)
     Template: #inline
     inline: "said: ${1}" -->

SAY(helm uses {{ .Values.x }} here)
`)
		require.NoError(t, err)
		assert.Contains(t, out, "said: helm uses {{ .Values.x }} here")
	})

	t.Run("in a string literal", func(t *testing.T) {
		// A quote in the capture ended the literal it was spliced into.
		out, err := applyAll(t, `<!-- Macro: SAY\((.*?)\)
     Template: #inline
     inline: "{{ \"${1}\" | xmlesc }}" -->

SAY(say "hi" {{ .x }})
`)
		require.NoError(t, err)
		assert.Contains(t, out, "say &#34;hi&#34; {{ .x }}")
	})

	t.Run("each match gets its own", func(t *testing.T) {
		out, err := applyAll(t, `<!-- Macro: T-(\d+)
     Template: #inline
     inline: "<${0}:{{ if eq \"${1}\" \"1\" }}one{{ else }}other{{ end }}>" -->

T-1 T-2
`)
		require.NoError(t, err)
		assert.Contains(t, out, "<T-1:one> <T-2:other>")
	})
}

// ExtractMacros removed a directive by appending onto remaining[:startIdx],
// which wrote into the buffer its caller passed in -- the document
// CompileMarkdown was handed.
func TestExtractMacros_LeavesInputUntouched(t *testing.T) {
	doc := "<!-- Macro: :a:\n     Template: #inline\n     inline: \"A\" -->\n\nbefore :a: after\n"
	input := []byte(doc)

	_, rest, err := ExtractMacros("", "", input, template.New("test"))
	require.NoError(t, err)

	assert.Equal(t, doc, string(input), "the caller's buffer must not change")
	assert.NotContains(t, string(rest), "Macro:")
}
