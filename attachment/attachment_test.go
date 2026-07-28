package attachment

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	replacements = []string{
		"image1.jpg",
		"images/image2.jpg",
		"../image3.jpg",
	}
)

type bufferCloser struct {
	*bytes.Buffer
}

func (bufferCloser) Close() error { return nil }

type virtualOpener struct {
	PathToBuf map[string]*bufferCloser
}

func (o *virtualOpener) Open(name string) (io.ReadCloser, error) {
	if buf, ok := o.PathToBuf[name]; ok {
		return buf, nil
	}
	return nil, os.ErrNotExist
}

func TestPrepareAttachmentsWithWorkDirBase(t *testing.T) {

	testingOpener := &virtualOpener{
		PathToBuf: map[string]*bufferCloser{
			"image1.jpg":        {bytes.NewBuffer(nil)},
			"images/image2.jpg": {bytes.NewBuffer(nil)},
			"../image3.jpg":     {bytes.NewBuffer(nil)},
		},
	}

	attaches, err := prepareAttachments(testingOpener, ".", replacements)
	t.Logf("attaches: %v", err)
	if err != nil {
		println(err.Error())
		t.Fatal(err)
	}

	assert.Equal(t, "image1.jpg", attaches[0].Name)
	assert.Equal(t, "image1.jpg", attaches[0].Replace)

	assert.Equal(t, "images/image2.jpg", attaches[1].Name)
	assert.Equal(t, "images/image2.jpg", attaches[1].Replace)

	assert.Equal(t, "../image3.jpg", attaches[2].Name)
	assert.Equal(t, "../image3.jpg", attaches[2].Replace)

	assert.Equal(t, len(attaches), 3)
}

func TestPrepareAttachmentsWithSubDirBase(t *testing.T) {

	testingOpener := &virtualOpener{
		PathToBuf: map[string]*bufferCloser{
			"a/b/image1.jpg":        {bytes.NewBuffer(nil)},
			"a/b/images/image2.jpg": {bytes.NewBuffer(nil)},
			"a/image3.jpg":          {bytes.NewBuffer(nil)},
		},
	}

	attaches, err := prepareAttachments(testingOpener, "a/b", replacements)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "image1.jpg", attaches[0].Name)
	assert.Equal(t, "image1.jpg", attaches[0].Replace)

	assert.Equal(t, "images/image2.jpg", attaches[1].Name)
	assert.Equal(t, "images/image2.jpg", attaches[1].Replace)

	assert.Equal(t, "../image3.jpg", attaches[2].Name)
	assert.Equal(t, "../image3.jpg", attaches[2].Replace)

	assert.Equal(t, len(attaches), 3)
}

func TestParseAttachmentLink(t *testing.T) {
	tests := []struct {
		name       string
		attachLink string
		expected   string
	}{
		{
			name:       "valid URL with multiple query parameters",
			attachLink: "https://example.com/download/attachments/12345/foo.png?version=1&modificationDate=123",
			expected:   "/download/attachments/12345/foo.png?modificationDate=123&version=1",
		},
		{
			name:       "valid URL without query parameters",
			attachLink: "https://example.com/download/attachments/12345/foo.png",
			expected:   "/download/attachments/12345/foo.png",
		},
		{
			name:       "valid relative path URL (without scheme)",
			attachLink: "/download/attachments/12345/foo.png?version=1&modificationDate=123",
			expected:   "/download/attachments/12345/foo.png?modificationDate=123&version=1",
		},
		{
			name:       "invalid URI with invalid port (triggers ParseRequestURI error)",
			attachLink: "http://[::1]:foo/bar?version=1&modificationDate=123",
			expected:   "http://[::1]:foo/bar?version=1&modificationDate=123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := parseAttachmentLink(tt.attachLink)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

// CompileAttachmentLinks replaced attachment names with a bare ReplaceAll over
// the whole document, which caused three distinct corruptions.
func TestCompileAttachmentLinksAnchorsOnLinkTarget(t *testing.T) {
	single := []Attachment{{
		Name: "a.png", Filename: "a.png", Replace: "a.png",
		Link: "/download/attachments/12345/a.png?version=1",
	}}

	t.Run("legacy form is substituted once", func(t *testing.T) {
		// The legacy branch was an if rather than an else if, so the plain branch
		// then matched "a.png" inside the URL the legacy branch had just written,
		// yielding a doubled path with two query strings.
		got := string(CompileAttachmentLinks([]byte("![x](attachment://a.png)"), single))
		assert.Equal(t, "![x](/download/attachments/12345/a.png?version=1)", got)
	})

	t.Run("plain form is substituted", func(t *testing.T) {
		got := string(CompileAttachmentLinks([]byte("![x](a.png)"), single))
		assert.Equal(t, "![x](/download/attachments/12345/a.png?version=1)", got)
	})

	t.Run("prose and code spans are untouched", func(t *testing.T) {
		in := "See the file `a.png` in the repo."
		got := string(CompileAttachmentLinks([]byte(in), single))
		assert.Equal(t, in, got, "only a link target may be rewritten")
	})

	t.Run("one name being a substring of another", func(t *testing.T) {
		// The length-descending sort only protects suffix collisions; "logo.png"
		// also occurs inside the already-substituted URL for "sub/logo.png".
		two := []Attachment{
			{Replace: "sub/logo.png", Link: "/dl/1/sub_logo.png?v=1"},
			{Replace: "logo.png", Link: "/dl/1/logo.png?v=1"},
		}
		got := string(CompileAttachmentLinks([]byte("![a](logo.png) ![b](sub/logo.png)"), two))
		assert.Equal(t, "![a](/dl/1/logo.png?v=1) ![b](/dl/1/sub_logo.png?v=1)", got)
	})
}
