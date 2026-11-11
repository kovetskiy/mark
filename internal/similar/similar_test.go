package similar_test

import (
	"context"
	"os"
	"strings"
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
	ten := "one two three four five six seven eight nine ten"

	assert.InDelta(t, 1, similar.Score(words(ten), words(strings.ToUpper(ten)+"!")), 1e-9)
	assert.Zero(t, similar.Score(nil, words(ten)), "an empty text is like nothing")
	assert.Zero(t, similar.Score(nil, nil), "two empty texts are not alike")

	// Eight shingles of ten words; the fifth word changed leaves five of
	// them on each side: 5 / (8 + 8 - 5).
	changed := strings.Replace(ten, "five", "5", 1)
	assert.InDelta(t, 5.0/11, similar.Score(words(ten), words(changed)), 1e-9)

	// Stubs too short to tell a rename by are alike to nothing, themselves
	// included.
	assert.Zero(t, similar.Score(words("TBD"), words("TBD")))
	assert.Zero(t, similar.Score(words("Coming soon, watch this space"), words("Coming soon, watch this space")))
}

func TestMarkdownWordsKeepsALessThan(t *testing.T) {
	assert.Equal(t,
		[]string{"if", "n", "10", "then", "later", "quote"},
		similar.MarkdownWords("if n < 10 then later > quote"),
	)
}

func TestStorageWordsKeepsACommentInCode(t *testing.T) {
	storage := `<ac:plain-text-body><![CDATA[<!-- open]]></ac:plain-text-body><p>Kept text.</p><!-- gone -->`

	assert.Equal(t, []string{"open", "kept", "text"}, similar.StorageWords(storage))
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
		}, nil, threshold, margin)
		assert.Equal(t, map[string]similar.Pair{"new": {New: "new", Old: "old", Score: 0.8}}, taken)
		assert.Empty(t, unclear)
	})

	t.Run("below the threshold is nothing", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{"new": {"old": 0.55}}, nil, threshold, margin)
		assert.Empty(t, taken)
		assert.Empty(t, unclear)
	})

	t.Run("two old ones within the margin", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"new": {"a": 0.8, "b": 0.75},
		}, nil, threshold, margin)
		assert.Empty(t, taken)
		assert.Equal(t, []similar.Pair{{New: "new", Old: "a", Score: 0.8}, {New: "new", Old: "b", Score: 0.75}}, unclear["new"])
	})

	t.Run("a runner-up below the threshold still counts", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"new": {"a": 0.65, "b": 0.58},
		}, nil, threshold, margin)
		assert.Empty(t, taken)
		assert.Len(t, unclear["new"], 1)
	})

	t.Run("one old one, two claimants", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"clear": {"old": 0.9},
			"weak":  {"old": 0.7},
		}, nil, threshold, margin)
		assert.Equal(t, map[string]similar.Pair{"clear": {New: "clear", Old: "old", Score: 0.9}}, taken)
		assert.Contains(t, unclear, "weak")

		taken, unclear = similar.Match(map[string]map[string]float64{
			"one":   {"old": 0.9},
			"other": {"old": 0.85},
		}, nil, threshold, margin)
		assert.Empty(t, taken, "neither leads the other by the margin")
		assert.Len(t, unclear, 2)
	})

	t.Run("a rival the new one cannot be paired with", func(t *testing.T) {
		scores := map[string]map[string]float64{"new": {"old": 0.7}}
		taken, unclear := similar.Match(scores, map[string]float64{"new": 0.65}, threshold, margin)
		assert.Empty(t, taken, "as alike to a document that still exists")
		assert.Contains(t, unclear, "new")

		taken, _ = similar.Match(scores, map[string]float64{"new": 0.2}, threshold, margin)
		assert.Contains(t, taken, "new")
	})

	t.Run("each pair is clear of the rest", func(t *testing.T) {
		taken, unclear := similar.Match(map[string]map[string]float64{
			"x": {"a": 0.9, "b": 0.1},
			"y": {"a": 0.2, "b": 0.7},
		}, nil, threshold, margin)
		assert.Equal(t, map[string]similar.Pair{
			"x": {New: "x", Old: "a", Score: 0.9},
			"y": {New: "y", Old: "b", Score: 0.7},
		}, taken)
		assert.Empty(t, unclear)
	})
}
