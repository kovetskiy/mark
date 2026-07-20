package mermaid

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"strconv"
	"sync"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/rs/zerolog/log"
)

var renderTimeout = 120 * time.Second

var (
	mermaidEngine *mermaid.RenderEngine
	mermaidMutex  sync.Mutex
)

func getMermaidEngine() (*mermaid.RenderEngine, error) {
	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidEngine != nil {
		return mermaidEngine, nil
	}

	log.Debug().Msg("Setting up global Mermaid renderer")
	engine, err := mermaid.NewRenderEngine(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	mermaidEngine = engine
	return mermaidEngine, nil
}

func ProcessMermaidLocally(title string, mermaidDiagram []byte, scale float64) (attachment.Attachment, error) {
	renderer, err := getMermaidEngine()
	if err != nil {
		return attachment.Attachment{}, err
	}

	log.Debug().Msgf("Rendering: %q", title)
	pngBytes, boxModel, err := renderer.RenderAsScaledPng(string(mermaidDiagram), scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	scaleAsBytes := make([]byte, 8)

	binary.LittleEndian.PutUint64(scaleAsBytes, math.Float64bits(scale))

	mermaidBytes := append(mermaidDiagram, scaleAsBytes...)

	checkSum, err := attachment.GetChecksum(bytes.NewReader(mermaidBytes))
	log.Debug().Msgf("Checksum: %q -> %s", title, checkSum)

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

func Cleanup() {
	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidEngine != nil {
		mermaidEngine.Cancel()
		mermaidEngine = nil
	}
}
