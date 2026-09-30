package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFrontMatterListKeysAcceptASingleValue covers a list key written with one
// value rather than a list of one. "parents: Engineering" was read as no
// parents at all, so the page was published at the root of the space -- and
// "labels: release" as no labels, so label sync removed the page's existing
// ones -- with nothing logged.
func TestFrontMatterListKeysAcceptASingleValue(t *testing.T) {
	meta, logged := extractWithFrontMatter(t,
		"parents: Engineering\nfolders: ' Specs '\nlabels: release\nattachments: diagram.png\n",
	)

	assert.Equal(t, []string{"Engineering"}, meta.Parents)
	assert.True(t, meta.DeclaredParents)
	assert.Equal(t, []string{"Specs"}, meta.Folders)
	assert.Equal(t, []string{"release"}, meta.Labels)
	assert.Equal(t, []string{"diagram.png"}, meta.Attachments)
	assert.Empty(t, logged)
}

// TestFrontMatterListKeysWarnAboutWhatTheyDrop covers a value that is neither a
// string nor a list of them. It cannot be used, but it is reported rather than
// dropped in silence.
func TestFrontMatterListKeysWarnAboutWhatTheyDrop(t *testing.T) {
	t.Run("mapping", func(t *testing.T) {
		meta, logged := extractWithFrontMatter(t, "parents:\n  name: Engineering\n")

		assert.Empty(t, meta.Parents)
		assert.Contains(t, logged, `\"parents\"`)
		assert.Contains(t, logged, "was ignored")
	})

	t.Run("number", func(t *testing.T) {
		meta, logged := extractWithFrontMatter(t, "labels: 2024\n")

		assert.Empty(t, meta.Labels)
		assert.Contains(t, logged, `\"labels\"`)
		assert.Contains(t, logged, "was ignored")
	})

	t.Run("item in a list", func(t *testing.T) {
		meta, logged := extractWithFrontMatter(t, "labels:\n  - release\n  - {a: b}\n")

		assert.Equal(t, []string{"release"}, meta.Labels)
		assert.Contains(t, logged, `\"labels\"`)
		assert.Contains(t, logged, "was ignored")
	})

	t.Run("empty", func(t *testing.T) {
		meta, logged := extractWithFrontMatter(t, "labels:\n")

		assert.Empty(t, meta.Labels)
		assert.Empty(t, logged)
	})
}
