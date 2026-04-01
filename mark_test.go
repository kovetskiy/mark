package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
)

func TestMergeComments(t *testing.T) {
	body := "<p>Hello world</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-123">world</ac:inline-comment-marker></p>`
	comments := &confluence.InlineComments{
		Results: []struct {
			Extensions struct {
				Location         string `json:"location"`
				InlineProperties struct {
					OriginalSelection string `json:"originalSelection"`
					MarkerRef         string `json:"markerRef"`
				} `json:"inlineProperties"`
			} `json:"extensions"`
		}{
			{
				Extensions: struct {
					Location         string `json:"location"`
					InlineProperties struct {
						OriginalSelection string `json:"originalSelection"`
						MarkerRef         string `json:"markerRef"`
					} `json:"inlineProperties"`
				}{
					Location: "inline",
					InlineProperties: struct {
						OriginalSelection string `json:"originalSelection"`
						MarkerRef         string `json:"markerRef"`
					}{
						OriginalSelection: "world",
						MarkerRef:         "uuid-123",
					},
				},
			},
		},
	}

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-123">world</ac:inline-comment-marker></p>`, result)
}

func TestMergeComments_Escaping(t *testing.T) {
	body := "<p>Hello &amp; world</p>"
	oldBody := `<p>Hello <ac:inline-comment-marker ac:ref="uuid-456">&amp;</ac:inline-comment-marker> world</p>`
	comments := &confluence.InlineComments{
		Results: []struct {
			Extensions struct {
				Location         string `json:"location"`
				InlineProperties struct {
					OriginalSelection string `json:"originalSelection"`
					MarkerRef         string `json:"markerRef"`
				} `json:"inlineProperties"`
			} `json:"extensions"`
		}{
			{
				Extensions: struct {
					Location         string `json:"location"`
					InlineProperties struct {
						OriginalSelection string `json:"originalSelection"`
						MarkerRef         string `json:"markerRef"`
					} `json:"inlineProperties"`
				}{
					Location: "inline",
					InlineProperties: struct {
						OriginalSelection string `json:"originalSelection"`
						MarkerRef         string `json:"markerRef"`
					}{
						OriginalSelection: "&",
						MarkerRef:         "uuid-456",
					},
				},
			},
		},
	}

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	assert.Equal(t, `<p>Hello <ac:inline-comment-marker ac:ref="uuid-456">&amp;</ac:inline-comment-marker> world</p>`, result)
}

func TestMergeComments_Disambiguation(t *testing.T) {
	body := "<p>Item one. Item two. Item one.</p>"
	// Comment is on the second "Item one."
	oldBody := `<p>Item one. Item two. <ac:inline-comment-marker ac:ref="uuid-1">Item one.</ac:inline-comment-marker></p>`
	comments := &confluence.InlineComments{
		Results: []struct {
			Extensions struct {
				Location         string `json:"location"`
				InlineProperties struct {
					OriginalSelection string `json:"originalSelection"`
					MarkerRef         string `json:"markerRef"`
				} `json:"inlineProperties"`
			} `json:"extensions"`
		}{
			{
				Extensions: struct {
					Location         string `json:"location"`
					InlineProperties struct {
						OriginalSelection string `json:"originalSelection"`
						MarkerRef         string `json:"markerRef"`
					} `json:"inlineProperties"`
				}{
					Location: "inline",
					InlineProperties: struct {
						OriginalSelection string `json:"originalSelection"`
						MarkerRef         string `json:"markerRef"`
					}{
						OriginalSelection: "Item one.",
						MarkerRef:         "uuid-1",
					},
				},
			},
		},
	}

	result, err := MergeComments(body, oldBody, comments)
	assert.NoError(t, err)
	// Context should correctly pick the second occurrence
	assert.Equal(t, `<p>Item one. Item two. <ac:inline-comment-marker ac:ref="uuid-1">Item one.</ac:inline-comment-marker></p>`, result)
}
