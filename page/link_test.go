package page

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkResolverLeavesNonRepositoryTargetsAlone(t *testing.T) {
	// A resolver with an API set but nothing on disk to point at: every target
	// here has to come back empty, meaning "leave the link as written".
	resolver := &LinkResolver{API: &confluence.API{}, Base: t.TempDir()}

	for _, target := range []string{
		"https://example.com/page",
		"http://example.com/page",
		"ftp://example.com/file.txt",
		"mailto:someone@example.com",
		"#heading-in-document",
		"",
		"no-such-file.md",
		"no-such-file.md#hash",
		"../outside/missing.md",
	} {
		resolved, err := resolver.Resolve(target, "")
		assert.NoError(t, err, "target %q", target)
		assert.Empty(t, resolved, "target %q should have been left alone", target)
	}
}

func TestLinkResolverIgnoresDirectories(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "docs"), 0o755))

	resolver := &LinkResolver{API: &confluence.API{}, Base: base}

	resolved, err := resolver.Resolve("docs", "")
	assert.NoError(t, err)
	assert.Empty(t, resolved)
}

// TestLinkResolverLeavesRootedTargetsAlone covers a link written from the root:
// [/example](/example).
//
// A rooted path is not a path in this repository. It is either somewhere on the
// site the pages are published to, or one an attachment has already claimed,
// and neither is mark's to follow -- so the link is left exactly as written
// without anything on disk being consulted at all.
//
// Which is why a directory is no different from a file here, and both are no
// different from nothing: the resolver never looks. Written down because the
// obvious reading of "resolve a link" is that it goes and finds something.
func TestLinkResolverLeavesRootedTargetsAlone(t *testing.T) {
	base := t.TempDir()

	// Everything a rooted target could name, sitting in the base directory so
	// that a resolver which did look would find it and do something.
	require.NoError(t, os.Mkdir(filepath.Join(base, "example"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "page.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

	resolver := &LinkResolver{API: &confluence.API{}, Base: base}

	for _, target := range []string{
		"/example",
		"/example/",
		"/page.md",
		"/nothing-here",
		"/example#a-heading",
	} {
		resolved, err := resolver.Resolve(target, "")
		assert.NoError(t, err, "target %q", target)
		assert.Empty(t, resolved, "target %q should have been left as written", target)
	}
}

// TestFindLinkTargetSaysWhichKindOfNothingItFound covers the two ways a
// relative link can fail to name a document, which are worth telling apart:
// --check-links reports the reason, and "there is no such file" would send
// somebody looking for a typo in a path that is perfectly correct.
func TestFindLinkTargetSaysWhichKindOfNothingItFound(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "example"), 0o755))

	_, why := findLinkTarget([]string{base}, "example")
	require.NotNil(t, why)
	assert.Contains(t, why.reason, "directory")

	_, why = findLinkTarget([]string{base}, "no-such-thing")
	require.NotNil(t, why)
	assert.Contains(t, why.reason, "no such file")
}

// TestFindLinkTargetPrefersAFileInALaterSearchDirectory covers a directory that
// shares its name with a document somewhere else on the search path.
//
// A directory is not an answer, so the search goes on rather than stopping at
// it: --include-path exists so that a name can be found somewhere other than
// beside the document, and a directory of that name in the first place looked
// would otherwise hide the file in the second.
func TestFindLinkTargetPrefersAFileInALaterSearchDirectory(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(first, "shared"), 0o755))

	document := filepath.Join(second, "shared")
	require.NoError(t, os.WriteFile(document, []byte("Body.\n"), 0o600))

	found, why := findLinkTarget([]string{first, second}, "shared")
	require.Nil(t, why, "the file in the second directory is the answer")
	assert.Equal(t, document, found)
}

func TestLinkResolverIgnoresNonTextFiles(t *testing.T) {
	base := t.TempDir()
	// A PNG header is enough for http.DetectContentType.
	require.NoError(t, os.WriteFile(
		filepath.Join(base, "image.png"),
		[]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"),
		0o644,
	))

	resolver := &LinkResolver{API: &confluence.API{}, Base: base}

	resolved, err := resolver.Resolve("image.png", "")
	assert.NoError(t, err)
	assert.Empty(t, resolved)
}

func TestLinkResolverIgnoresFilesWithoutMetadata(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(base, "plain.md"),
		[]byte("# Just a heading\n\nNo mark metadata here.\n"),
		0o644,
	))

	resolver := &LinkResolver{API: &confluence.API{}, Base: base}

	// Without metadata there is no space or title to look a page up by, so the
	// link stays as it is rather than the run failing.
	resolved, err := resolver.Resolve("plain.md", "")
	assert.NoError(t, err)
	assert.Empty(t, resolved)
}

func TestLinkResolverWithoutAPIDoesNothing(t *testing.T) {
	var resolver *LinkResolver

	resolved, err := resolver.Resolve("anything.md", "")
	assert.NoError(t, err)
	assert.Empty(t, resolved)
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
