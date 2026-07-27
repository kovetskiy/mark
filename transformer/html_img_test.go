package transformer

import (
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func TestHTMLImgTransformer_BlockImage(t *testing.T) {
	markdown := []byte(`<img src="https://example.com/logo.png" width="600" height="400" alt="Logo" title="My Logo">`)
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(NewHTMLImgTransformer(), 100),
			),
		),
	)

	reader := text.NewReader(markdown)
	doc := md.Parser().Parse(reader)

	var foundImg *ast.Image
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if img, ok := n.(*ast.Image); ok {
				foundImg = img
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})

	if foundImg == nil {
		t.Fatalf("expected AST image node, got nil")
	}

	if string(foundImg.Destination) != "https://example.com/logo.png" {
		t.Errorf("destination = %q, want https://example.com/logo.png", string(foundImg.Destination))
	}

	if string(foundImg.Title) != "My Logo" {
		t.Errorf("title = %q, want My Logo", string(foundImg.Title))
	}

	widthAttr, ok := foundImg.Attribute([]byte("width"))
	if !ok || string(widthAttr.([]byte)) != "600" {
		t.Errorf("width attribute = %v, want 600", widthAttr)
	}

	heightAttr, ok := foundImg.Attribute([]byte("height"))
	if !ok || string(heightAttr.([]byte)) != "400" {
		t.Errorf("height attribute = %v, want 400", heightAttr)
	}
}

func TestHTMLImgTransformer_InlineImage(t *testing.T) {
	markdown := []byte(`Here is an inline image <img src="local.png" width="200" alt="Inline"> in paragraph.`)
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(NewHTMLImgTransformer(), 100),
			),
		),
	)

	reader := text.NewReader(markdown)
	doc := md.Parser().Parse(reader)

	var foundImg *ast.Image
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if img, ok := n.(*ast.Image); ok {
				foundImg = img
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})

	if foundImg == nil {
		t.Fatalf("expected AST image node for inline img tag, got nil")
	}

	if string(foundImg.Destination) != "local.png" {
		t.Errorf("destination = %q, want local.png", string(foundImg.Destination))
	}

	widthAttr, ok := foundImg.Attribute([]byte("width"))
	if !ok || string(widthAttr.([]byte)) != "200" {
		t.Errorf("width attribute = %v, want 200", widthAttr)
	}
}

func TestHTMLImgTransformer_MultilineImage(t *testing.T) {
	markdown := []byte("<img\n  src=\"hero.png\"\n  width=\"800\"\n  alt=\"Hero\"\n/>")
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(NewHTMLImgTransformer(), 100),
			),
		),
	)

	reader := text.NewReader(markdown)
	doc := md.Parser().Parse(reader)

	var foundImg *ast.Image
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if img, ok := n.(*ast.Image); ok {
				foundImg = img
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})

	if foundImg == nil {
		t.Fatalf("expected AST image node for multiline img tag, got nil")
	}

	if string(foundImg.Destination) != "hero.png" {
		t.Errorf("destination = %q, want hero.png", string(foundImg.Destination))
	}

	widthAttr, ok := foundImg.Attribute([]byte("width"))
	if !ok || string(widthAttr.([]byte)) != "800" {
		t.Errorf("width attribute = %v, want 800", widthAttr)
	}
}

func TestHTMLImgTransformer_StyleAttribute(t *testing.T) {
	markdown := []byte(`<img src="styled.png" style="width: 350px; height: 175px; float: left">`)
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(NewHTMLImgTransformer(), 100),
			),
		),
	)

	reader := text.NewReader(markdown)
	doc := md.Parser().Parse(reader)

	var foundImg *ast.Image
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if img, ok := n.(*ast.Image); ok {
				foundImg = img
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})

	if foundImg == nil {
		t.Fatalf("expected AST image node for styled img tag, got nil")
	}

	widthAttr, ok := foundImg.Attribute([]byte("width"))
	if !ok || string(widthAttr.([]byte)) != "350" {
		t.Errorf("width attribute = %v, want 350", widthAttr)
	}

	heightAttr, ok := foundImg.Attribute([]byte("height"))
	if !ok || string(heightAttr.([]byte)) != "175" {
		t.Errorf("height attribute = %v, want 175", heightAttr)
	}

	alignAttr, ok := foundImg.Attribute([]byte("align"))
	if !ok || string(alignAttr.([]byte)) != "left" {
		t.Errorf("align attribute = %v, want left", alignAttr)
	}
}
