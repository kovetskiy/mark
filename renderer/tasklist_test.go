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
