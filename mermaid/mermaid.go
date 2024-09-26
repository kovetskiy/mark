package mermaid

import (
	"bytes"
	"context"
	"strconv"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/mark/attachment"
	"github.com/reconquest/pkg/log"
)

var renderTimeout = 90 * time.Second

func ProcessMermaidLocally(title string, mermaidDiagram []byte, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	defer cancel()

	log.Debugf(nil, "Setting up Mermaid renderer: %q", title)
	renderer, err := mermaid.NewRenderEngine(ctx)

	if err != nil {
		return attachment.Attachment{}, err
	}

	log.Debugf(nil, "Rendering: %q", title)
	pngBytes, boxModel, err := renderer.RenderAsScaledPng(string(mermaidDiagram), scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	checkSum, err := attachment.GetChecksum(bytes.NewReader(mermaidDiagram))
	log.Debugf(nil, "Checksum: %q -> %s", title, checkSum)

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
