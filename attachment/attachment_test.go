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

// Attachment destinations used to be substituted with a bare ReplaceAll over
// the whole document, which caused several distinct corruptions. Resolving a
// destination at a time makes each of them unrepresentable; these pin that.
func TestResolver(t *testing.T) {
	single := []Attachment{{
		Name: "a.png", Filename: "a.png", Replace: "a.png",
		Link: "/download/attachments/12345/a.png?version=1",
	}}

	t.Run("plain form", func(t *testing.T) {
		assert.Equal(t,
			"/download/attachments/12345/a.png?version=1",
			NewResolver(single).Resolve("a.png"),
		)
	})

	t.Run("legacy form is substituted once", func(t *testing.T) {
		// The legacy branch was an if rather than an else if, so the plain branch
		// then matched "a.png" inside the URL the legacy branch had just written,
		// yielding a doubled path with two query strings.
		assert.Equal(t,
			"/download/attachments/12345/a.png?version=1",
			NewResolver(single).Resolve("attachment://a.png"),
		)
	})

	t.Run("text that is not a destination", func(t *testing.T) {
		// "See the file `a.png`" used to become a download URL.
		assert.Empty(t, NewResolver(single).Resolve("See the file a.png in the repo."))
		assert.Empty(t, NewResolver(single).Resolve("b.png"))
		assert.Empty(t, NewResolver(single).Resolve(""))
	})

	t.Run("one name being a substring of another", func(t *testing.T) {
		// The length-descending sort only protected suffix collisions;
		// "logo.png" also occurs inside the URL written for "sub/logo.png".
		two := []Attachment{
			{Replace: "sub/logo.png", Link: "/dl/1/sub_logo.png?v=1"},
			{Replace: "logo.png", Link: "/dl/1/logo.png?v=1"},
		}
		resolver := NewResolver(two)
		assert.Equal(t, "/dl/1/logo.png?v=1", resolver.Resolve("logo.png"))
		assert.Equal(t, "/dl/1/sub_logo.png?v=1", resolver.Resolve("sub/logo.png"))
	})
}

func TestResolverUnused(t *testing.T) {
	attachments := []Attachment{
		{Replace: "used.png", Link: "/dl/1/used.png"},
		{Replace: "legacy.png", Link: "/dl/1/legacy.png"},
		{Replace: "never.png", Link: "/dl/1/never.png"},
	}

	resolver := NewResolver(attachments)
	resolver.Resolve("used.png")
	// Referring to an attachment by its legacy spelling still counts as using
	// it; reporting it unused would send someone looking for a second link.
	resolver.Resolve("attachment://legacy.png")

	assert.Equal(t, []string{"never.png"}, resolver.Unused(attachments))
}

func TestResolverNil(t *testing.T) {
	var resolver *Resolver
	assert.Empty(t, resolver.Resolve("a.png"))
	assert.Nil(t, resolver.Unused([]Attachment{{Replace: "a.png"}}))
}
