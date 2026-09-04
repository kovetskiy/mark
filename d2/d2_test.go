package d2

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var diagram string = `d2
vars: {
  d2-config: {
    layout-engine: elk
    # Terminal theme code
    theme-id: 300
  } 
}
network: {
  cell tower: {
    satellites: {
      shape: stored_data
      style.multiple: true
    }

    transmitter

    satellites -> transmitter: send
    satellites -> transmitter: send
    satellites -> transmitter: send
  }

  online portal: {
    ui: {shape: hexagon}
  }   
      
  data processor: {
    storage: {
      shape: cylinder
      style.multiple: true
    }
  }

  cell tower.transmitter -> data processor.storage: phone logs
}

user: {
  shape: person
  width: 130
}

user -> network.cell tower: make call
user -> network.online portal.ui: access {
  style.stroke-dash: 3
}   

api server -> network.online portal.ui: display
api server -> logs: persist
logs: {shape: page; style.multiple: true}

network.data processor -> api server
`

func TestExtractD2Image(t *testing.T) {
	tests := []struct {
		name     string
		markdown []byte
		scale    float64
		want     attachment.Attachment
		wantErr  assert.ErrorAssertionFunc
	}{
		{"example", []byte(diagram), 1.0, attachment.Attachment{
			// This is only the PNG Magic Header
			FileBytes: []byte{0x89, 0x50, 0x4e, 0x47, 0xd, 0xa, 0x1a, 0xa},
			Filename:  "example.png",
			Name:      "example",
			Replace:   "example",
			Checksum:  "40e75f93e09da9242d4b1ab8e2892665ec7d5bd1ac78a4b65210ee219cf62297",
			ID:        "",
		},
			assert.NoError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProcessD2(tt.name, tt.markdown, tt.scale)
			if !tt.wantErr(t, err, fmt.Sprintf("processD2(%v, %v)", tt.name, string(tt.markdown))) {
				return
			}
			assert.Equal(t, tt.want.Filename, got.Filename, "processD2(%v, %v)", tt.name, string(tt.markdown))
			// We only test for the header as png changes based on system png library
			assert.Equal(t, tt.want.FileBytes, got.FileBytes[0:8], "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Name, got.Name, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Replace, got.Replace, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Checksum, got.Checksum, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.ID, got.ID, "processD2(%v, %v)", tt.name, string(tt.markdown))
			gotWidth, widthErr := strconv.ParseInt(got.Width, 10, 64)
			assert.NoError(t, widthErr, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Greater(t, gotWidth, int64(0), "processD2(%v, %v)", tt.name, string(tt.markdown))

			gotHeight, heightErr := strconv.ParseInt(got.Height, 10, 64)
			assert.NoError(t, heightErr, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Greater(t, gotHeight, int64(0), "processD2(%v, %v)", tt.name, string(tt.markdown))
		})
	}
}

// TestProcessD2SVG covers the other thing a diagram can be published as: the
// drawing itself rather than a picture of it.
func TestProcessD2SVG(t *testing.T) {
	got, err := ProcessD2SVG("example", []byte(diagram), "-", 1.0)
	require.NoError(t, err)

	assert.Equal(t, "example.svg", got.Filename)
	assert.Equal(t, "example", got.Name)
	assert.Equal(t, "example", got.Replace)
	assert.True(t, strings.Contains(string(got.FileBytes), "<svg"),
		"the attachment should be an SVG document")

	width, err := strconv.Atoi(got.Width)
	require.NoError(t, err, "width should be a whole number of pixels")
	assert.Positive(t, width)

	height, err := strconv.Atoi(got.Height)
	require.NoError(t, err, "height should be a whole number of pixels")
	assert.Positive(t, height)
}

// TestProcessD2SVGScalesWhatThePageShowsAndNotTheFile covers what the scale
// means for an SVG. The drawing is the same at every size, so the file and the
// checksum that identifies it do not move; the size the page displays it at
// does.
func TestProcessD2SVGScalesWhatThePageShowsAndNotTheFile(t *testing.T) {
	plain, err := ProcessD2SVG("example", []byte(diagram), "-", 1.0)
	require.NoError(t, err)

	scaled, err := ProcessD2SVG("example", []byte(diagram), "-", 2.0)
	require.NoError(t, err)

	assert.Equal(t, plain.Checksum, scaled.Checksum, "the drawing did not change")
	assert.Equal(t, plain.FileBytes, scaled.FileBytes)

	plainWidth, err := strconv.Atoi(plain.Width)
	require.NoError(t, err)

	scaledWidth, err := strconv.Atoi(scaled.Width)
	require.NoError(t, err)

	assert.Equal(t, plainWidth*2, scaledWidth, "the page shows it twice as wide")
}

// TestProcessD2SVGAndPNGAreDifferentAttachments pins that publishing the same
// diagram both ways cannot be mistaken for publishing it once.
func TestProcessD2SVGAndPNGAreDifferentAttachments(t *testing.T) {
	svg, err := ProcessD2SVG("example", []byte(diagram), "-", 1.0)
	require.NoError(t, err)

	png, err := ProcessD2("example", []byte(diagram), 1.0)
	require.NoError(t, err)

	assert.NotEqual(t, svg.Checksum, png.Checksum)
	assert.NotEqual(t, svg.Filename, png.Filename)
}
