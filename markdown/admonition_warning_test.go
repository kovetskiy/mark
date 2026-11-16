package markdown

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compileWarned compiles a document at path with the admonition feature on
// and returns the page and the warnings of the kind an admonition published
// without its body is given.
func compileWarned(t *testing.T, src, path string, features ...string) (string, []report.Warning) {
	t.Helper()

	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	warnings := report.NewWarnings(path)

	out, _, err := CompileMarkdown(t.Context(), []byte(src), std, path, Options{Features: features, Warnings: warnings})
	require.NoError(t, err)

	var found []report.Warning
	for _, warning := range warnings.List() {
		if warning.Kind == report.WarningAdmonitionBody {
			found = append(found, warning)
		}
	}

	return out, found
}

// TestAdmonitionBodyLeftOutIsAWarning: lines written straight after an
// opening line, not indented and not ended by a closing line, are published
// after the admonition. They were in it whenever the document ended with
// them, so the author is told, with the line and what to do about it.
func TestAdmonitionBodyLeftOutIsAWarning(t *testing.T) {
	out, warnings := compileWarned(t, "Intro.\n\n!!! note \"Remember\"\nIn the box, as written.\n", "docs/page.md", "mkdocsadmonitions")

	assert.Contains(t, out, "</ac:structured-macro>\n<p>In the box, as written.</p>", "the page is still published")

	require.Len(t, warnings, 1)
	assert.Equal(t, report.Warning{
		Kind: report.WarningAdmonitionBody,
		File: "docs/page.md",
		Line: 3,
		Message: `the lines after "!!! note \"Remember\"" are not indented and no line of "!!!" ends them, ` +
			`so they are published after the admonition and not in it; indent them under it, or end them with a line of "!!!"`,
	}, warnings[0])
}

// TestAdmonitionBodyWarningIsForTheRightAdmonitions: one warning for each
// admonition that lost the lines written as its body, and none for an
// admonition that has no body to lose, has an indented one, or is closed.
func TestAdmonitionBodyWarningIsForTheRightAdmonitions(t *testing.T) {
	tests := []struct {
		name   string
		source string
		lines  []int
	}{
		{name: "unindented line straight after", source: "!!! tip\nbody\n\nlater\n", lines: []int{1}},
		{name: "two of them", source: "!!! tip\nbody\n\n!!! note\nmore\n", lines: []int{1, 4}},
		{name: "another admonition straight after", source: "!!! note \"one\"\n!!! tip \"two\"\n    b\n", lines: []int{1}},
		{name: "in a list item", source: "- one\n- !!! note\n  in the item\n- three\n", lines: []int{2}},
		{name: "in a blockquote", source: "> !!! note\n> quoted\n", lines: []int{1}},
		{name: "in an indented body", source: "!!! note\n    !!! tip\n    a\n", lines: []int{2}},
		{
			// The run closes the admonition with the indented body, and the
			// one around it has none of its own.
			name:   "a closing run that an indented body inside it takes",
			source: "!!! note \"outer\"\nintro\n!!! tip \"inner\"\n    b\n!!!\n\nafter\n",
			lines:  []int{1},
		},
		{
			name:   "two with one closing run between them",
			source: "!!! note \"outer\"\na\n!!! tip \"inner\"\nb\n!!!\nc\n",
			lines:  []int{1},
		},
		{name: "a run too short to close it", source: "!!!! note\nbody\n!!!\n", lines: []int{1}},

		{name: "the last thing in the document", source: "text\n\n!!! note \"T\"\n"},
		{name: "without a newline", source: "!!! note \"T\""},
		{name: "followed by blank lines only", source: "!!! note \"T\"\n\n\n"},
		{name: "the last thing in a list item", source: "- !!! note \"T\"\n- next\n"},
		{name: "the last thing in a blockquote", source: "> !!! note \"T\"\n\nafter\n"},
		{name: "a blank line, then the rest of the document", source: "!!! note \"T\"\n\nNext paragraph.\n"},
		{name: "indented body", source: "!!! note \"T\"\n    body\n\nafter\n"},
		{name: "indented body after a blank line", source: "!!! note \"T\"\n\n    body\n"},
		{name: "closed by a run", source: "!!! note \"T\"\nbody\n!!!\nafter\n"},
		{name: "closed by a run straight after", source: "!!! note \"T\"\n!!!\nafter\n"},
		{name: "nested, each closed by its run", source: "!!! note\na\n!!! tip\nb\n!!!\nc\n!!!\n"},
		{name: "ended by a line indented less than it", source: "  !!! note\nbody\n"},
		{name: "closed by a run after a line that lacks the list item's indentation", source: "- !!! note\n  text\nlazy\n  !!!\n"},
		{
			name:   "nested, the inner body going on after a line level with it, each closed by its run",
			source: "!!! note\nintro\n!!! tip\n    b\nlazy\n\n    c\n!!!\nstill outer\n!!!\nafter\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, warnings := compileWarned(t, tt.source, "test.md", "mkdocsadmonitions")

			var lines []int
			for _, warning := range warnings {
				lines = append(lines, warning.Line)
			}

			assert.Equal(t, tt.lines, lines, "%v", warnings)
		})
	}
}

// TestAdmonitionBodyWarningNeedsTheFeature: without the feature the line is a
// paragraph like any other, and nothing is said about it.
func TestAdmonitionBodyWarningNeedsTheFeature(t *testing.T) {
	out, warnings := compileWarned(t, "!!! note\nbody\n", "test.md")

	assert.Equal(t, "<p>!!! note\nbody</p>\n", out)
	assert.Empty(t, warnings)
}

// TestAdmonitionBodyWarningQuotesTheOpeningLine: the warning names the
// closing line by the run the admonition was opened with, and quotes no more
// of a long opening line than says which it is.
func TestAdmonitionBodyWarningQuotesTheOpeningLine(t *testing.T) {
	_, warnings := compileWarned(t,
		"!!!! warning \""+strings.Repeat("long ", 20)+"title\"\nbody\n", "test.md", "mkdocsadmonitions")

	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, `the lines after "!!!! warning \"long long long long long long long long long l..." are`)
	assert.Contains(t, warnings[0].Message, `no line of "!!!!" ends them`)
}

// TestAdmonitionBodyWarningNamesTheLineWritten: what is parsed has had its
// macro definitions taken out and its includes put in, and the warning still
// names the line of the text that was given. Where an include holds a line
// that reads like the opening line, which of the two it is cannot be told, and
// the warning names no line.
func TestAdmonitionBodyWarningNamesTheLineWritten(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"box.md":  "<b>{{ .Text }}</b>\n",
		"frag.md": "included\n\nlines\n",
		"same.md": "!!! note\n    with a body\n",
	})

	_, warnings := compileWarned(t,
		"<!-- Macro: BOX-([0-9]+)\n     Template: box.md\n     Text: ${1} -->\n\nBOX-7\n\n<!-- Include: frag.md -->\n\n!!! note\nbody\n",
		filepath.Join(dir, "doc.md"), "mkdocsadmonitions")

	require.Len(t, warnings, 1)
	assert.Equal(t, 9, warnings[0].Line)

	_, warnings = compileWarned(t,
		"<!-- Include: same.md -->\n\n!!! note\nbody\n", filepath.Join(dir, "doc.md"), "mkdocsadmonitions")

	require.Len(t, warnings, 1)
	assert.Zero(t, warnings[0].Line)
	assert.Contains(t, warnings[0].Message, `"!!! note"`)
}

// TestAdmonitionBodyWarningsTakeLinearMemory: each warning finds its line in
// the text written, which is read once for all of them, not once for each.
func TestAdmonitionBodyWarningsTakeLinearMemory(t *testing.T) {
	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	allocated := func(admonitions int) uint64 {
		var source strings.Builder
		for i := range admonitions {
			fmt.Fprintf(&source, "!!! note \"T%d\"\nprose %d\n\n", i, i)
		}

		warnings := report.NewWarnings("test.md")

		var before, after runtime.MemStats

		runtime.GC()
		runtime.ReadMemStats(&before)

		_, _, err := CompileMarkdown(t.Context(), []byte(source.String()), std, "test.md",
			Options{Features: []string{"mkdocsadmonitions"}, Warnings: warnings})

		runtime.ReadMemStats(&after)
		require.NoError(t, err)
		require.Len(t, warnings.List(), admonitions)

		return after.TotalAlloc - before.TotalAlloc
	}

	small, large := allocated(500), allocated(2000)

	// Four times the document takes about four times the memory; reading it
	// again for each warning takes sixteen.
	assert.Less(t, large, 8*small, "%d bytes for 500 admonitions, %d for 2000", small, large)
}
