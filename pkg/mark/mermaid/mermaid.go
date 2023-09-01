package mermaid

import (
	"bytes"
	"context"
	"strconv"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/mark/pkg/mark/attachment"
)

var renderTimeout = 60 * time.Second

func ProcessMermaidLocally(title string, mermaidDiagram []byte, scale float64) (attachement attachment.Attachment, err error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	defer cancel()

	renderer, err := mermaid.NewRenderEngine(ctx)

	if err != nil {
		return attachment.Attachment{}, err
	}

	pngBytes, boxModel, err := renderer.RenderAsScaledPng(string(mermaidDiagram), scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	checkSum, err := attachment.GetChecksum(bytes.NewReader(mermaidDiagram))

	if err != nil {
		return attachment.Attachment{}, err
	}
	if title == "" {
		title = checkSum
	}

	fileName := title + ".png"

	return attachment.Attachment{
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
