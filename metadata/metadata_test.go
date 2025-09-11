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
		assert.Equal(t, "test", meta.Title)
	})
}
