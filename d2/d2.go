package d2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/rs/zerolog/log"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

var renderTimeout = 120 * time.Second

func ProcessD2(title string, d2Diagram []byte, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return attachment.Attachment{}, err
	}
	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(5)),
		ThemeID: &d2themescatalog.GrapeSoda.ID,
	}
	compileOpts := &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}

	diagram, _, err := d2lib.Compile(ctx, string(d2Diagram), compileOpts, renderOpts)
	if err != nil {
		return attachment.Attachment{}, err
	}

	out, err := d2svg.Render(diagram, renderOpts)
	if err != nil {
		return attachment.Attachment{}, err
	}

	log.Debug().Msgf("Rendering: %q", title)
	pngBytes, boxModel, err := convertSVGtoPNG(ctx, out, scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	scaleAsBytes := make([]byte, 8)

	binary.LittleEndian.PutUint64(scaleAsBytes, math.Float64bits(scale))

	d2Bytes := append(d2Diagram, scaleAsBytes...)

	checkSum, err := attachment.GetChecksum(bytes.NewReader(d2Bytes))

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

var (
	chromeCtx       context.Context
	chromeCtxCancel context.CancelFunc
	chromeMutex     sync.Mutex
)

func getChromeCtx(ctx context.Context) (context.Context, error) {
	chromeMutex.Lock()
	defer chromeMutex.Unlock()

	if chromeCtx != nil {
		return chromeCtx, nil
	}

	opts := chromedp.DefaultExecAllocatorOptions[:]
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		opts = append(opts,
			chromedp.DisableGPU,
			chromedp.NoSandbox,
			chromedp.NoFirstRun,
			chromedp.NoDefaultBrowserCheck,
			chromedp.Headless,
		)
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	cCtx, cCancel := chromedp.NewContext(allocCtx)

	err := chromedp.Run(cCtx)
	if err != nil {
		cCancel()
		allocCancel()
		return nil, err
	}

	chromeCtx = cCtx
	chromeCtxCancel = func() {
		cCancel()
		allocCancel()
	}
	return chromeCtx, nil
}

func convertSVGtoPNG(ctx context.Context, svg []byte, scale float64) (png []byte, m *dom.BoxModel, err error) {
	var (
		result []byte
		model  *dom.BoxModel
	)

	cCtx, err := getChromeCtx(ctx)
	if err != nil {
		return nil, nil, err
	}

	runCtx, runCancel := context.WithTimeout(cCtx, renderTimeout)
	defer runCancel()

	err = chromedp.Run(runCtx,
		chromedp.Navigate(fmt.Sprintf("data:image/svg+xml;base64,%s", base64.StdEncoding.EncodeToString(svg))),
		chromedp.ScreenshotScale(`document.querySelector("svg > svg")`, scale, &result, chromedp.ByJSPath),
		chromedp.Dimensions(`document.querySelector("svg > svg")`, &model, chromedp.ByJSPath),
	)
	if err != nil {
		return nil, nil, err
	}
	return result, model, err
}

func Cleanup() {
	chromeMutex.Lock()
	defer chromeMutex.Unlock()

	if chromeCtxCancel != nil {
		chromeCtxCancel()
		chromeCtx = nil
		chromeCtxCancel = nil
	}
}
