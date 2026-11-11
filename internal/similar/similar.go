// Package similar scores how alike two documents' texts are, and pairs
// documents that are clearly each other's closest match: what following a
// document renamed and edited at once needs.
//
// The measure is the Jaccard similarity of the two texts' word shingles: the
// sets of every run of Shingle consecutive words, lowercased, with markup and
// punctuation dropped. It is 1 for the same words in the same order and falls
// with every word changed, added or removed, each touching up to Shingle
// shingles. A text of fewer than MinWords words is like nothing: a stub such
// as "TBD" says nothing about which document it was.
package similar

import (
	"cmp"
	"html"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Shingle is how many consecutive words make one shingle.
const Shingle = 3

// MinWords is the fewest words a text needs to be like another at all.
const MinWords = 10

// Words splits text into lowercased runs of letters and digits.
func Words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

var (
	// markup is a CDATA section, a comment or a tag, matched left to right
	// so a "<" or "<!--" in code is not taken for markup.
	markup  = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>|<!--.*?-->|<[^>]*>`)
	comment = regexp.MustCompile(`(?s)<!--.*?-->`)
	// htmlTag is a tag written in Markdown, not a "<" in prose or code.
	htmlTag  = regexp.MustCompile(`<[A-Za-z/!?][^<>]*>`)
	linkDest = regexp.MustCompile(`\]\([^)]*\)`)
)

// StorageWords is the words of a page body in storage format: its text and
// the code in its CDATA sections, without tags, attributes or comments.
func StorageWords(storage string) []string {
	text := markup.ReplaceAllStringFunc(storage, func(match string) string {
		if code := markup.FindStringSubmatch(match)[1]; code != "" {
			return " " + code + " "
		}
		return " "
	})

	return Words(html.UnescapeString(text))
}

// MarkdownWords is the words of Markdown as a reader sees them: without
// comments, HTML or ac: tags, or link destinations, which a published page
// keeps in attributes.
func MarkdownWords(markdown string) []string {
	text := comment.ReplaceAllString(markdown, " ")
	text = linkDest.ReplaceAllString(text, "] ")
	text = htmlTag.ReplaceAllString(text, " ")

	return Words(html.UnescapeString(text))
}

// Text is a text's shingles, made once to compare it with many others.
type Text map[string]bool

// Shingles is the shingles of words; none for fewer than MinWords.
func Shingles(words []string) Text {
	if len(words) < MinWords {
		return nil
	}

	set := make(Text, len(words)-Shingle+1)
	for i := 0; i+Shingle <= len(words); i++ {
		set[strings.Join(words[i:i+Shingle], "\x00")] = true
	}

	return set
}

// Similarity is the Jaccard similarity of t and other, from 0 to 1.
func (t Text) Similarity(other Text) float64 {
	if len(t) == 0 || len(other) == 0 {
		return 0
	}
	if len(other) < len(t) {
		t, other = other, t
	}

	shared := 0
	for s := range t {
		if other[s] {
			shared++
		}
	}

	return float64(shared) / float64(len(t)+len(other)-shared)
}

// Score is the Jaccard similarity of a's and b's shingles, from 0 to 1.
func Score(a, b []string) float64 {
	return Shingles(a).Similarity(Shingles(b))
}

// Pair is a new document's score against an old one.
type Pair struct {
	New, Old string
	Score    float64
}

// Match pairs each new document with the old one it is clearly closest to,
// from scores[new][old]. A pair is taken when its score is at least threshold
// and leads by at least margin both the new document's next best old one and
// the old one's next best new one. That makes the pairing one-to-one: no two
// pairs taken share a document, since each leads all the others of both.
//
// Rival, when not nil, is a new document's best score against texts it cannot
// be paired with, such as documents that still exist, which it has to lead by
// the margin too: text shared by a whole set of documents, such as a
// template's, is then no reason to pair two of them. It is asked only about a
// document with a pair to take.
//
// Unclear names each new document scoring at least threshold against some old
// one but taken with none, with every old one it scored that well against,
// best first.
func Match(
	scores map[string]map[string]float64, rival func(doc string) float64, threshold, margin float64,
) (taken map[string]Pair, unclear map[string][]Pair) {
	bestOld := map[string][2]float64{}
	for _, row := range scores {
		for old, score := range row {
			bestOld[old] = top2(bestOld[old], score)
		}
	}

	taken, unclear = map[string]Pair{}, map[string][]Pair{}
	for doc, row := range scores {
		var bestNew [2]float64
		var near []Pair
		for old, score := range row {
			bestNew = top2(bestNew, score)
			if score >= threshold {
				near = append(near, Pair{New: doc, Old: old, Score: score})
			}
		}
		if len(near) == 0 {
			continue
		}
		slices.SortFunc(near, func(a, b Pair) int {
			return cmp.Or(cmp.Compare(b.Score, a.Score), strings.Compare(a.Old, b.Old))
		})

		best := near[0]
		ahead := best.Score-bestNew[1] >= margin && best.Score-bestOld[best.Old][1] >= margin
		if ahead && rival != nil {
			ahead = best.Score-rival(doc) >= margin
		}
		if ahead {
			taken[doc] = best
		} else {
			unclear[doc] = near
		}
	}

	return taken, unclear
}

// top2 is the two highest of best and score, highest first.
func top2(best [2]float64, score float64) [2]float64 {
	switch {
	case score > best[0]:
		return [2]float64{score, best[0]}
	case score > best[1]:
		return [2]float64{best[0], score}
	}

	return best
}
