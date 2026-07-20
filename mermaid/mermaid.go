package mermaid

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/rs/zerolog/log"
)

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

var renderTimeout = 120 * time.Second

func resetEngine() {
	mermaidMutex.Lock()
	defer mermaidMutex.Unlock()

	if mermaidEngine != nil {
		mermaidEngine.Cancel()
		mermaidEngine = nil
	}
}

func ProcessMermaidLocally(title string, mermaidDiagram []byte, scale float64) (attachment.Attachment, error) {
	renderer, err := getMermaidEngine()
	if err != nil {
		return attachment.Attachment{}, err
	}

	log.Debug().Msgf("Rendering: %q", title)

	type renderResult struct {
		pngBytes []byte
		boxModel *mermaid.BoxModel
		err      error
	}

	ch := make(chan renderResult, 1)
	go func() {
		pngBytes, boxModel, err := renderer.RenderAsScaledPng(string(mermaidDiagram), scale)
		ch <- renderResult{pngBytes, boxModel, err}
	}()

	var pngBytes []byte
	var boxModel *mermaid.BoxModel

	select {
	case res := <-ch:
		if res.err != nil {
			return attachment.Attachment{}, res.err
		}
		pngBytes = res.pngBytes
		boxModel = res.boxModel
	case <-time.After(renderTimeout):
		log.Error().Msgf("Mermaid rendering timed out after %v, resetting engine", renderTimeout)
		resetEngine()
		return attachment.Attachment{}, fmt.Errorf("mermaid rendering timed out after %v", renderTimeout)
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
