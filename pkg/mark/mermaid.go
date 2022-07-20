package mark

import (
	"context"
	"github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/blackfriday/v2"
	"io"
	"strconv"
	"time"
)

const (
	MERMAID_LANG   = "mermaid"
	RENDER_TIMEOUT = 10 * time.Second
)

type MermaidImageFile struct {
	Bytes         []byte
	Filename      string
	Checksum      string
	Width, Height string
}

type MermaidExportRender struct {
	blackfriday.Renderer
	Attachments []MermaidImageFile
	Err         error
}

func (renderer *MermaidExportRender) RenderNode(writer io.Writer, node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
	if node.Type != blackfriday.CodeBlock || ParseLanguage(string(node.Info)) != "mermaid" {
		return renderer.Renderer.RenderNode(writer, node, entering)
	}

	var (
		re       *mermaid_go.RenderEngine
		err      error
		pngBytes []byte
		boxModel *mermaid_go.BoxModel
	)
	ctx, cancel := context.WithTimeout(context.TODO(), RENDER_TIMEOUT)
	defer cancel()
	if re, err = mermaid_go.NewRenderEngine(ctx); err != nil {
		renderer.Err = err
		return blackfriday.Terminate
	}
	if pngBytes, boxModel, err = re.RenderAsPng(string(node.Literal)); err != nil {
		renderer.Err = err
		return blackfriday.GoToNext
	}

	checkSum := codeChecksum(node.Literal)
	fileName := ParseTitle(string(node.Info))
	if fileName == "" {
		fileName = checkSum
	}
	fileName = fileName + ".png"

	renderer.Attachments = append(renderer.Attachments, MermaidImageFile{
		Bytes:    pngBytes,
		Filename: fileName,
		Checksum: codeChecksum(node.Literal),
		Height:   strconv.FormatInt(boxModel.Height, 10),
		Width:    strconv.FormatInt(boxModel.Width, 10),
	})
	return blackfriday.GoToNext
}
func ExtractMermaidImage(markdown []byte) ([]MermaidImageFile, error) {
	renderer := &MermaidExportRender{
		Renderer: blackfriday.NewHTMLRenderer(
			blackfriday.HTMLRendererParameters{
				Flags: blackfriday.UseXHTML |
					blackfriday.Smartypants |
					blackfriday.SmartypantsFractions |
					blackfriday.SmartypantsDashes |
					blackfriday.SmartypantsLatexDashes,
			},
		),
		Attachments: []MermaidImageFile{},
	}

	_ = blackfriday.Run(
		markdown,
		blackfriday.WithRenderer(renderer),
		blackfriday.WithExtensions(
			blackfriday.NoIntraEmphasis|
				blackfriday.Tables|
				blackfriday.FencedCode|
				blackfriday.Autolink|
				blackfriday.LaxHTMLBlocks|
				blackfriday.Strikethrough|
				blackfriday.SpaceHeadings|
				blackfriday.HeadingIDs|
				blackfriday.AutoHeadingIDs|
				blackfriday.Titleblock|
				blackfriday.BackslashLineBreak|
				blackfriday.DefinitionLists|
				blackfriday.NoEmptyLineBeforeBlock|
				blackfriday.Footnotes,
		),
	)
	return renderer.Attachments, renderer.Err
}

func ConvertToAttachments(files []MermaidImageFile) []Attachment {
	attaches := make([]Attachment, len(files))
	for n, f := range files {
		attaches[n] = Attachment{
			ID:        "",
			Name:      f.Filename,
			Filename:  f.Filename,
			FileBytes: f.Bytes,
			Checksum:  f.Checksum,
			Replace:   f.Filename,
			Width:     f.Width,
			Height:    f.Height,
		}
	}
	return attaches
}
