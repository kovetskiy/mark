package page

import (
	"os"
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
func PathHierarchy(root, file string, titles *DirectoryTitles) (parents []string, title string, err error) {
	file = filepath.ToSlash(file)
	root = strings.TrimSuffix(filepath.ToSlash(root), "/")

	relative := file
	if root != "" {
		trimmed, ok := strings.CutPrefix(file, root+"/")
		if !ok {
			// Outside the root the path says nothing about where the page goes.
			return nil, "", nil
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
			return nil, "", nil
		}

		parents, err = resolveTitles(root, segments[:len(segments)-1], titles)
		if err != nil {
			return nil, "", err
		}

		own, err := titles.Title(joinPath(root, segments))
		if err != nil {
			return nil, "", err
		}

		return parents, own, nil
	}

	parents, err = resolveTitles(root, segments, titles)

	return parents, "", err
}

func joinPath(root string, segments []string) string {
	path := strings.Join(segments, "/")
	if root != "" {
		return root + "/" + path
	}

	return path
}

// resolveTitles names each directory on the way down.
func resolveTitles(root string, segments []string, titles *DirectoryTitles) ([]string, error) {
	if len(segments) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(segments))
	for i := range segments {
		title, err := titles.Title(joinPath(root, segments[:i+1]))
		if err != nil {
			return nil, err
		}
		out = append(out, title)
	}

	return out, nil
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

// DirectoryKeys reports the directories a document's derived parents stand for,
// outermost first, as paths rather than titles.
//
// "docs/guides/deep/setup.md" under root "docs" gives docs/guides and
// docs/guides/deep, which are the pages mark creates on the way to it. They are
// worth remembering: nothing else does, so when the last document under a
// directory goes, the page standing for that directory would otherwise be left
// behind with no one aware it was ever mark's.
//
// An index file is not counted as its own directory, having a page of its own
// under its own name already.
func DirectoryKeys(root, file string) []string {
	file = filepath.ToSlash(file)
	root = strings.TrimSuffix(filepath.ToSlash(root), "/")

	relative := file
	if root != "" {
		trimmed, ok := strings.CutPrefix(file, root+"/")
		if !ok {
			return nil
		}
		relative = trimmed
	}

	directory := filepath.ToSlash(filepath.Dir(relative))
	if directory == "." {
		return nil
	}

	segments := strings.Split(directory, "/")

	base := filepath.Base(relative)
	if indexNames[strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base)))] {
		segments = segments[:len(segments)-1]
	}

	keys := make([]string, 0, len(segments))
	for i := range segments {
		path := strings.Join(segments[:i+1], "/")
		if root != "" {
			path = root + "/" + path
		}
		keys = append(keys, path)
	}

	return keys
}

// HasIndexFile reports whether a directory holds a document of its own.
//
// One that does owns the directory's page under its own path, and recording the
// directory as well would leave two entries claiming a single page.
func HasIndexFile(directory string) bool {
	for _, name := range []string{"index.md", "README.md", "readme.md", "Index.md"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
			return true
		}
	}

	return false
}

// DirectoryTitles answers what the page standing for a directory is called.
//
// One answer, asked for in two places: by the document that is the directory's
// page, and by every document underneath it that has to name its parent. Worked
// out separately they disagreed, and a README that titled itself ended up beside
// an empty page named after its directory rather than being it.
type DirectoryTitles struct {
	resolve func(directory string) (string, error)

	mu     sync.Mutex
	cached map[string]string
}

// NewDirectoryTitles returns a register that asks resolve once per directory.
func NewDirectoryTitles(resolve func(directory string) (string, error)) *DirectoryTitles {
	return &DirectoryTitles{resolve: resolve, cached: map[string]string{}}
}

// Title reports what the directory's page is called, falling back to the
// directory's own name.
func (d *DirectoryTitles) Title(directory string) (string, error) {
	name := metadata.TitleFromName(filepath.Base(directory))

	if d == nil || d.resolve == nil {
		return name, nil
	}

	d.mu.Lock()
	if title, ok := d.cached[directory]; ok {
		d.mu.Unlock()

		return title, nil
	}
	d.mu.Unlock()

	title, err := d.resolve(directory)
	if err != nil {
		return "", err
	}
	if title == "" {
		title = name
	}

	d.mu.Lock()
	d.cached[directory] = title
	d.mu.Unlock()

	return title, nil
}
