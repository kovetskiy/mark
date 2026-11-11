// Package similar scores how alike two documents' texts are, and pairs
// documents that are clearly each other's closest match: what following a
// document renamed and edited at once needs.
//
// The measure is the Jaccard similarity of the two texts' word shingles: the
// sets of every run of Shingle consecutive words, lowercased, with markup and
// punctuation dropped. It is 1 for the same words in the same order and falls
// with every word changed, added or removed, each touching up to Shingle
// shingles. A text shorter than Shingle words is compared by runs as long as
// the shorter text, down to single words; an empty text is like nothing.
package similar

import (
	"html"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Shingle is how many consecutive words make one shingle.
const Shingle = 3

// Words splits text into lowercased runs of letters and digits.
func Words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

var (
	cdata    = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>`)
	comment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	tag      = regexp.MustCompile(`(?s)<[^>]*>`)
	linkDest = regexp.MustCompile(`\]\([^)]*\)`)
)

// StorageWords is the words of a page body in storage format: its text and
// the code in its CDATA sections, without tags, attributes or comments.
func StorageWords(storage string) []string {
	text := comment.ReplaceAllString(storage, " ")
	// Before tags are dropped, so code holding a "<" is not cut short.
	var code []string
	text = cdata.ReplaceAllStringFunc(text, func(section string) string {
		code = append(code, cdata.FindStringSubmatch(section)[1])
		return " \x00 "
	})
	text = html.UnescapeString(tag.ReplaceAllString(text, " "))
	for _, c := range code {
		text = strings.Replace(text, "\x00", " "+c+" ", 1)
	}

	return Words(text)
}

// MarkdownWords is the words of Markdown as a reader sees them: without
// comments, HTML or ac: tags, or link destinations, which a published page
// keeps in attributes.
func MarkdownWords(markdown string) []string {
	text := comment.ReplaceAllString(markdown, " ")
	text = linkDest.ReplaceAllString(text, "] ")
	text = tag.ReplaceAllString(text, " ")

	return Words(html.UnescapeString(text))
}

// Score is the Jaccard similarity of a's and b's shingles, from 0 to 1.
func Score(a, b []string) float64 {
	n := min(Shingle, len(a), len(b))
	if n == 0 {
		return 0
	}

	sa, sb := shingles(a, n), shingles(b, n)
	shared := 0
	for s := range sa {
		if sb[s] {
			shared++
		}
	}

	return float64(shared) / float64(len(sa)+len(sb)-shared)
}

func shingles(words []string, n int) map[string]bool {
	set := make(map[string]bool, len(words)-n+1)
	for i := 0; i+n <= len(words); i++ {
		set[strings.Join(words[i:i+n], "\x00")] = true
	}

	return set
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
// Unclear names each new document scoring at least threshold against some old
// one but taken with none, with every old one it scored that well against,
// best first.
func Match(scores map[string]map[string]float64, threshold, margin float64) (taken map[string]Pair, unclear map[string][]Pair) {
	bestOld := map[string][2]float64{}
	for _, row := range scores {
		for old, score := range row {
			bestOld[old] = top2(bestOld[old], score)
		}
	}

	taken, unclear = map[string]Pair{}, map[string][]Pair{}
	for doc, row := range scores {
		var bestNew [2]float64
		var close []Pair
		for old, score := range row {
			bestNew = top2(bestNew, score)
			if score >= threshold {
				close = append(close, Pair{New: doc, Old: old, Score: score})
			}
		}
		if len(close) == 0 {
			continue
		}
		slices.SortFunc(close, func(a, b Pair) int {
			if a.Score != b.Score {
				if a.Score > b.Score {
					return -1
				}
				return 1
			}
			return strings.Compare(a.Old, b.Old)
		})

		best := close[0]
		if best.Score-bestNew[1] >= margin && best.Score-bestOld[best.Old][1] >= margin {
			taken[doc] = best
		} else {
			unclear[doc] = close
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
