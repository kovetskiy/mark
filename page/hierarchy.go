package page

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/kovetskiy/mark/v16/metadata"
)

// indexNames are the files that stand for the directory they are in rather than
// sitting inside it, which is the convention every static site generator and
// code host already uses.
var indexNames = map[string]bool{
	"index":  true,
	"readme": true,
}

// GlobRoot reports the directory a --files pattern starts from.
//
// Everything up to the first segment holding a wildcard: "docs/**/*.md" begins
// at "docs", and so does "docs/*.md". A pattern with a wildcard in its first
// segment has no fixed root and answers "", which leaves the paths beneath it
// as they are.
func GlobRoot(pattern string) string {
	if pattern == "" {
		return ""
	}

	segments := strings.Split(filepath.ToSlash(pattern), "/")

	fixed := make([]string, 0, len(segments))
	for _, segment := range segments {
		if strings.ContainsAny(segment, "*?[{") {
			break
		}
		fixed = append(fixed, segment)
	}

	// The last fixed segment is the file itself when the pattern names one.
	if len(fixed) == len(segments) && len(fixed) > 0 {
		fixed = fixed[:len(fixed)-1]
	}

	return strings.Join(fixed, "/")
}

// PathHierarchy reports the parent titles a document's location implies, and
// the title to give it if it has none of its own.
//
// A document is placed under a page for each directory between the root and
// itself. An index file is not placed under a page named after its own
// directory -- it is that page, which is what makes a directory's own document
// its landing page rather than a child of an empty one.
//
// The returned title is only a suggestion, and only for an index file: every
// other document keeps whatever title it would have had.
func PathHierarchy(root, file string) (parents []string, title string) {
	file = filepath.ToSlash(file)
	root = strings.TrimSuffix(filepath.ToSlash(root), "/")

	relative := file
	if root != "" {
		trimmed, ok := strings.CutPrefix(file, root+"/")
		if !ok {
			// Outside the root the path says nothing about where the page goes.
			return nil, ""
		}
		relative = trimmed
	}

	directory := filepath.ToSlash(filepath.Dir(relative))
	if directory == "." {
		directory = ""
	}

	var segments []string
	if directory != "" {
		segments = strings.Split(directory, "/")
	}

	base := filepath.Base(relative)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	if indexNames[strings.ToLower(name)] {
		// The document is its directory's page, so its parents are the
		// directories above it and its title is the directory's own.
		if len(segments) == 0 {
			return nil, ""
		}

		parents = titles(segments[:len(segments)-1])

		return parents, metadata.TitleFromName(segments[len(segments)-1])
	}

	return titles(segments), ""
}

func titles(segments []string) []string {
	if len(segments) == 0 {
		return nil
	}

	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		out = append(out, metadata.TitleFromName(segment))
	}

	return out
}

// TitleClaims remembers which document laid claim to which page title.
//
// Confluence allows one page of a given title per space, so two documents
// wanting the same title want the same page: the second overwrites the first
// and moves it under its own parents, leaving one page where two were meant and
// no sign that anything was lost.
//
// Deriving parents from the path makes that likely rather than unlucky. Every
// directory tends to hold a README, and "Overview" is a thing several of them
// will call a page. The claim is taken before anything is published, so the
// second document fails and the first keeps the page it already had.
type TitleClaims struct {
	mu     sync.Mutex
	claims map[string]string
}

// NewTitleClaims returns an empty register.
func NewTitleClaims() *TitleClaims {
	return &TitleClaims{claims: map[string]string{}}
}

// Claim records that file publishes title in space, and reports the document
// that got there first, if any.
func (c *TitleClaims) Claim(space, title, file string) (string, bool) {
	if c == nil || title == "" {
		return "", false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := space + "\x00" + title
	if previous, taken := c.claims[key]; taken && previous != file {
		return previous, true
	}

	c.claims[key] = file

	return "", false
}
