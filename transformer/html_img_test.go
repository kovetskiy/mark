package transformer

import (
	"testing"

	"github.com/kovetskiy/mark/v16/internal/goldmarktest"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/util"
)

func TestHTMLImgTransformer_BlockImage(t *testing.T) {
	markdown := []byte(`<img src="https://example.com/logo.png" width="600" height="400" alt="Logo" title="My Logo">`)
	md := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](NewHTMLImgTransformer(), 100),
			),
		),
	)

	doc := md.Parse(markdown)

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

	if foundImg.Destination.Str(markdown) != "https://example.com/logo.png" {
		t.Errorf("destination = %q, want https://example.com/logo.png", foundImg.Destination.Str(markdown))
	}

	if foundImg.Title.Str(markdown) != "My Logo" {
		t.Errorf("title = %q, want My Logo", foundImg.Title.Str(markdown))
	}

	widthAttr, ok := AttributeText(foundImg, "width", markdown)
	if !ok || widthAttr != "600" {
		t.Errorf("width attribute = %v, want 600", widthAttr)
	}

	heightAttr, ok := AttributeText(foundImg, "height", markdown)
	if !ok || heightAttr != "400" {
		t.Errorf("height attribute = %v, want 400", heightAttr)
	}
}

func TestHTMLImgTransformer_InlineImage(t *testing.T) {
	markdown := []byte(`Here is an inline image <img src="local.png" width="200" alt="Inline"> in paragraph.`)
	md := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](NewHTMLImgTransformer(), 100),
			),
		),
	)

	doc := md.Parse(markdown)

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

	if foundImg.Destination.Str(markdown) != "local.png" {
		t.Errorf("destination = %q, want local.png", foundImg.Destination.Str(markdown))
	}

	widthAttr, ok := AttributeText(foundImg, "width", markdown)
	if !ok || widthAttr != "200" {
		t.Errorf("width attribute = %v, want 200", widthAttr)
	}
}

func TestHTMLImgTransformer_MultilineImage(t *testing.T) {
	markdown := []byte("<img\n  src=\"hero.png\"\n  width=\"800\"\n  alt=\"Hero\"\n/>")
	md := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](NewHTMLImgTransformer(), 100),
			),
		),
	)

	doc := md.Parse(markdown)

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

	if foundImg.Destination.Str(markdown) != "hero.png" {
		t.Errorf("destination = %q, want hero.png", foundImg.Destination.Str(markdown))
	}

	widthAttr, ok := AttributeText(foundImg, "width", markdown)
	if !ok || widthAttr != "800" {
		t.Errorf("width attribute = %v, want 800", widthAttr)
	}
}

func TestHTMLImgTransformer_StyleAttribute(t *testing.T) {
	markdown := []byte(`<img src="styled.png" style="width: 350px; height: 175px; float: left">`)
	md := goldmarktest.New(
		goldmarktest.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized[parser.ASTTransformer](NewHTMLImgTransformer(), 100),
			),
		),
	)

	doc := md.Parse(markdown)

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

	widthAttr, ok := AttributeText(foundImg, "width", markdown)
	if !ok || widthAttr != "350" {
		t.Errorf("width attribute = %v, want 350", widthAttr)
	}

	heightAttr, ok := AttributeText(foundImg, "height", markdown)
	if !ok || heightAttr != "175" {
		t.Errorf("height attribute = %v, want 175", heightAttr)
	}

	alignAttr, ok := AttributeText(foundImg, "align", markdown)
	if !ok || alignAttr != "left" {
		t.Errorf("align attribute = %v, want left", alignAttr)
	}
}

// splitImages only ever takes the <img> tags out: put back in place of each
// image, the pieces are the input byte for byte, including a tag left
// unterminated at the end and a CDATA section the tokenizer would misread.
func TestSplitImagesKeepsTheMarkupAroundTheTags(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{
			raw:  "<p align=\"center\">\n  <img src=\"a.png\" width=\"200\">\n</p>\n",
			want: "<p align=\"center\">\n  [a.png]\n</p>\n",
		},
		{
			raw:  "<div><IMG SRC=\"a.png\"/><!-- <img src=\"c.png\"> --><img src=\"b.png\"></div>",
			want: "<div>[a.png]<!-- <img src=\"c.png\"> -->[b.png]</div>",
		},
		{
			raw:  "<div><![CDATA[ <img src=\"x.png\"> ]]><img src=\"a.png\"><img alt=\"no src\"><span",
			want: "<div><![CDATA[ <img src=\"x.png\"> ]]>[a.png]<img alt=\"no src\"><span",
		},
		{
			raw:  "<picture><img src=\"x.png\"></picture><img src=\"a.png\">",
			want: "<picture><img src=\"x.png\"></picture>[a.png]",
		},
	}

	for _, tt := range tests {
		pieces := NewHTMLImgTransformer().splitImages([]byte(tt.raw))
		if pieces == nil {
			t.Fatalf("%q: no pieces", tt.raw)
		}

		var got []byte
		for _, piece := range pieces {
			if img, ok := piece.(*ast.Image); ok {
				got = append(got, "["+img.Destination.Str(nil)+"]"...)
				continue
			}

			markup, _ := AttributeText(piece, ReplacementContentAttribute, nil)
			got = append(got, markup...)
		}

		if string(got) != tt.want {
			t.Errorf("%q:\n got %q\nwant %q", tt.raw, got, tt.want)
		}
	}

	if pieces := NewHTMLImgTransformer().splitImages([]byte("<div><img alt=\"x\"></div>")); pieces != nil {
		t.Errorf("a block with no convertible <img> is left alone, got %d pieces", len(pieces))
	}
}
