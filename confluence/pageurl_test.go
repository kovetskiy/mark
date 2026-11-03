package confluence

import (
	"encoding/base64"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			name:     "large page ID (Confluence Cloud)",
			pageID:   5000000001,
			expected: "AfIFKgE",
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
			pageID:   "5000000001",
			expected: "https://example.atlassian.net/wiki/x/AfIFKgE",
			wantErr:  false,
		},
		{
			name:     "cloud URL without trailing slash",
			baseURL:  "https://example.atlassian.net/wiki",
			pageID:   "5000000001",
			expected: "https://example.atlassian.net/wiki/x/AfIFKgE",
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
	largeID := uint64(5000000001)
	result := encodeTinyLinkID(largeID)
	assert.NotEmpty(t, result)
	assert.Equal(t, "AfIFKgE", result)

	// Verify the result is a valid URL-safe base64-like string
	assert.Regexp(t, `^[A-Za-z0-9_-]+$`, result)
}

func TestDecodeTinyLinkID(t *testing.T) {
	for _, id := range []uint64{1, 255, 256, 65535, 98319, 98320, 5000000001, 1<<63 + 5} {
		code := encodeTinyLinkID(id)
		got, err := DecodeTinyLinkID(code)
		require.NoError(t, err, code)
		assert.Equal(t, strconv.FormatUint(id, 10), got, code)

		// Confluence strips trailing "A"s from the codes it hands out.
		stripped := strings.TrimRight(code, "A")
		if stripped == "" {
			continue
		}
		got, err = DecodeTinyLinkID(stripped)
		require.NoError(t, err, stripped)
		assert.Equal(t, strconv.FormatUint(id, 10), got, stripped)
	}

	for _, bad := range []string{"", "A", "AAAA", "a.b", "a/b", "a+b", "AQAAAAAAAAAB"} {
		_, err := DecodeTinyLinkID(bad)
		assert.Error(t, err, bad)
	}

	_, err := DecodeTinyLinkID("A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"A" is not`, "the error quotes the code as it was given")
}
