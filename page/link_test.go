package page

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLinks(t *testing.T) {
	markdown := `
	[example1](../path/to/example.md#second-heading)
	[example2](../path/to/example.md)
	[example3](#heading-in-document)
	[Text link that should be put as attachment](../path/to/example.txt)
	[Image link that should be put as attachment](../path/to/example.png)
	[relative link without dots](relative-link-without-dots.md)
	[relative link without dots but with hash](relative-link-without-dots-but-with-hash.md#hash)
	[example [example]](example.md)
	`

	links := parseLinks(markdown)

	assert.Equal(t, "../path/to/example.md#second-heading", links[0].full)
	assert.Equal(t, "../path/to/example.md", links[0].filename)
	assert.Equal(t, "second-heading", links[0].hash)

	assert.Equal(t, "../path/to/example.md", links[1].full)
	assert.Equal(t, "../path/to/example.md", links[1].filename)
	assert.Equal(t, "", links[1].hash)

	assert.Equal(t, "#heading-in-document", links[2].full)
	assert.Equal(t, "", links[2].filename)
	assert.Equal(t, "heading-in-document", links[2].hash)

	assert.Equal(t, "../path/to/example.txt", links[3].full)
	assert.Equal(t, "../path/to/example.txt", links[3].filename)
	assert.Equal(t, "", links[3].hash)

	assert.Equal(t, "../path/to/example.png", links[4].full)
	assert.Equal(t, "../path/to/example.png", links[4].filename)
	assert.Equal(t, "", links[4].hash)

	assert.Equal(t, "relative-link-without-dots.md", links[5].full)
	assert.Equal(t, "relative-link-without-dots.md", links[5].filename)
	assert.Equal(t, "", links[5].hash)

	assert.Equal(t, "relative-link-without-dots-but-with-hash.md#hash", links[6].full)
	assert.Equal(t, "relative-link-without-dots-but-with-hash.md", links[6].filename)
	assert.Equal(t, "hash", links[6].hash)

	assert.Equal(t, "example.md", links[7].full)
	assert.Equal(t, len(links), 8)
}

func TestEncodeTinyLinkID(t *testing.T) {
	// Test cases for the tiny link encoding algorithm.
	// The algorithm: little-endian bytes -> base64 -> URL-safe transform
	tests := []struct {
		name     string
		pageID   uint64
		expected string
	}{
		{
			name:     "small page ID",
			pageID:   98319,
			expected: "D4AB",
		},
		{
			name:     "another small page ID",
			pageID:   98320,
			expected: "EIAB",
		},
		{
			name:     "large page ID from user example",
			pageID:   5645697027,
			expected: "A4CCUAE",
		},
		{
			name:     "page ID 1",
			pageID:   1,
			expected: "AQ",
		},
		{
			name:     "page ID 255",
			pageID:   255,
			expected: "-w",
		},
		{
			name:     "page ID 256",
			pageID:   256,
			expected: "AAE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeTinyLinkID(tt.pageID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateTinyLink(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		pageID   string
		expected string
		wantErr  bool
	}{
		{
			name:     "cloud URL with trailing slash",
			baseURL:  "https://example.atlassian.net/wiki/",
			pageID:   "5645697027",
			expected: "https://example.atlassian.net/wiki/x/A4CCUAE",
			wantErr:  false,
		},
		{
			name:     "cloud URL without trailing slash",
			baseURL:  "https://example.atlassian.net/wiki",
			pageID:   "5645697027",
			expected: "https://example.atlassian.net/wiki/x/A4CCUAE",
			wantErr:  false,
		},
		{
			name:     "server URL",
			baseURL:  "https://confluence.example.com",
			pageID:   "98319",
			expected: "https://confluence.example.com/x/D4AB",
			wantErr:  false,
		},
		{
			name:     "invalid page ID",
			baseURL:  "https://example.atlassian.net/wiki",
			pageID:   "not-a-number",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenerateTinyLink(tt.baseURL, tt.pageID)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// encodeTinyLinkIDPerl32 implements the Perl algorithm from Atlassian docs
// using pack("L", $pageID) which is 32-bit little-endian.
// This is used to validate our implementation matches the documented algorithm.
func encodeTinyLinkIDPerl32(id uint32) string {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, id)
	encoded := base64.StdEncoding.EncodeToString(buf)

	var result strings.Builder
	for _, c := range encoded {
		switch c {
		case '=':
			continue
		case '/':
			result.WriteByte('-')
		case '+':
			result.WriteByte('_')
		default:
			result.WriteRune(c)
		}
	}
	s := result.String()
	// Perl strips trailing 'A' chars (which are base64 for zero bits)
	s = strings.TrimRight(s, "A")
	return s
}

func TestEncodeTinyLinkIDMatchesPerl(t *testing.T) {
	// Validate that our implementation matches the Perl algorithm from:
	// https://support.atlassian.com/confluence/kb/how-to-programmatically-generate-the-tiny-link-of-a-confluence-page
	testIDs := []uint32{1, 255, 256, 65535, 98319, 98320}

	for _, id := range testIDs {
		goResult := encodeTinyLinkID(uint64(id))
		perlResult := encodeTinyLinkIDPerl32(id)
		assert.Equal(t, perlResult, goResult, "ID %d should match Perl implementation", id)
	}
}

func TestEncodeTinyLinkIDLargeIDs(t *testing.T) {
	// Test large page IDs (> 32-bit) which are common in Confluence Cloud
	// These exceed Perl's pack("L") but our implementation handles them
	largeID := uint64(5645697027) // User's actual page ID from the issue
	result := encodeTinyLinkID(largeID)
	assert.NotEmpty(t, result)
	assert.Equal(t, "A4CCUAE", result)

	// Verify the result is a valid URL-safe base64-like string
	assert.Regexp(t, `^[A-Za-z0-9_-]+$`, result)
}
