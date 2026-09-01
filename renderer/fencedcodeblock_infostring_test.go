package renderer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFencedCodeBlockMkDocsTitle covers the way a code block is captioned
// everywhere outside mark.
//
// Material for MkDocs writes title="config.yml", and mark accepted only its
// own "title config.yml": the caption was lost and the entire token fell
// through to the theme catch-all, so the published macro carried a theme of
// `title="config.yml"` and no title at all.
func TestFencedCodeBlockMkDocsTitle(t *testing.T) {
	tests := []struct {
		name string
		info string
		lang string
	}{
		{name: "double quoted", info: `yaml title="config.yml"`, lang: "yaml"},
		{name: "single quoted", info: `yaml title='config.yml'`, lang: "yaml"},
		{name: "unquoted", info: `yaml title=config.yml`, lang: "yaml"},
		{name: "spaces around the equals", info: `yaml title = "config.yml"`, lang: "yaml"},
		{name: "no language", info: `title="config.yml"`, lang: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := fencedCode(t, tt.info)
			assertWellFormed(t, actual)

			assert.Contains(t, actual, `<ac:parameter ac:name="language">`+tt.lang+`</ac:parameter>`)
			assert.Contains(t, actual, `<ac:parameter ac:name="title">config.yml</ac:parameter>`)
			assert.NotContains(t, actual, `ac:name="theme"`)
		})
	}
}

// TestFencedCodeBlockMkDocsTitleWithSpaces covers a quoted title holding the
// spaces that the unquoted form could not.
func TestFencedCodeBlockMkDocsTitleWithSpaces(t *testing.T) {
	actual := fencedCode(t, `bash collapse title="Deploy the whole thing"`)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:parameter ac:name="language">bash</ac:parameter>`)
	assert.Contains(t, actual, `<ac:parameter ac:name="collapse">true</ac:parameter>`)
	assert.Contains(t, actual, `<ac:parameter ac:name="title">Deploy the whole thing</ac:parameter>`)
	assert.NotContains(t, actual, `ac:name="theme"`)
}

// TestFencedCodeBlockAttributeBlock covers the "{...}" attribute list Pandoc
// and MkDocs put in an info string.
//
// Nothing inside it is mark's vocabulary, so every word of it used to fall
// through to the theme catch-all -- and in the Pandoc form the language was
// lost as well and the digits of hl_lines turned line numbering on.
func TestFencedCodeBlockAttributeBlock(t *testing.T) {
	tests := []struct {
		name string
		info string
		lang string
	}{
		{name: "trailing list keeps the language", info: `python {.line-numbers}`, lang: "python"},
		{name: "leading list names the language", info: `{ .js hl_lines="1 2" }`, lang: "js"},
		{name: "leading list with no space", info: `{.python}`, lang: "python"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := fencedCode(t, tt.info)
			assertWellFormed(t, actual)

			assert.Contains(t, actual, `<ac:parameter ac:name="language">`+tt.lang+`</ac:parameter>`)
			assert.NotContains(t, actual, `ac:name="theme"`)
			assert.NotContains(t, actual, `ac:name="linenumbers"`)
			assert.NotContains(t, actual, `ac:name="firstline"`)
		})
	}
}

// TestFencedCodeBlockAttributeBlockTitle covers the two conventions written
// together, which is how Material for MkDocs documents its own examples.
func TestFencedCodeBlockAttributeBlockTitle(t *testing.T) {
	actual := fencedCode(t, `{ .yaml title="config.yml" }`)
	assertWellFormed(t, actual)

	assert.Contains(t, actual, `<ac:parameter ac:name="language">yaml</ac:parameter>`)
	assert.Contains(t, actual, `<ac:parameter ac:name="title">config.yml</ac:parameter>`)
	assert.NotContains(t, actual, `ac:name="theme"`)
}

// TestFencedCodeBlockNumericOptionMustBeAllDigits covers the option that means
// two things at once.
//
// fmt.Sscanf("%d") stops at the first byte it cannot use and calls what it read
// a success, so an option that merely began with a digit set the first line and
// turned numbering on. Only an option that is a number all through is one.
func TestFencedCodeBlockNumericOptionMustBeAllDigits(t *testing.T) {
	for _, info := range []string{`go 1;`, `go 2"`, `go 3px`} {
		t.Run(info, func(t *testing.T) {
			actual := fencedCode(t, info)
			assertWellFormed(t, actual)

			assert.NotContains(t, actual, `ac:name="firstline"`)
			assert.NotContains(t, actual, `ac:name="linenumbers"`)
		})
	}
}
