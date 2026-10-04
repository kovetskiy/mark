package renderer

import (
	"fmt"

	"github.com/kovetskiy/mark/v16/transformer"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	ext_ast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// ConfluenceTableRenderer adds a <colgroup> to a table that carries column
// widths, and leaves every other table exactly as goldmark writes it.
//
// Only the table element itself is taken over; rows and cells stay with
// goldmark's table renderer, so cell alignment keeps working.
type ConfluenceTableRenderer struct {
	table renderer.NodeRendererFunc
}

func NewConfluenceTableRenderer() renderer.NodeRenderer {
	r := &ConfluenceTableRenderer{}

	// goldmark's render function is unexported; registering its renderer into a
	// recorder is the only way to call it and stay byte-identical.
	extension.NewTableHTMLRenderer().RegisterFuncs(tableRecorder{r})

	return r
}

type tableRecorder struct{ r *ConfluenceTableRenderer }

func (rec tableRecorder) Register(kind ast.NodeKind, fn renderer.NodeRendererFunc) {
	if kind == ext_ast.KindTable {
		rec.r.table = fn
	}
}

func (r *ConfluenceTableRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ext_ast.KindTable, r.renderTable)
}

func (r *ConfluenceTableRenderer) renderTable(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	status, err := r.table(w, source, node, entering)
	if err != nil || !entering {
		return status, err
	}

	value, ok := node.AttributeString(transformer.TableWidthsAttribute)
	if !ok {
		return status, nil
	}

	widths, ok := value.([]int)
	if !ok || len(widths) == 0 {
		return status, nil
	}

	_, _ = w.WriteString("<colgroup>\n")
	for _, width := range widths {
		_, _ = fmt.Fprintf(w, "<col style=\"width: %dpx;\"/>\n", width)
	}
	_, _ = w.WriteString("</colgroup>\n")

	return status, nil
}
