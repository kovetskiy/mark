package d2

import (
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
			Checksum:  "58fa387384181445e2d8f90a8c7fda945cb75174f73e8b9853ff59b9e0103ddd",
			ID:        "",
			Width:     "198",
			Height:    "441",
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
			assert.Equal(t, tt.want.Width, got.Width, "processD2(%v, %v)", tt.name, string(tt.markdown))
			assert.Equal(t, tt.want.Height, got.Height, "processD2(%v, %v)", tt.name, string(tt.markdown))
		})
	}
}
