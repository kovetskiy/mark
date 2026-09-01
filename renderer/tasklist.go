package renderer

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
	ext_ast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// ConfluenceTaskListRenderer renders GFM task lists as Confluence ac:task-list elements.
type ConfluenceTaskListRenderer struct {
	html.Config
	taskID int
}

func NewConfluenceTaskListRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceTaskListRenderer{
		Config: html.NewConfig(),
	}
}

func (r *ConfluenceTaskListRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindList, r.renderList)
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ext_ast.KindTaskCheckBox, r.renderTaskCheckBox)
}

// isTaskList returns true only if every top-level list item is a task item.
func isTaskList(list *ast.List) bool {
	hasChildren := false
	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		hasChildren = true
		if !isTaskItem(child) {
			return false
		}
	}
	return hasChildren
}

// isTaskItem reports whether a list item carries a checkbox.
func isTaskItem(item ast.Node) bool {
	return getTaskCheckBox(item) != nil
}

// splitsIntoRuns reports whether a list holds both kinds of item and can be
// published as one run of each.
//
// Mixing <ac:task> and <li> inside one container is not something the storage
// format allows, so a list with an explanatory bullet among its checkboxes used
// to fall back wholesale: every item became an <li> and every checkbox became
// the literal text "[x]", losing every task on the page. Splitting the list at
// the boundaries -- a task list per run of checkboxes, a plain list per run of
// bullets -- keeps both, and is what the author wrote anyway.
//
// Only unordered lists. Splitting an ordered one restarts its numbering at each
// run, and quietly renumbering somebody's list is worse than the fallback.
func splitsIntoRuns(list *ast.List) bool {
	if list.IsOrdered() {
		return false
	}

	var tasks, plain bool
	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		if isTaskItem(child) {
			tasks = true
		} else {
			plain = true
		}
	}

	return tasks && plain
}

// rendersAsTask reports whether an item is published as an ac:task, which is
// true in a list of nothing but tasks and in the task runs of a split one.
func rendersAsTask(item ast.Node) bool {
	if !isTaskItem(item) {
		return false
	}

	list, ok := item.Parent().(*ast.List)
	if !ok {
		return false
	}

	return isTaskList(list) || splitsIntoRuns(list)
}

// opensRun reports whether an item begins a run of its own kind, and closesRun
// whether it ends one. A run's container is written by its first and last item,
// since the list itself spans several.
func opensRun(item ast.Node) bool {
	previous := item.PreviousSibling()

	return previous == nil || isTaskItem(previous) != isTaskItem(item)
}

func closesRun(item ast.Node) bool {
	next := item.NextSibling()

	return next == nil || isTaskItem(next) != isTaskItem(item)
}

// getTaskCheckBox returns the TaskCheckBox node for a ListItem, or nil if not a task item.
// The structure is: ListItem -> TextBlock -> TaskCheckBox
func getTaskCheckBox(item ast.Node) *ext_ast.TaskCheckBox {
	fc := item.FirstChild()
	if fc == nil {
		return nil
	}
	gfc := fc.FirstChild()
	if gfc == nil {
		return nil
	}
	checkbox, ok := gfc.(*ext_ast.TaskCheckBox)
	if !ok {
		return nil
	}
	return checkbox
}

func (r *ConfluenceTaskListRenderer) renderList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.List)

	// A split list has no container of its own: each run opens and closes one,
	// written by the item that begins and the item that ends it.
	if splitsIntoRuns(n) {
		return ast.WalkContinue, nil
	}

	if !isTaskList(n) {
		return r.goldmarkRenderList(w, source, node, entering)
	}
	if entering {
		// The counter is deliberately not reset here. It is per document, not per
		// list: resetting on entering every task list made a second list restart
		// at 1, and a nested list collide with its own parent, so one page carried
		// several tasks with the same ac:task-id.
		_, _ = w.WriteString("<ac:task-list>\n")
	} else {
		_, _ = w.WriteString("</ac:task-list>\n")
	}
	return ast.WalkContinue, nil
}

func (r *ConfluenceTaskListRenderer) renderListItem(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	parentList, _ := node.Parent().(*ast.List)
	if parentList == nil {
		return r.goldmarkRenderListItem(w, source, node, entering)
	}

	// In a split list the item carries its run's container.
	if splitsIntoRuns(parentList) {
		return r.renderRunItem(w, source, node, entering)
	}

	checkbox := getTaskCheckBox(node)
	if checkbox == nil || !isTaskList(parentList) {
		return r.goldmarkRenderListItem(w, source, node, entering)
	}

	if entering {
		r.writeTaskOpening(w, checkbox)
	} else {
		_, _ = w.WriteString("</ac:task-body>\n</ac:task>\n")
	}
	return ast.WalkContinue, nil
}

// renderRunItem publishes one item of a list that holds both kinds, opening the
// run's container before the first item of the run and closing it after the
// last.
func (r *ConfluenceTaskListRenderer) renderRunItem(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	task := isTaskItem(node)

	if entering && opensRun(node) {
		if task {
			_, _ = w.WriteString("<ac:task-list>\n")
		} else {
			_, _ = w.WriteString("<ul>\n")
		}
	}

	if task {
		if entering {
			r.writeTaskOpening(w, getTaskCheckBox(node))
		} else {
			_, _ = w.WriteString("</ac:task-body>\n</ac:task>\n")
		}
	} else if _, err := r.goldmarkRenderListItem(w, source, node, entering); err != nil {
		return ast.WalkStop, err
	}

	if !entering && closesRun(node) {
		if task {
			_, _ = w.WriteString("</ac:task-list>\n")
		} else {
			_, _ = w.WriteString("</ul>\n")
		}
	}

	return ast.WalkContinue, nil
}

// writeTaskOpening writes everything an ac:task needs before its body.
func (r *ConfluenceTaskListRenderer) writeTaskOpening(w util.BufWriter, checkbox *ext_ast.TaskCheckBox) {
	r.taskID++

	status := "incomplete"
	if checkbox != nil && checkbox.IsChecked {
		status = "complete"
	}

	_, _ = fmt.Fprintf(
		w,
		"<ac:task>\n<ac:task-id>%d</ac:task-id>\n<ac:task-status>%s</ac:task-status>\n<ac:task-body>",
		r.taskID, status,
	)
}

// renderTaskCheckBox skips checkbox rendering when inside an ac:task-list (status
// is already encoded by renderListItem). For any other list (e.g. mixed lists that
// fall back to plain <ul>/<ol>), a textual "[x]"/"[ ]" marker is emitted so that
// completion state is not silently lost.
func (r *ConfluenceTaskListRenderer) renderTaskCheckBox(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	// Traverse up: TaskCheckBox -> TextBlock -> ListItem
	if block := node.Parent(); block != nil {
		if item := block.Parent(); item != nil && rendersAsTask(item) {
			// Status is encoded by renderListItem; nothing to emit here.
			return ast.WalkSkipChildren, nil
		}
	}
	// Fallback: emit a textual marker so completion state is preserved.
	if entering {
		checkbox := node.(*ext_ast.TaskCheckBox)
		if checkbox.IsChecked {
			_, _ = w.WriteString("[x] ")
		} else {
			_, _ = w.WriteString("[ ] ")
		}
	}
	return ast.WalkSkipChildren, nil
}

// goldmarkRenderList is the default list rendering from goldmark.
func (r *ConfluenceTaskListRenderer) goldmarkRenderList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.List)
	tag := "ul"
	if n.IsOrdered() {
		tag = "ol"
	}
	if entering {
		_ = w.WriteByte('<')
		_, _ = w.WriteString(tag)
		if n.IsOrdered() && n.Start != 1 {
			_, _ = fmt.Fprintf(w, " start=\"%d\"", n.Start)
		}
		if n.Attributes() != nil {
			html.RenderAttributes(w, n, html.ListAttributeFilter)
		}
		_, _ = w.WriteString(">\n")
	} else {
		_, _ = w.WriteString("</")
		_, _ = w.WriteString(tag)
		_, _ = w.WriteString(">\n")
	}
	return ast.WalkContinue, nil
}

// goldmarkRenderListItem is the default list item rendering from goldmark.
func (r *ConfluenceTaskListRenderer) goldmarkRenderListItem(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		if node.Attributes() != nil {
			_, _ = w.WriteString("<li")
			html.RenderAttributes(w, node, html.ListItemAttributeFilter)
			_ = w.WriteByte('>')
		} else {
			_, _ = w.WriteString("<li>")
		}
		fc := node.FirstChild()
		if fc != nil {
			if _, ok := fc.(*ast.TextBlock); !ok {
				_ = w.WriteByte('\n')
			}
		}
	} else {
		_, _ = w.WriteString("</li>\n")
	}
	return ast.WalkContinue, nil
}
