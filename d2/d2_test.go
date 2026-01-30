package d2

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/kovetskiy/mark/attachment"
	"github.com/stretchr/testify/assert"
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

			// Dimensions can vary slightly by renderer/runtime; just ensure we set positive values
			assert.NotEmpty(t, got.Width, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.NotEmpty(t, got.Height, "processD2(%v, %v)", tt.name, string(tt.markdown))
		})
	}
}

func TestProcessD2SVG(t *testing.T) {
	attachment, err := ProcessD2SVG("example", []byte(diagram), true, "-", 1.0)
	if err != nil {
		t.Fatalf("ProcessD2SVG returned error: %v", err)
	}

	if !(bytes.HasPrefix(attachment.FileBytes, []byte("<svg")) || bytes.HasPrefix(attachment.FileBytes, []byte("<?xml"))) {
		t.Fatalf("expected svg output, got: %q", attachment.FileBytes[:20])
	}

	assert.Equal(t, "example.svg", attachment.Filename)
	assert.Equal(t, "example", attachment.Name)
	assert.Equal(t, "example", attachment.Replace)
	assert.Equal(t, "ba1dfc2b732c33fcc52b4f9f4f67a7a59c053e8dc3aefbc5a8b0e94c12d98352", attachment.Checksum)
	assert.NotEmpty(t, attachment.Width)
	assert.NotEmpty(t, attachment.Height)
}

func TestRenderD2SVGHasDimensions(t *testing.T) {
	svg, err := renderD2ToSVG(context.Background(), []byte(diagram))
	if err != nil {
		t.Fatalf("renderD2ToSVG returned error: %v", err)
	}

	if !bytes.Contains(svg, []byte("width=\"")) {
		t.Fatalf("expected width attribute in svg, got %s", svg[:80])
	}

	if !bytes.Contains(svg, []byte("height=\"")) {
		t.Fatalf("expected height attribute in svg, got %s", svg[:80])
	}
}
