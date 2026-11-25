package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hashedMeta(t *testing.T, document string, opts ExtractOptions) *Meta {
	t.Helper()

	opts.TitleAppendGeneratedHash = true
	meta, _, err := ExtractMeta([]byte(document), opts)
	require.NoError(t, err)
	require.NotNil(t, meta)

	return meta
}

// TestAppendGeneratedHashIsPinned: a hashed title is how a later run finds
// its page, so the hash must not drift under pages already published.
func TestAppendGeneratedHashIsPinned(t *testing.T) {
	meta := &Meta{Parents: []string{"Root"}, Space: "DOCS", Title: "Guides"}
	AppendGeneratedHash(meta)
	assert.Equal(t, "Guides - 91e9696d", meta.Title)
	assert.Equal(t, meta.Title, CaseFoldedHashTitle([]string{"Root"}, "DOCS", "Guides"))
}

// TestAppendGeneratedHashFoldsCase: Confluence takes a parent's title and a
// space key without regard to case, so mark publishes these to one place,
// and the hash has to agree or the page is published again beside itself.
// The title keeps the author's spelling.
func TestAppendGeneratedHashFoldsCase(t *testing.T) {
	want := hashedMeta(t, "<!-- Space: DOCS -->\n<!-- Parent: Handbook -->\n<!-- Title: Guide -->\n\nx\n", ExtractOptions{})

	for name, document := range map[string]string{
		"space":  "<!-- Space: docs -->\n<!-- Parent: Handbook -->\n<!-- Title: Guide -->\n\nx\n",
		"parent": "<!-- Space: DOCS -->\n<!-- Parent: handbook -->\n<!-- Title: Guide -->\n\nx\n",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, want.Title, hashedMeta(t, document, ExtractOptions{}).Title)
		})
	}

	t.Run("cli parents", func(t *testing.T) {
		document := "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nx\n"
		assert.Equal(t,
			hashedMeta(t, document, ExtractOptions{Parents: []string{"Root"}}).Title,
			hashedMeta(t, document, ExtractOptions{Parents: []string{"ROOT"}}).Title,
		)
	})

	t.Run("title keeps its case", func(t *testing.T) {
		lower := hashedMeta(t, "<!-- Space: DOCS -->\n<!-- Title: guide -->\n\nx\n", ExtractOptions{})
		upper := hashedMeta(t, "<!-- Space: DOCS -->\n<!-- Title: GUIDE -->\n\nx\n", ExtractOptions{})
		assert.Regexp(t, `^guide - [0-9a-f]{8}$`, lower.Title)
		assert.Regexp(t, `^GUIDE - [0-9a-f]{8}$`, upper.Title)
		assert.Equal(t, lower.Title[len("guide"):], upper.Title[len("GUIDE"):])
	})

	assert.NotEqual(t, want.Title,
		hashedMeta(t, "<!-- Space: DOCS -->\n<!-- Parent: Other -->\n<!-- Title: Guide -->\n\nx\n", ExtractOptions{}).Title)
}

// TestAppendGeneratedHashAfterTrim: a title taken from "guide-.md" is
// "Guide", and a space given with spaces around it is the space without
// them, so these hash as the plain spelling does.
func TestAppendGeneratedHashAfterTrim(t *testing.T) {
	document := "<!-- Space: DOCS -->\n\nx\n"
	want := hashedMeta(t, document, ExtractOptions{Filename: "guide.md", TitleFromFilename: true})
	assert.Regexp(t, `^Guide - [0-9a-f]{8}$`, want.Title)

	for _, filename := range []string{"guide-.md", "guide_.md"} {
		t.Run(filename, func(t *testing.T) {
			got := hashedMeta(t, document, ExtractOptions{Filename: filename, TitleFromFilename: true})
			assert.Equal(t, want.Title, got.Title)
		})
	}

	t.Run("space", func(t *testing.T) {
		document := "<!-- Title: Guide -->\n\nx\n"
		assert.Equal(t,
			hashedMeta(t, document, ExtractOptions{Space: "DOCS"}).Title,
			hashedMeta(t, document, ExtractOptions{Space: " DOCS "}).Title,
		)
	})
}
