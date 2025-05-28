package d2

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"

	"github.com/kovetskiy/mark/attachment"
	"github.com/reconquest/pkg/log"

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

	log.Debugf(nil, "Rendering: %q", title)
	pngBytes, boxModel, err := convertSVGtoPNG(ctx, out, scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	checkSum, err := attachment.GetChecksum(bytes.NewReader(d2Diagram))
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

func convertSVGtoPNG(ctx context.Context, svg []byte, scale float64) (png []byte, m *dom.BoxModel, err error) {
	var (
		result []byte
		model  *dom.BoxModel
	)
	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()

	err = chromedp.Run(ctx,
		chromedp.Navigate(fmt.Sprintf("data:image/svg+xml;base64,%s", base64.StdEncoding.EncodeToString(svg))),
		chromedp.ScreenshotScale(`document.querySelector("svg > svg")`, scale, &result, chromedp.ByJSPath),
		chromedp.Dimensions(`document.querySelector("svg > svg")`, &model, chromedp.ByJSPath),
	)
	if err != nil {
		return nil, nil, err
	}
	return result, model, err
}
