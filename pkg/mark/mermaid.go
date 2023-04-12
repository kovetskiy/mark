package mark

import (
	"bytes"
	"context"
	"strconv"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
)

var renderTimeout = 60 * time.Second

func processMermaidLocally(title string, mermaidDiagram []byte) (attachement Attachment, err error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	defer cancel()

	renderer, err := mermaid.NewRenderEngine(ctx)

	if err != nil {
		return Attachment{}, err
	}

	pngBytes, boxModel, err := renderer.RenderAsPng(string(mermaidDiagram))
	if err != nil {
		return Attachment{}, err
	}

	checkSum, err := GetChecksum(bytes.NewReader(mermaidDiagram))

	if err != nil {
		return Attachment{}, err
	}
	if title == "" {
		title = checkSum
	}

	fileName := title + ".png"

	return Attachment{
		ID:        "",
		Name:      title,
		Filename:  fileName,
		FileBytes: pngBytes,
		Checksum:  checkSum,
		Replace:   title,
		Width:     strconv.FormatInt(boxModel.Width, 10),
		Height:    strconv.FormatInt(boxModel.Height, 10),
	}, nil
}
