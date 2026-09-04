// Package svg reads what a rendered SVG says about its own size.
//
// Both diagram renderers publish SVG attachments, and Confluence lays an
// attachment out from the width and height that go beside it: the alignment
// thresholds and the display width are decided from them, and a size nobody
// stated is a diagram the page cannot place. What the two renderers do with the
// numbers differs -- one scales them, the other does not -- so this reports
// them and leaves that to the caller.
package svg

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"
)

// Dimensions reports the size an SVG document states, in pixels. A dimension it
// does not state, or states in something that is not a number of pixels, is
// reported as 0.
//
// The root element's width and height are preferred and the viewBox is the
// fallback for either of them, because a diagram too wide for its frame is
// drawn as width="100%" -- which is not a number of pixels, and read as 100 of
// them would lay a wide diagram out as a narrow one.
func Dimensions(document string) (width, height float64) {
	var attrWidth, attrHeight, attrViewBox string

	decoder := xml.NewDecoder(strings.NewReader(document))

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		element, ok := token.(xml.StartElement)
		if !ok || element.Name.Local != "svg" {
			continue
		}

		for _, attr := range element.Attr {
			switch attr.Name.Local {
			case "width":
				attrWidth = attr.Value
			case "height":
				attrHeight = attr.Value
			case "viewBox":
				attrViewBox = attr.Value
			}
		}

		break
	}

	width = absoluteLength(attrWidth)
	height = absoluteLength(attrHeight)

	// "minX minY width height", separated by whitespace or commas.
	if (width == 0 || height == 0) && attrViewBox != "" {
		fields := strings.Fields(strings.ReplaceAll(attrViewBox, ",", " "))
		if len(fields) == 4 {
			if width == 0 {
				width = absoluteLength(fields[2])
			}

			if height == 0 {
				height = absoluteLength(fields[3])
			}
		}
	}

	return width, height
}

// Pixels renders a length as the whole number of pixels an attachment is
// measured in, or "" for a length that was never known.
func Pixels(length float64) string {
	if length == 0 {
		return ""
	}

	return strconv.Itoa(int(math.Round(length)))
}

// absoluteLength reads a length that is a number of pixels -- bare, or written
// with px -- and returns it, or 0 for anything else. A relative unit (%, em,
// rem, vw, vh) is not a size on its own, so it is reported as unknown rather
// than as the number in front of it.
func absoluteLength(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if suffix := strings.TrimSuffix(value, "px"); suffix != value {
		value = strings.TrimSpace(suffix)
	} else if last := value[len(value)-1]; last < '0' || last > '9' {
		return 0
	}

	pixels, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(pixels) || math.IsInf(pixels, 0) {
		return 0
	}

	return pixels
}
