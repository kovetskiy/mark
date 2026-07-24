package includes

import (
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
)

// rangeOf returns a fenceRange spanning input[needleStart .. closingLineEnd+1].
// closingNeedle is the closing fence text (e.g. "```\n") whose end-of-line
// (including trailing newline if present) marks the range's end. Computing
// expected ranges this way keeps tests robust to edits of the input string.
func rangeOf(t *testing.T, input, openingNeedle, closingNeedle string) fenceRange {
	t.Helper()
	start := strings.Index(input, openingNeedle)
	if start < 0 {
		t.Fatalf("opening needle %q not in input", openingNeedle)
	}
	closeAt := strings.Index(input[start+len(openingNeedle):], closingNeedle)
	if closeAt < 0 {
		t.Fatalf("closing needle %q not in input after opening", closingNeedle)
	}
	end := start + len(openingNeedle) + closeAt + len(closingNeedle)
	return fenceRange{start: start, end: end}
}

func TestScanFenceRanges(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  func(string) []fenceRange
	}{
		{
			name:  "no code blocks",
			input: "Hello world\nNo code here",
			want:  func(string) []fenceRange { return nil },
		},
		{
			name:  "single backtick fence",
			input: "Some text\n```\ncode inside\n```\nmore text",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, "```\n", "```\n")}
			},
		},
		{
			name:  "single tilde fence",
			input: "Some text\n~~~\ncode inside\n~~~\nmore text",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, "~~~\n", "~~~\n")}
			},
		},
		{
			name:  "fence with info string",
			input: "Some text\n```bash\necho hello\n```\nmore text",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, "```bash\n", "```\n")}
			},
		},
		{
			name:  "two consecutive fences",
			input: "First\n```\nblock 1\n```\nBetween\n```\nblock 2\n```\nLast",
			want: func(in string) []fenceRange {
				first := strings.Index(in, "```\n")
				firstClose := first + len("```\n") + strings.Index(in[first+len("```\n"):], "```\n") + len("```\n")
				second := firstClose + strings.Index(in[firstClose:], "```\n")
				secondClose := second + len("```\n") + strings.Index(in[second+len("```\n"):], "```\n") + len("```\n")
				return []fenceRange{
					{start: first, end: firstClose},
					{start: second, end: secondClose},
				}
			},
		},
		{
			name:  "four-backtick fence closes only on four-or-more",
			input: "Some text\n````\ncontains ``` inside\n````\nmore",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, "````\n", "````\n")}
			},
		},
		{
			name:  "shorter close does not close longer open",
			input: "````\ncontent\n```\nstill in fence\n````\nafter",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, "````\n", "````\n")}
			},
		},
		{
			name:  "tilde inside backtick fence stays inside",
			input: "```\n~~~\ntext\n~~~\n```\nafter",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, "```\n", "```\n")}
			},
		},
		{
			name:  "indented one-space fence",
			input: "Hi\n ```\ncode\n ```\nbye",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, " ```\n", " ```\n")}
			},
		},
		{
			name:  "indented two-space fence",
			input: "Hi\n  ```\ncode\n  ```\nbye",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, "  ```\n", "  ```\n")}
			},
		},
		{
			name:  "indented three-space fence",
			input: "Hi\n   ```\ncode\n   ```\nbye",
			want: func(in string) []fenceRange {
				return []fenceRange{rangeOf(t, in, "   ```\n", "   ```\n")}
			},
		},
		{
			name:  "four-space indent is not a fence",
			input: "Hi\n    ```\nnot a fence\n    ```\nbye",
			want:  func(string) []fenceRange { return nil },
		},
		{
			name:  "unclosed fence runs to EOF",
			input: "intro\n```\nno close in sight\n",
			want: func(in string) []fenceRange {
				start := strings.Index(in, "```")
				return []fenceRange{{start: start, end: len(in)}}
			},
		},
		{
			name:  "unclosed fence with no trailing newline",
			input: "intro\n```\ndangling",
			want: func(in string) []fenceRange {
				start := strings.Index(in, "```")
				return []fenceRange{{start: start, end: len(in)}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanFenceRanges([]byte(tt.input))
			assert.Equal(t, tt.want(tt.input), got)
		})
	}
}

func TestInsideAnyFence(t *testing.T) {
	ranges := []fenceRange{
		{start: 10, end: 30},
		{start: 50, end: 70},
	}

	cases := []struct {
		pos  int
		want bool
	}{
		{5, false},
		{10, true},
		{20, true},
		{29, true},
		{30, false},
		{40, false},
		{50, true},
		{60, true},
		{70, false},
		{80, false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, insideAnyFence(c.pos, ranges), "pos=%d", c.pos)
	}
}

func TestProcessIncludes(t *testing.T) {
	t.Run("skips directives inside code blocks", func(t *testing.T) {
		templates := template.New("test")
		_, err := templates.New("present").Parse("EXPANDED")
		assert.NoError(t, err)

		input := "Some text before\n\n" +
			"```\n" +
			"<!-- Include: example.md -->\n" +
			"```\n\n" +
			"Some text after\n" +
			"<!-- Include: present.md -->\n" +
			"End"

		_, result, recurse, err := ProcessIncludes(".", "", []byte(input), templates)
		assert.NoError(t, err)
		assert.True(t, recurse)

		resultStr := string(result)
		assert.Contains(t, resultStr, "<!-- Include: example.md -->", "directive in code block must be preserved")
		assert.Contains(t, resultStr, "```\n<!-- Include: example.md -->\n```", "code block structure must be intact")
		assert.Contains(t, resultStr, "EXPANDED", "directive outside code block must be expanded")
		assert.NotContains(t, resultStr, "<!-- Include: present.md -->", "expanded directive must be gone")
	})

	t.Run("processes directives outside code blocks", func(t *testing.T) {
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
	})

	t.Run("self-documentation page leaves directives alone", func(t *testing.T) {
		// Reproducer for https://github.com/kovetskiy/mark/issues/717: an
		// Include directive inside a code block (e.g. self-documenting Mark
		// usage) must not be processed.
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
		assert.False(t, recurse, "no includes should have been processed")

		resultStr := string(result)
		assert.Contains(t, resultStr, "<!-- Include: shared/disclaimer.md -->")
		assert.Contains(t, resultStr, "```\n<!-- Space: EXMPLSPC -->")
	})

	t.Run("mixed in and out of code blocks", func(t *testing.T) {
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
		assert.True(t, recurse, "outside-fence directive should have been processed")

		resultStr := string(result)
		assert.Contains(t, resultStr, "REAL INCLUDED CONTENT")
		assert.Contains(t, resultStr, "<!-- Include: example-in-codeblock.md -->")
	})
}
