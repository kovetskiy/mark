package similar_test

import (
	"context"
	"os"
	"testing"

	"github.com/kovetskiy/mark/v17/internal/similar"
	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWords(t *testing.T) {
	assert.Equal(t, []string{"don", "t", "panic", "42", "größe"}, similar.Words("Don't PANIC: 42 — Größe!"))
	assert.Empty(t, similar.Words(" -- ** "))
}

func TestStorageWords(t *testing.T) {
	storage := `<h1 id="Title-Here">Title &amp; here</h1>` +
		`<!-- a comment -->` +
		`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter>` +
		`<ac:plain-text-body><![CDATA[if a < b { return }]]></ac:plain-text-body></ac:structured-macro>` +
		`<p>Link <a href="https://example.com/x">there</a>.</p>`

	assert.Equal(t,
		[]string{"title", "here", "go", "if", "a", "b", "return", "link", "there"},
		similar.StorageWords(storage),
	)
}

func TestMarkdownWords(t *testing.T) {
	md := "<!-- hidden -->\n# Title &amp; here\n\nSee [the docs](https://example.com/x \"t\") and <b>bold</b>.\n"

	assert.Equal(t, []string{"title", "here", "see", "the", "docs", "and", "bold"}, similar.MarkdownWords(md))
}

func TestScore(t *testing.T) {
	words := similar.Words

	assert.InDelta(t, 1, similar.Score(words("one two three four"), words("One, two; three four.")), 1e-9)
	assert.Zero(t, similar.Score(nil, words("anything")), "an empty text is like nothing")
	assert.Zero(t, similar.Score(nil, nil), "two empty texts are not alike")

	// Four shingles of five words; one word changed in the middle leaves one
	// of them on each side: 1 / (4 + 4 - 1).
	assert.InDelta(t, 1.0/7, similar.Score(words("a b c d e f"), words("a b x d e f")), 1e-9)

	// Shorter than a shingle: compared by runs as long as the shorter text.
	assert.InDelta(t, 1, similar.Score(words("Hello"), words("hello!")), 1e-9)
	assert.InDelta(t, 1.0/2, similar.Score(words("hello world"), words("hello")), 1e-9)
	assert.Zero(t, similar.Score(words("hello world"), words("goodbye")))
}

// TestScoresOfRealisticRenames is the evidence for the threshold the docs
// recommend: old ADRs as mark publishes them, against new Markdown.
func TestScoresOfRealisticRenames(t *testing.T) {
	std, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	published := func(name string) []string {
		src, err := os.ReadFile("testdata/" + name + ".md")
		require.NoError(t, err)
		body, _, err := markdown.CompileMarkdown(context.Background(), src, std, name+".md", markdown.Options{})
		require.NoError(t, err)

		return similar.StorageWords(body)
	}
	source := func(name string) []string {
		src, err := os.ReadFile("testdata/" + name + ".md")
		require.NoError(t, err)

		return similar.MarkdownWords(string(src))
	}

	old := published("adr-0001")
	assert.InDelta(t, 1, similar.Score(old, source("adr-0001")), 1e-9, "unchanged")

	// New title, status Proposed to Accepted, a sentence reworded and a
	// paragraph added: about 0.78.
	edited := similar.Score(old, source("adr-0001-edited"))
	assert.Greater(t, edited, 0.7)

	// The decision and consequences rewritten: about 0.37, a different
	// decision more than the same one renamed.
	rewritten := similar.Score(old, source("adr-0001-rewritten"))
	assert.Less(t, rewritten, 0.5)

	// Other ADRs of the same template: under 0.05.
	for _, other := range []string{"adr-0002", "adr-0003"} {
		assert.Less(t, similar.Score(old, source(other)), 0.05, other)
		assert.Less(t, similar.Score(published(other), source("adr-0001-edited")), 0.05, other)
	}
}

func TestMatch(t *testing.T) {
	const threshold, margin = 0.6, 0.1

	t.Run("clear best is taken", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"new": {"old": 0.8, "other": 0.2},
		}, threshold, margin)
		assert.Equal(t, map[string]similar.Pair{"new": {New: "new", Old: "old", Score: 0.8}}, taken)
		assert.Empty(t, unclear)
	})

	t.Run("below the threshold is nothing", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{"new": {"old": 0.55}}, threshold, margin)
		assert.Empty(t, taken)
		assert.Empty(t, unclear)
	})

	t.Run("two old ones within the margin", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"new": {"a": 0.8, "b": 0.75},
		}, threshold, margin)
		assert.Empty(t, taken)
		assert.Equal(t, []similar.Pair{{New: "new", Old: "a", Score: 0.8}, {New: "new", Old: "b", Score: 0.75}}, unclear["new"])
	})

	t.Run("a runner-up below the threshold still counts", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"new": {"a": 0.65, "b": 0.58},
		}, threshold, margin)
		assert.Empty(t, taken)
		assert.Len(t, unclear["new"], 1)
	})

	t.Run("one old one, two claimants", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"clear": {"old": 0.9},
			"weak":  {"old": 0.7},
		}, threshold, margin)
		assert.Equal(t, map[string]similar.Pair{"clear": {New: "clear", Old: "old", Score: 0.9}}, taken)
		assert.Contains(t, unclear, "weak")

		taken, unclear = similar.Match(map[string]map[string]float64{
			"one":   {"old": 0.9},
			"other": {"old": 0.85},
		}, threshold, margin)
		assert.Empty(t, taken, "neither leads the other by the margin")
		assert.Len(t, unclear, 2)
	})

	t.Run("each pair is clear of the rest", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"x": {"a": 0.9, "b": 0.1},
			"y": {"a": 0.2, "b": 0.7},
		}, threshold, margin)
		assert.Equal(t, map[string]similar.Pair{
			"x": {New: "x", Old: "a", Score: 0.9},
			"y": {New: "y", Old: "b", Score: 0.7},
		}, taken)
		assert.Empty(t, unclear)
	})
}
