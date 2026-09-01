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

// renderTaskList compiles with the task-list extension and mark's renderer,
// which is the pair every test here needs.
func renderTaskList(t *testing.T, input string) string {
	t.Helper()

	gm := goldmark.New(
		goldmark.WithExtensions(extension.TaskList),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(
				util.Prioritized(crenderer.NewConfluenceTaskListRenderer(), 100),
			),
		),
	)

	var buf bytes.Buffer
	require.NoError(t, gm.Convert([]byte(input), &buf))

	return buf.String()
}

// TestMixedListSplitsIntoRuns is what this replaced. Mixing <ac:task> and <li>
// in one container is not something the storage format allows, so a list with
// one explanatory bullet among its checkboxes fell back wholesale: every task
// became an <li> and every checkbox the literal text "[x]". A checklist with a
// note in it lost every task on the page.
func TestMixedListSplitsIntoRuns(t *testing.T) {
	actual := renderTaskList(t, "- [x] done\n- plain bullet\n- [ ] todo\n")
	assertWellFormed(t, actual)

	assert.Equal(t, 2, strings.Count(actual, "<ac:task-list>"), "one per run of tasks")
	assert.Equal(t, 1, strings.Count(actual, "<ul>"), "and one for the bullet between them")

	assert.Contains(t, actual, "<ac:task-status>complete</ac:task-status>")
	assert.Contains(t, actual, "<ac:task-status>incomplete</ac:task-status>")
	assert.Contains(t, actual, "<li>plain bullet</li>")

	assert.NotContains(t, actual, "[x]", "no task is published as its marker")
	assert.NotContains(t, actual, "[ ]")
}

// TestMixedListTaskIdsStayUnique: the ids number the tasks on the page, and a
// split list must not restart or repeat them.
func TestMixedListTaskIdsStayUnique(t *testing.T) {
	actual := renderTaskList(t, "- [x] one\n- note\n- [ ] two\n- note\n- [ ] three\n")
	assertWellFormed(t, actual)

	for _, id := range []string{"<ac:task-id>1<", "<ac:task-id>2<", "<ac:task-id>3<"} {
		assert.Equal(t, 1, strings.Count(actual, id), id)
	}
}

// TestOrderedMixedListIsNotSplit: splitting an ordered list restarts its
// numbering at every run, and renumbering somebody's list quietly is worse than
// the fallback that keeps the state as text.
func TestOrderedMixedListIsNotSplit(t *testing.T) {
	actual := renderTaskList(t, "1. [x] done\n1. plain\n1. [ ] todo\n")
	assertWellFormed(t, actual)

	assert.Contains(t, actual, "<ol>")
	assert.NotContains(t, actual, "<ac:task-list>")
	assert.Contains(t, actual, "[x]", "completion state is kept as text")
}

// TestPureListsAreUnchanged is the control for both shapes that already worked.
func TestPureListsAreUnchanged(t *testing.T) {
	tasks := renderTaskList(t, "- [x] one\n- [ ] two\n")
	assertWellFormed(t, tasks)
	assert.Equal(t, 1, strings.Count(tasks, "<ac:task-list>"))
	assert.NotContains(t, tasks, "<ul>")

	plain := renderTaskList(t, "- one\n- two\n")
	assertWellFormed(t, plain)
	assert.Equal(t, 1, strings.Count(plain, "<ul>"))
	assert.NotContains(t, plain, "ac:task")
}
