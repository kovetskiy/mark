package mermaid

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/stretchr/testify/assert"
)

// findDescContent walks an SVG XML string and returns the text content of the
// first <desc> element (regardless of namespace or attributes) and whether one
// was found at all.
func findDescContent(svgStr string) (string, bool) {
	dec := xml.NewDecoder(strings.NewReader(svgStr))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "desc" {
			var sb strings.Builder
			for {
				inner, err := dec.Token()
				if err != nil {
					break
				}
				if cd, ok := inner.(xml.CharData); ok {
					sb.Write([]byte(cd))
				}
				if ee, ok := inner.(xml.EndElement); ok && ee.Name.Local == "desc" {
					break
				}
			}
			return sb.String(), true
		}
	}
	return "", false
}

// hasSVGRoot returns true when the XML stream contains a root <svg> element,
// tolerating an optional <?xml ...?> processing instruction or other preamble.
func hasSVGRoot(svgStr string) bool {
	dec := xml.NewDecoder(strings.NewReader(svgStr))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local == "svg"
		}
	}
	return false
}

func TestExtractMermaidImage(t *testing.T) {
	tests := []struct {
		name     string
		markdown []byte
		scale    float64
		want     attachment.Attachment
		wantErr  assert.ErrorAssertionFunc
	}{
		{"example", []byte("graph TD;\n A-->B;"), 1.0, attachment.Attachment{
			// This is only the PNG Magic Header
			FileBytes: []byte{0x89, 0x50, 0x4e, 0x47, 0xd, 0xa, 0x1a, 0xa},
			Filename:  "example.png",
			Name:      "example",
			Replace:   "example",
			Checksum:  "26296b73c960c25850b37bc9dd77cb24fce1a78db83b37755a25af7f8a48cc96",
			ID:        "",
		},
			assert.NoError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProcessMermaidLocally(tt.name, tt.markdown, tt.scale)
			if !tt.wantErr(t, err, fmt.Sprintf("processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))) {
				return
			}
			assert.Equal(t, tt.want.Filename, got.Filename, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			// We only test for the header as png changes based on system png library
			assert.Equal(t, tt.want.FileBytes, got.FileBytes[0:8], "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Name, got.Name, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Replace, got.Replace, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Checksum, got.Checksum, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.ID, got.ID, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))

			gotWidth, widthErr := strconv.ParseInt(got.Width, 10, 64)
			assert.NoError(t, widthErr, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Greater(t, gotWidth, int64(0), "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))

			gotHeight, heightErr := strconv.ParseInt(got.Height, 10, 64)
			assert.NoError(t, heightErr, "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
			assert.Greater(t, gotHeight, int64(0), "processMermaidLocally(%v, %v)", tt.name, string(tt.markdown))
		})
	}
}

func TestProcessMermaidSVG(t *testing.T) {
	diagram := []byte("graph TD;\n A-->B;")
	got, err := ProcessMermaidSVG("svgtest", diagram)
	assert.NoError(t, err)
	assert.Equal(t, "svgtest.svg", got.Filename)
	assert.Equal(t, "svgtest", got.Name)
	assert.Equal(t, "svgtest", got.Replace)
	assert.NotEmpty(t, got.FileBytes)
	assert.True(t, hasSVGRoot(string(got.FileBytes)), "output should be SVG with an <svg> root element")
	// Dimensions should be extracted from the SVG (returned as integer pixel strings)
	gotWidth, widthErr := strconv.Atoi(got.Width)
	assert.NoError(t, widthErr, "Width should be an integer string")
	assert.Greater(t, gotWidth, 0, "Width should be positive")
	gotHeight, heightErr := strconv.Atoi(got.Height)
	assert.NoError(t, heightErr, "Height should be an integer string")
	assert.Greater(t, gotHeight, 0, "Height should be positive")
}

func TestProcessMermaidWithBundle(t *testing.T) {
	diagram := []byte("graph TD;\n A-->B;")
	got, err := ProcessMermaidWithBundle("bundletest", diagram)
	assert.NoError(t, err)
	assert.Equal(t, "bundletest.svg", got.Filename)
	assert.Equal(t, "bundletest", got.Name)
	assert.Equal(t, "bundletest", got.Replace)
	assert.NotEmpty(t, got.FileBytes)
	svgStr := string(got.FileBytes)
	assert.True(t, hasSVGRoot(svgStr), "output should be SVG with an <svg> root element")
	// WithBundle embeds the diagram source in a <desc> element; parse the SVG
	// to find it robustly regardless of attributes or namespace on the element.
	descContent, hasDesc := findDescContent(svgStr)
	assert.True(t, hasDesc, "bundled SVG should contain a <desc> element")
	assert.Contains(t, descContent, "graph TD;", "bundled SVG <desc> should contain original diagram source")
	// Dimensions should be extracted from the SVG (returned as integer pixel strings)
	gotWidth, widthErr := strconv.Atoi(got.Width)
	assert.NoError(t, widthErr, "Width should be an integer string")
	assert.Greater(t, gotWidth, 0, "Width should be positive")
	gotHeight, heightErr := strconv.Atoi(got.Height)
	assert.NoError(t, heightErr, "Height should be an integer string")
	assert.Greater(t, gotHeight, 0, "Height should be positive")
}

func TestExtractSVGDimensions(t *testing.T) {
	tests := []struct {
		name        string
		svg         string
		wantWidth   string
		wantHeight  string
	}{
		{
			name:       "width and height attributes",
			svg:        `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="200"></svg>`,
			wantWidth:  "300",
			wantHeight: "200",
		},
		{
			name:       "width and height with px unit",
			svg:        `<svg xmlns="http://www.w3.org/2000/svg" width="450px" height="300px"></svg>`,
			wantWidth:  "450",
			wantHeight: "300",
		},
		{
			name:       "fallback to viewBox",
			svg:        `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 480"></svg>`,
			wantWidth:  "640",
			wantHeight: "480",
		},
		{
			name:       "partial fallback: missing height falls back to viewBox",
			svg:        `<svg xmlns="http://www.w3.org/2000/svg" width="320" viewBox="0 0 640 480"></svg>`,
			wantWidth:  "320",
			wantHeight: "480",
		},
		{
			name:       "relative percent width falls back to viewBox",
			svg:        `<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" viewBox="0 0 800 600"></svg>`,
			wantWidth:  "800",
			wantHeight: "600",
		},
		{
			name:       "em unit treated as unknown, falls back to viewBox",
			svg:        `<svg xmlns="http://www.w3.org/2000/svg" width="20em" height="15em" viewBox="0 0 320 240"></svg>`,
			wantWidth:  "320",
			wantHeight: "240",
		},
		{
			name:       "comma-separated viewBox",
			svg:        `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0,0,640,480"></svg>`,
			wantWidth:  "640",
			wantHeight: "480",
		},
		{
			name:       "no dimensions",
			svg:        `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
			wantWidth:  "",
			wantHeight: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := extractSVGDimensions(tt.svg)
			assert.Equal(t, tt.wantWidth, w)
			assert.Equal(t, tt.wantHeight, h)
		})
	}
}

func TestSVGBundleChecksumsDiffer(t *testing.T) {
	diagram := []byte("graph TD;\n A-->B;")
	plain, err := ProcessMermaidSVG("chk", diagram)
	assert.NoError(t, err)
	bundled, err := ProcessMermaidWithBundle("chk", diagram)
	assert.NoError(t, err)
	assert.NotEqual(t, plain.Checksum, bundled.Checksum,
		"plain SVG and bundled SVG should have different checksums so toggling --mermaid-bundle triggers a re-upload")
}
