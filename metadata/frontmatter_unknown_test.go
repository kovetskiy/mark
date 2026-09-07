package metadata

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractWithFrontMatter reads a document and returns whatever was logged while
// doing it.
func extractWithFrontMatter(t *testing.T, frontMatter string) (*Meta, string) {
	t.Helper()

	var logged bytes.Buffer

	restore := log.Logger
	log.Logger = zerolog.New(&logged)

	t.Cleanup(func() { log.Logger = restore })

	document := "---\nspace: DOCS\ntitle: Doc\n" + frontMatter + "---\n\nBody.\n"

	meta, _, err := ExtractMeta([]byte(document), "", false, false, "doc.md", nil, false, "", true)
	require.NoError(t, err)

	return meta, logged.String()
}

// TestFrontMatterWarnsAboutMarksOwnSingularNames covers the mistake mark's own
// documentation invites: the HTML headers are singular, so somebody who has
// been writing <!-- Attachment: --> for years writes attachment in front matter
// and is told nothing at all, by a run that publishes perfectly happily with
// the attachment missing.
func TestFrontMatterWarnsAboutMarksOwnSingularNames(t *testing.T) {
	for _, singular := range []string{"attachment", "parent", "folder", "label"} {
		t.Run(singular, func(t *testing.T) {
			meta, logged := extractWithFrontMatter(t, singular+":\n  - something\n")

			assert.Contains(t, logged, singular)
			assert.Contains(t, logged, "was ignored")
			assert.Empty(t, meta.Attachments)
			assert.Empty(t, meta.Labels)
		})
	}
}

// TestFrontMatterWarnsAboutATypo covers a key that is nobody's convention, only
// fingers.
func TestFrontMatterWarnsAboutATypo(t *testing.T) {
	_, logged := extractWithFrontMatter(t, "attachmnets:\n  - a.png\n")

	assert.Contains(t, logged, "attachmnets")
	assert.Contains(t, logged, "was ignored")
}

// TestFrontMatterWarnsAboutEveryKeyItDoesNotRead covers the keys that belong to
// something else -- a static site generator's, sitting in the same block.
//
// They are reported too. mark cannot tell a key written for another tool from
// one written for mark and misspelled, and the second kind is a setting that
// silently did not happen: the cost of saying so about both is a line per key,
// where the cost of saying nothing is a document that publishes wrong.
func TestFrontMatterWarnsAboutEveryKeyItDoesNotRead(t *testing.T) {
	for _, key := range []string{
		"date: 2024-01-01\n",
		"draft: true\n",
		"weight: 10\n",
		"description: a page\n",
		"slug: a-page\n",
		"author: someone\n",
		"tags:\n  - one\n",
		"aliases:\n  - /old/path\n",
	} {
		name := key[:strings.IndexByte(key, ':')]

		t.Run(name, func(t *testing.T) {
			_, logged := extractWithFrontMatter(t, key)

			assert.Contains(t, logged, name)
			assert.Contains(t, logged, "was ignored")
		})
	}
}

// TestFrontMatterWarnsAboutEveryUnknownKeyOnce covers a document carrying
// several at once, which is what a shared front matter block looks like.
func TestFrontMatterWarnsAboutEveryUnknownKeyOnce(t *testing.T) {
	_, logged := extractWithFrontMatter(t, "date: 2024-01-01\ndraft: true\nweight: 10\n")

	for _, key := range []string{"date", "draft", "weight"} {
		// The log escapes its quotes, so the key is matched with the words
		// that follow it rather than by the quoting around it.
		assert.Equal(t, 1, strings.Count(logged, key+`\" is not read by mark`),
			"%q should be reported exactly once", key)
	}
}

// TestFrontMatterSaysNothingAboutWhatItRead is the boundary the others rest on:
// a document written correctly, including the spellings that are meant to work.
func TestFrontMatterSaysNothingAboutWhatItRead(t *testing.T) {
	meta, logged := extractWithFrontMatter(t,
		"attachments:\n  - a.png\nimage-align: center\nContent_Appearance: full-width\n")

	assert.Empty(t, logged)
	assert.Equal(t, []string{"a.png"}, meta.Attachments)
	assert.Equal(t, "center", meta.ImageAlign)
}

// TestFrontMatterWarningNamesTheDocument covers the part of the warning that
// makes it actionable when a run is publishing hundreds of files -- and the
// caller that has no name to give, since ExtractMeta is reached from a library
// where a document need not be a file.
func TestFrontMatterWarningNamesTheDocument(t *testing.T) {
	_, logged := extractWithFrontMatter(t, "draft: true\n")
	assert.Contains(t, logged, "doc.md: front matter key")

	var buffer bytes.Buffer

	restore := log.Logger
	log.Logger = zerolog.New(&buffer)

	t.Cleanup(func() { log.Logger = restore })

	_, _, err := ExtractMeta(
		[]byte("---\nspace: DOCS\ntitle: Doc\ndraft: true\n---\n\nBody.\n"),
		"", false, false, "", nil, false, "", true,
	)
	require.NoError(t, err)

	assert.Contains(t, buffer.String(), "front matter key")
	assert.NotContains(t, buffer.String(), `": front matter`,
		"a document with no name should not be reported as one called \": \"")
}
