package renderer

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceGHAlertsBlockQuoteRenderer struct {
	html.Config
	LevelMap       BlockQuoteLevelMap
	BlockQuoteNode ast.Node
}

// NewConfluenceGHAlertsBlockQuoteRenderer creates a new instance of the renderer for GitHub Alerts
func NewConfluenceGHAlertsBlockQuoteRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceGHAlertsBlockQuoteRenderer{
		Config:         html.NewConfig(),
		LevelMap:       nil,
		BlockQuoteNode: nil,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs
func (r *ConfluenceGHAlertsBlockQuoteRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindBlockquote, r.renderBlockQuote)
}

// Define GitHub Alert to Confluence macro mapping
func (r *ConfluenceGHAlertsBlockQuoteRenderer) getConfluenceMacroName(alertType string) string {
	switch alertType {
	case "note":
		return "info"
	case "tip":
		return "tip"
	case "important":
		return "info"
	case "warning":
		return "note"
	case "caution":
		return "warning"
	default:
		return "info"
	}
}

func (r *ConfluenceGHAlertsBlockQuoteRenderer) renderBlockQuote(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if r.LevelMap == nil {
		r.LevelMap = GenerateBlockQuoteLevel(node)
	}

	// Check if this blockquote has been transformed by the GHAlerts transformer
	if alertTypeBytes, hasAttribute := node.Attribute([]byte("gh-alert-type")); hasAttribute && alertTypeBytes != nil {
		if alertTypeStr, ok := alertTypeBytes.([]byte); ok {
			return r.renderGHAlert(writer, source, node, entering, string(alertTypeStr))
		}
	}

	// Fall back to legacy blockquote rendering for non-GitHub Alert blockquotes
	return r.renderLegacyBlockQuote(writer, source, node, entering)
}

func (r *ConfluenceGHAlertsBlockQuoteRenderer) renderGHAlert(writer util.BufWriter, source []byte, node ast.Node, entering bool, alertType string) (ast.WalkStatus, error) {
	quoteLevel := r.LevelMap.Level(node)

	if quoteLevel == 0 && entering {
		r.BlockQuoteNode = node
		macroName := r.getConfluenceMacroName(alertType)
		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", macroName)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}

	if quoteLevel == 0 && !entering && node == r.BlockQuoteNode {
		suffix := "</ac:rich-text-body></ac:structured-macro>\n"
		if _, err := writer.Write([]byte(suffix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}

	// For nested blockquotes or continuing the content, use default rendering
	if quoteLevel > 0 {
		if entering {
			if _, err := writer.WriteString("<blockquote>\n"); err != nil {
				return ast.WalkStop, err
			}
		} else {
			if _, err := writer.WriteString("</blockquote>\n"); err != nil {
				return ast.WalkStop, err
			}
		}
	}

	return ast.WalkContinue, nil
}

func (r *ConfluenceGHAlertsBlockQuoteRenderer) renderLegacyBlockQuote(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	// Legacy blockquote handling (same as original ParseBlockQuoteType logic)
	quoteType := ParseBlockQuoteType(node, source)
	quoteLevel := r.LevelMap.Level(node)

	if quoteLevel == 0 && entering && quoteType != None {
		r.BlockQuoteNode = node
		prefix := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:parameter ac:name=\"icon\">true</ac:parameter><ac:rich-text-body>\n", quoteType)
		if _, err := writer.Write([]byte(prefix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}

	if quoteLevel == 0 && !entering && node == r.BlockQuoteNode {
		suffix := "</ac:rich-text-body></ac:structured-macro>\n"
		if _, err := writer.Write([]byte(suffix)); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, nil
	}

	// For nested blockquotes or regular blockquotes
	if quoteLevel > 0 || (quoteLevel == 0 && quoteType == None) {
		if entering {
			if _, err := writer.WriteString("<blockquote>\n"); err != nil {
				return ast.WalkStop, err
			}
		} else {
			if _, err := writer.WriteString("</blockquote>\n"); err != nil {
				return ast.WalkStop, err
			}
		}
	}

	return ast.WalkContinue, nil
}
