package mermaid

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/kovetskiy/mark/attachment"
	"github.com/stretchr/testify/assert"
)

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
