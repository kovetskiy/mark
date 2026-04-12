package metadata

import (
	"os"
	"path"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractDocumentLeadingH1(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	filename = "testdata/header.md"

	markdown, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	actual := ExtractDocumentLeadingH1(markdown)

	assert.Equal(t, "a", actual)
}

func TestSetTitleFromFilename(t *testing.T) {
	t.Run("set title from filename", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/test.md")
		assert.Equal(t, "Test", meta.Title)
	})

	t.Run("replace underscores with spaces", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/test_with_underscores.md")
		assert.Equal(t, "Test With Underscores", meta.Title)
	})

	t.Run("replace dashes with spaces", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/test-with-dashes.md")
		assert.Equal(t, "Test With Dashes", meta.Title)
	})

	t.Run("mixed underscores and dashes", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/test_with-mixed_separators.md")
		assert.Equal(t, "Test With Mixed Separators", meta.Title)
	})

	t.Run("already title cased", func(t *testing.T) {
		meta := &Meta{Title: ""}
		setTitleFromFilename(meta, "/path/to/Already-Title-Cased.md")
		assert.Equal(t, "Already Title Cased", meta.Title)
	})
}

func TestExtractMetaContentAppearance(t *testing.T) {
	t.Run("default fills missing content appearance", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->\n\nbody\n")

		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false, FixedContentAppearance)
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, FixedContentAppearance, meta.ContentAppearance)
	})

	t.Run("header takes precedence over default", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->\n<!-- Content-Appearance: full-width -->\n\nbody\n")

		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false, FixedContentAppearance)
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, FullWidthContentAppearance, meta.ContentAppearance)
	})

	t.Run("falls back to full-width when default isn't set", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->\n\nbody\n")

		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false, "")
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, FullWidthContentAppearance, meta.ContentAppearance)
	})
}

func TestExtractMetaNoTrailingNewline(t *testing.T) {
	t.Run("header-only file without trailing newline does not panic", func(t *testing.T) {
		data := []byte("# Blogs")

		assert.NotPanics(t, func() {
			_, _, _ = ExtractMeta(data, "", true, false, "", nil, false, "")
		})
	})

	t.Run("body without trailing newline", func(t *testing.T) {
		data := []byte("some content")

		meta, body, err := ExtractMeta(data, "", false, false, "", nil, false, "")
		assert.NoError(t, err)
		assert.Nil(t, meta)
		assert.Equal(t, []byte("some content"), body)
	})

	t.Run("metadata headers without trailing newline on last header", func(t *testing.T) {
		data := []byte("<!-- Space: DOC -->\n<!-- Title: Example -->")

		meta, body, err := ExtractMeta(data, "", false, false, "", nil, false, "")
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, "Example", meta.Title)
		assert.Equal(t, "DOC", meta.Space)
		assert.Empty(t, body)
	})
}
