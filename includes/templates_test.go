package includes

import (
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
)

func TestFindCodeBlockRanges(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []codeBlockRange
	}{
		{
			name:     "no code blocks",
			input:    "Hello world\nNo code here",
			expected: nil,
		},
		{
			name: "single fenced code block with backticks",
			// "Some text\n```\ncode inside\n```\nmore text"
			// 0123456789...
			input: `Some text
` + "```" + `
code inside
` + "```" + `
more text`,
			expected: []codeBlockRange{{start: 10, end: 30}},
		},
		{
			name: "single fenced code block with tildes",
			input: `Some text
~~~
code inside
~~~
more text`,
			expected: []codeBlockRange{{start: 10, end: 30}},
		},
		{
			name: "code block with language identifier",
			// "Some text\n```bash\necho hello\n```\nmore text"
			input: `Some text
` + "```bash" + `
echo hello
` + "```" + `
more text`,
			expected: []codeBlockRange{{start: 10, end: 33}},
		},
		{
			name: "multiple code blocks",
			// "First\n```\nblock 1\n```\nBetween\n```\nblock 2\n```\nLast"
			input: `First
` + "```" + `
block 1
` + "```" + `
Between
` + "```" + `
block 2
` + "```" + `
Last`,
			expected: []codeBlockRange{{start: 6, end: 22}, {start: 30, end: 46}},
		},
		{
			name: "four backticks",
			input: "Some text\n" + "````" + "\ncode\n" + "````" + "\nmore",
			expected: []codeBlockRange{{start: 10, end: 25}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findCodeBlockRanges([]byte(tt.input))
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsInsideCodeBlock(t *testing.T) {
	ranges := []codeBlockRange{
		{start: 10, end: 30},
		{start: 50, end: 70},
	}

	tests := []struct {
		pos      int
		expected bool
	}{
		{5, false},   // before first block
		{10, true},   // start of first block
		{20, true},   // inside first block
		{29, true},   // just before end of first block
		{30, false},  // at end of first block (exclusive)
		{40, false},  // between blocks
		{50, true},   // start of second block
		{60, true},   // inside second block
		{70, false},  // at end of second block
		{80, false},  // after all blocks
	}

	for _, tt := range tests {
		result := isInsideCodeBlock(tt.pos, ranges)
		assert.Equal(t, tt.expected, result, "pos=%d", tt.pos)
	}
}

func TestProcessIncludes_SkipsCodeBlocks(t *testing.T) {
	// This test verifies that Include directives inside code blocks are not processed
	input := `Some text before

` + "```" + `
<!-- Include: example.md -->
` + "```" + `

Some text after
<!-- Include: real.md -->
End`

	// The Include inside the code block should be preserved
	// The Include outside should be processed (but will fail since file doesn't exist)
	// We just verify the code block content is untouched

	templates := template.New("test")

	_, result, _, _ := ProcessIncludes(".", "", []byte(input), templates)

	resultStr := string(result)

	// The Include inside code block should still be there
	assert.Contains(t, resultStr, "<!-- Include: example.md -->")

	// The code block markers should still be there
	assert.Contains(t, resultStr, "```\n<!-- Include: example.md -->")
}

func TestProcessIncludes_ProcessesOutsideCodeBlocks(t *testing.T) {
	// Create a simple template to include
	templates := template.New("test")
	templates, _ = templates.New("test-include").Parse("INCLUDED CONTENT")

	input := `Normal text
<!-- Include: test-include.md -->
More text`

	_, result, recurse, err := ProcessIncludes(".", "", []byte(input), templates)

	assert.NoError(t, err)
	assert.True(t, recurse)
	assert.Contains(t, string(result), "INCLUDED CONTENT")
	assert.NotContains(t, string(result), "<!-- Include:")
}

func TestProcessIncludes_SelfDocumentation(t *testing.T) {
	// This test replicates the issue https://github.com/kovetskiy/mark/issues/717:
	// Include directive inside a code block showing example Mark usage should not be processed
	input := "# Documentation page\n\nHere's how to use Mark:\n\n" +
		"```\n" +
		"<!-- Space: EXMPLSPC -->\n" +
		"<!-- Title: Documentation as code -->\n" +
		"<!-- Include: shared/disclaimer.md -->\n" +
		"```\n\n" +
		"Copy the above to your markdown file."

	templates := template.New("test")

	_, result, recurse, err := ProcessIncludes(".", "", []byte(input), templates)

	assert.NoError(t, err)
	assert.False(t, recurse, "Should not have processed any includes")

	resultStr := string(result)

	// The Include inside the code block should be preserved exactly as written
	assert.Contains(t, resultStr, "<!-- Include: shared/disclaimer.md -->")
	// The code block structure should be intact
	assert.Contains(t, resultStr, "```\n<!-- Space: EXMPLSPC -->")
}

func TestProcessIncludes_MixedInAndOutOfCodeBlocks(t *testing.T) {
	// Test having includes both inside and outside code blocks
	templates := template.New("test")
	templates, _ = templates.New("real-include").Parse("REAL INCLUDED CONTENT")

	input := "Some intro text\n\n" +
		"<!-- Include: real-include.md -->\n\n" +
		"Example code:\n\n" +
		"```\n" +
		"<!-- Include: example-in-codeblock.md -->\n" +
		"```\n\n" +
		"More text"

	_, result, recurse, err := ProcessIncludes(".", "", []byte(input), templates)

	assert.NoError(t, err)
	assert.True(t, recurse, "Should have processed at least one include")

	resultStr := string(result)

	// The include outside the code block should be processed
	assert.Contains(t, resultStr, "REAL INCLUDED CONTENT")
	// The include inside the code block should NOT be processed
	assert.Contains(t, resultStr, "<!-- Include: example-in-codeblock.md -->")
}
