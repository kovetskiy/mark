package d2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
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
	"oss.terrastruct.com/d2/lib/imgbundler"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

var renderTimeout = 120 * time.Second

// markSimpleLogger adapts mark's logger to the simple Debug/Info/Error interface
// expected by d2's imgbundler.
type markSimpleLogger struct{}

func (markSimpleLogger) Debug(s string) { log.Debugf(nil, "%s", s) }
func (markSimpleLogger) Info(s string)  { log.Infof(nil, "%s", s) }
func (markSimpleLogger) Error(s string) { log.Errorf(nil, "%s", s) }

func renderD2ToSVG(ctx context.Context, d2Diagram []byte) ([]byte, error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, err
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
		return nil, err
	}

	return d2svg.Render(diagram, renderOpts)
}

func ProcessD2(title string, d2Diagram []byte, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	out, err := renderD2ToSVG(ctx, d2Diagram)
	if err != nil {
		return attachment.Attachment{}, err
	}

	log.Debugf(nil, "Rendering: %q", title)
	pngBytes, boxModel, err := convertSVGtoPNG(ctx, out, scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	scaleAsBytes := make([]byte, 8)

	binary.LittleEndian.PutUint64(scaleAsBytes, math.Float64bits(scale))

	d2Bytes := append(append([]byte{}, d2Diagram...), scaleAsBytes...)

	checkSum, err := attachment.GetChecksum(bytes.NewReader(d2Bytes))

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

func ProcessD2SVG(title string, d2Diagram []byte, bundle bool, inputPath string, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	out, err := renderD2ToSVG(ctx, d2Diagram)
	if err != nil {
		return attachment.Attachment{}, err
	}

	logger := markSimpleLogger{}

	out, err = imgbundler.BundleLocal(ctx, logger, inputPath, out, false)
	if err != nil {
		return attachment.Attachment{}, err
	}

	if bundle {
		out, err = imgbundler.BundleRemote(ctx, logger, out, false)
		if err != nil {
			return attachment.Attachment{}, err
		}
	}

	boxModel, err := parseSVGDimensions(out)
	if err != nil {
		log.Debugf(nil, "could not read svg dimensions: %v", err)
	}

	checksumSource := append([]byte{}, d2Diagram...)
	if bundle {
		checksumSource = append(checksumSource, 1)
	} else {
		checksumSource = append(checksumSource, 0)
	}

	checkSum, err := attachment.GetChecksum(bytes.NewReader(checksumSource))
	if err != nil {
		return attachment.Attachment{}, err
	}

	if title == "" {
		title = checkSum
	}

	width := ""
	height := ""
	if boxModel != nil {
		w := boxModel.width
		h := boxModel.height
		if scale > 0 {
			w *= scale
			h *= scale
		}
		width = strconv.FormatInt(int64(w), 10)
		height = strconv.FormatInt(int64(h), 10)
	}

	fileName := title + ".svg"

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  fileName,
		FileBytes: out,
		Checksum:  checkSum,
		Replace:   title,
		Width:     width,
		Height:    height,
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

type svgBox struct {
	width  float64
	height float64
}

func parseSVGDimensions(svg []byte) (*svgBox, error) {
	dec := xml.NewDecoder(bytes.NewReader(svg))

	parseLength := func(val string) (float64, error) {
		val = strings.TrimSpace(val)
		val = strings.TrimSuffix(val, "px")
		return strconv.ParseFloat(val, 64)
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}

		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "svg" {
			continue
		}

		var widthStr, heightStr, viewBoxStr string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "width":
				widthStr = attr.Value
			case "height":
				heightStr = attr.Value
			case "viewBox":
				viewBoxStr = attr.Value
			}
		}

		if widthStr != "" && heightStr != "" {
			w, err := parseLength(widthStr)
			if err != nil {
				return nil, err
			}
			h, err := parseLength(heightStr)
			if err != nil {
				return nil, err
			}
			return &svgBox{width: w, height: h}, nil
		}

		if viewBoxStr != "" {
			parts := strings.Fields(viewBoxStr)
			if len(parts) == 4 {
				w, err := strconv.ParseFloat(parts[2], 64)
				if err != nil {
					return nil, err
				}
				h, err := strconv.ParseFloat(parts[3], 64)
				if err != nil {
					return nil, err
				}
				return &svgBox{width: w, height: h}, nil
			}
		}

		return nil, fmt.Errorf("svg dimensions not found")
	}
}
