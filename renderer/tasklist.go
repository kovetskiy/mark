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

func NewConfluenceTaskListRenderer() renderer.NodeRenderer {
	return &ConfluenceTaskListRenderer{
		Config: html.NewConfig(),
	}
}

func (r *ConfluenceTaskListRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindList, r.renderList)
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ext_ast.KindTaskCheckBox, r.renderTaskCheckBox)
}

// isTaskList returns true if the list contains at least one task list item.
func isTaskList(list *ast.List) bool {
	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		if getTaskCheckBox(child) != nil {
			return true
		}
	}
	return false
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
	if !isTaskList(n) {
		return r.goldmarkRenderList(w, source, node, entering)
	}
	if entering {
		r.taskID = 0
		_, _ = w.WriteString("<ac:task-list>\n")
	} else {
		_, _ = w.WriteString("</ac:task-list>\n")
	}
	return ast.WalkContinue, nil
}

func (r *ConfluenceTaskListRenderer) renderListItem(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	checkbox := getTaskCheckBox(node)
	if checkbox == nil {
		return r.goldmarkRenderListItem(w, source, node, entering)
	}
	if entering {
		r.taskID++
		status := "incomplete"
		if checkbox.IsChecked {
			status = "complete"
		}
		_, _ = fmt.Fprintf(w, "<ac:task>\n<ac:task-id>%d</ac:task-id>\n<ac:task-status>%s</ac:task-status>\n<ac:task-body>", r.taskID, status)
	} else {
		_, _ = w.WriteString("</ac:task-body>\n</ac:task>\n")
	}
	return ast.WalkContinue, nil
}

// renderTaskCheckBox skips the default checkbox rendering; the status is handled by renderListItem.
func (r *ConfluenceTaskListRenderer) renderTaskCheckBox(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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
