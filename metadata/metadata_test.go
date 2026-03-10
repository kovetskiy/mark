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

func TestExtractMetaParentID(t *testing.T) {
	t.Run("parse Parent-ID", func(t *testing.T) {
		data := []byte("<!-- Parent-ID: 12345 -->\ncontent\n")
		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false)
		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, "12345", meta.ParentID)
	})

	t.Run("reject empty Parent-ID", func(t *testing.T) {
		data := []byte("<!-- Parent-ID: -->\ncontent\n")
		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false)
		assert.Nil(t, meta)
		assert.EqualError(t, err, "Parent-ID header value is empty")
	})

	t.Run("reject duplicate Parent-ID", func(t *testing.T) {
		data := []byte("<!-- Parent-ID: 123 -->\n<!-- Parent-ID: 456 -->\n")
		meta, _, err := ExtractMeta(data, "", false, false, "", nil, false)
		assert.Nil(t, meta)
		assert.EqualError(t, err, "Parent-ID header is already set")
	})
}
