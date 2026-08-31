package renderer_test

import (
	"bytes"
	"strings"
	"testing"

	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// ac:task-id must be unique within a page. The counter was reset on entering
// every task list, so a second list restarted at 1, and a nested list collided
// with its own parent.
func TestConfluenceTaskListRendererIDsAreUniquePerDocument(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "two separate task lists",
			input: "- [ ] a\n\ntext\n\n- [ ] b\n",
			want:  []string{"<ac:task-id>1</ac:task-id>", "<ac:task-id>2</ac:task-id>"},
		},
		{
			name:  "nested task list",
			input: "- [ ] outer1\n    - [x] inner\n- [ ] outer2\n",
			want: []string{
				"<ac:task-id>1</ac:task-id>",
				"<ac:task-id>2</ac:task-id>",
				"<ac:task-id>3</ac:task-id>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gm := goldmark.New(
				goldmark.WithExtensions(extension.TaskList),
				goldmark.WithRendererOptions(
					renderer.WithNodeRenderers(
						util.Prioritized(crenderer.NewConfluenceTaskListRenderer(), 100),
					),
				),
			)

			var buf bytes.Buffer
			require.NoError(t, gm.Convert([]byte(tt.input), &buf))
			got := buf.String()

			for _, w := range tt.want {
				assert.Equal(t, 1, strings.Count(got, w),
					"%s must appear exactly once in:\n%s", w, got)
			}
		})
	}
}

// TestTaskListRendererLeavesOrdinaryListsAlone covers the other half of this
// renderer's job. It claims List and ListItem for the whole document, not only
// for lists with checkboxes, so an ordinary bullet or numbered list has to come
// out as one -- an <ul> turned into an ac:task-list would put a checkbox beside
// every line of prose.
func TestTaskListRendererLeavesOrdinaryListsAlone(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "bullets",
			input: "- one\n- two\n",
			want:  []string{"<ul>", "<li>one</li>", "</ul>"},
		},
		{
			name:  "numbers keep their start",
			input: "5. five\n6. six\n",
			want:  []string{`<ol start="5">`, "<li>five</li>", "</ol>"},
		},
		{
			name:  "nesting",
			input: "- outer\n    - inner\n",
			want:  []string{"<ul>", "<li>outer", "<li>inner</li>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gm := goldmark.New(
				goldmark.WithExtensions(extension.TaskList),
				goldmark.WithRendererOptions(
					renderer.WithNodeRenderers(
						util.Prioritized(crenderer.NewConfluenceTaskListRenderer(), 100),
					),
				),
			)

			var buf bytes.Buffer
			require.NoError(t, gm.Convert([]byte(tt.input), &buf))
			got := buf.String()

			assert.NotContains(t, got, "ac:task", "an ordinary list is not a task list")
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
		})
	}
}

// TestTaskListCheckedState covers the state Confluence reads: a task list that
// loses the difference between a done and an open item is worse than no task
// list at all.
func TestTaskListCheckedState(t *testing.T) {
	gm := goldmark.New(
		goldmark.WithExtensions(extension.TaskList),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(
				util.Prioritized(crenderer.NewConfluenceTaskListRenderer(), 100),
			),
		),
	)

	var buf bytes.Buffer
	require.NoError(t, gm.Convert([]byte("- [x] done\n- [ ] open\n"), &buf))
	got := buf.String()

	assert.Contains(t, got, "<ac:task-status>complete</ac:task-status>")
	assert.Contains(t, got, "<ac:task-status>incomplete</ac:task-status>")
	assert.Equal(t, 1, strings.Count(got, "<ac:task-list>"))
}
