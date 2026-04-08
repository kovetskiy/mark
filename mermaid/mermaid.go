package mermaid

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"math"
	"strconv"
	"strings"
	"time"

	mermaid "github.com/dreampuf/mermaid.go"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/rs/zerolog/log"
)

var renderTimeout = 120 * time.Second

func ProcessMermaidLocally(title string, mermaidDiagram []byte, scale float64) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	defer cancel()

	log.Debug().Msgf("Setting up Mermaid renderer: %q", title)
	renderer, err := mermaid.NewRenderEngine(ctx, nil)

	if err != nil {
		return attachment.Attachment{}, err
	}
	defer renderer.Cancel()

	log.Debug().Msgf("Rendering: %q", title)
	pngBytes, boxModel, err := renderer.RenderAsScaledPng(string(mermaidDiagram), scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	scaleAsBytes := make([]byte, 8)

	binary.LittleEndian.PutUint64(scaleAsBytes, math.Float64bits(scale))

	mermaidBytes := make([]byte, 0, len(mermaidDiagram)+len(scaleAsBytes))
	mermaidBytes = append(mermaidBytes, mermaidDiagram...)
	mermaidBytes = append(mermaidBytes, scaleAsBytes...)

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

// ProcessMermaidSVG renders a Mermaid diagram as a plain SVG file.
// The mermaid-scale flag is not applicable to SVG output.
func ProcessMermaidSVG(title string, mermaidDiagram []byte) (attachment.Attachment, error) {
	return processMermaidSVG(title, mermaidDiagram, false)
}

// ProcessMermaidWithBundle renders a Mermaid diagram as an SVG file with the
// original diagram source embedded in the SVG <desc> element (via
// mermaid.go's WithBundle option). The resulting attachment is an SVG rather
// than a PNG, so it is resolution-independent and the source can be recovered
// from the attachment. The mermaid-scale flag is not applicable to SVG output.
func ProcessMermaidWithBundle(title string, mermaidDiagram []byte) (attachment.Attachment, error) {
	return processMermaidSVG(title, mermaidDiagram, true)
}

func processMermaidSVG(title string, mermaidDiagram []byte, bundle bool) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), renderTimeout)
	defer cancel()

	log.Debug().Msgf("Setting up Mermaid renderer (SVG, bundle=%v): %q", bundle, title)
	renderer, err := mermaid.NewRenderEngine(ctx, nil)
	if err != nil {
		return attachment.Attachment{}, err
	}
	defer renderer.Cancel()

	log.Debug().Msgf("Rendering (SVG, bundle=%v): %q", bundle, title)
	var svgContent string
	if bundle {
		svgContent, err = renderer.Render(string(mermaidDiagram), mermaid.WithBundle())
	} else {
		svgContent, err = renderer.Render(string(mermaidDiagram))
	}
	if err != nil {
		return attachment.Attachment{}, err
	}

	checksumInput := make([]byte, 0, len(mermaidDiagram)+1)
	checksumInput = append(checksumInput, mermaidDiagram...)
	checksumInput = append(checksumInput, boolByte(bundle))
	checkSum, err := attachment.GetChecksum(bytes.NewReader(checksumInput))
	log.Debug().Msgf("Checksum: %q -> %s", title, checkSum)
	if err != nil {
		return attachment.Attachment{}, err
	}
	if title == "" {
		title = checkSum
	}

	svgWidth, svgHeight := extractSVGDimensions(svgContent)

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  title + ".svg",
		FileBytes: []byte(svgContent),
		Checksum:  checkSum,
		Replace:   title,
		Width:     svgWidth,
		Height:    svgHeight,
	}, nil
}

// boolByte converts a bool to a single byte for use in checksum inputs.
func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// extractSVGDimensions parses the width and height from an SVG string.
// It reads the width/height attributes of the root <svg> element, accepting
// unitless values and stripping a trailing "px". If either attribute is
// absent, uses another unit suffix, or is otherwise non-numeric, it falls
// back to the viewBox (third and fourth fields).
func extractSVGDimensions(svgContent string) (width, height string) {
	type svgAttrs struct {
		Width   string
		Height  string
		ViewBox string
	}
	var attrs svgAttrs
	dec := xml.NewDecoder(strings.NewReader(svgContent))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "svg" {
			for _, attr := range se.Attr {
				switch attr.Name.Local {
				case "width":
					attrs.Width = attr.Value
				case "height":
					attrs.Height = attr.Value
				case "viewBox":
					attrs.ViewBox = attr.Value
				}
			}
			break
		}
	}

	parseAbsoluteLength := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}

		if strings.HasSuffix(s, "px") {
			s = strings.TrimSpace(strings.TrimSuffix(s, "px"))
		} else if s[len(s)-1] < '0' || s[len(s)-1] > '9' {
			// Treat relative or other non-absolute units (e.g. "%", "em",
			// "rem", "vw", "vh") as unknown so callers can fall back to viewBox.
			return ""
		}

		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return ""
		}

		return strconv.Itoa(int(math.Round(v)))
	}

	w := parseAbsoluteLength(attrs.Width)
	h := parseAbsoluteLength(attrs.Height)

	// Fall back to viewBox ("minX minY width height").
	// The SVG spec allows comma or whitespace separators (e.g. "0,0,640,480").
	if (w == "" || h == "") && attrs.ViewBox != "" {
		parts := strings.Fields(strings.ReplaceAll(attrs.ViewBox, ",", " "))
		if len(parts) == 4 {
			if w == "" {
				w = parseAbsoluteLength(parts[2])
			}
			if h == "" {
				h = parseAbsoluteLength(parts[3])
			}
		}
	}

	return w, h
}
