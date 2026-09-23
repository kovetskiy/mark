package page

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/kovetskiy/mark/v16/metadata"
)

// indexNames are the files that stand for the directory they are in rather than
// sitting inside it, which is the convention every static site generator and
// code host already uses. Matched without regard to case, so README.md,
// readme.md and Readme.md are the same thing everywhere this package looks.
var indexNames = map[string]bool{
	"index":  true,
	"readme": true,
}

// isIndexFile reports whether a file stands for its directory.
func isIndexFile(file string) bool {
	base := filepath.Base(file)

	return indexNames[strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base)))]
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

// TitleResolver says what the page standing for a directory is called, given
// the directory and the document in the run that stands for it, if any. An
// empty answer leaves the directory named after itself.
type TitleResolver func(directory, indexFile string) (string, error)

// Hierarchy is what a run's file layout says about where its pages go: the
// parents a document's directories imply, the title a directory's own document
// takes, and the pages standing for directories, which are worth remembering.
//
// It is built once from the files of the run, so every question is answered
// from the same list. Asking the filesystem instead would let a README the
// pattern does not select name its directory's page, or stop that directory
// from being tracked, without ever being published.
type Hierarchy struct {
	root    string
	index   map[string]string
	outside []string
	resolve TitleResolver

	mu     sync.Mutex
	titles map[string]string
	claims map[string]string
}

// NewHierarchy lays out files under root. Paths are compared cleaned, so "docs",
// "./docs" and "docs/" name the same root.
func NewHierarchy(root string, files []string, resolve TitleResolver) *Hierarchy {
	h := &Hierarchy{
		root:    cleanPath(root),
		index:   map[string]string{},
		resolve: resolve,
		titles:  map[string]string{},
		claims:  map[string]string{},
	}

	for _, file := range files {
		relative, ok := h.relative(file)
		if !ok {
			h.outside = append(h.outside, file)
			continue
		}

		if isIndexFile(relative) {
			h.index[h.key(filepath.Dir(relative))] = file
		}
	}

	return h
}

// Outside lists the files of the run that the root does not cover. The path of
// such a file says nothing about where its page goes, which is worth knowing
// when the root was given by hand.
func (h *Hierarchy) Outside() []string {
	return h.outside
}

// cleanPath normalises a path for comparison. An empty result means no root.
func cleanPath(path string) string {
	if path == "" {
		return ""
	}

	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." {
		return ""
	}

	return cleaned
}

// relative reports file's path under the root, or false when the root does not
// cover it.
func (h *Hierarchy) relative(file string) (string, bool) {
	file = cleanPath(file)
	if h.root == "" {
		return file, true
	}

	return strings.CutPrefix(file, h.root+"/")
}

// key names a root-relative directory the way the run names it -- root and all
// -- which is what the manifest records and what the resolver is asked about.
// The root itself is "." relative, and its key is the root.
func (h *Hierarchy) key(relativeDir string) string {
	relativeDir = filepath.ToSlash(relativeDir)
	if relativeDir == "." || relativeDir == "" {
		return h.root
	}

	if h.root == "" {
		return relativeDir
	}

	return h.root + "/" + relativeDir
}

// directories names the directories between the root and a document, outermost
// first, as keys. A document standing for its directory is not under it.
func (h *Hierarchy) directories(file string) ([]string, bool) {
	relative, ok := h.relative(file)
	if !ok {
		return nil, false
	}

	dir := filepath.ToSlash(filepath.Dir(relative))

	var segments []string
	if dir != "." {
		segments = strings.Split(dir, "/")
	}

	if isIndexFile(relative) && len(segments) > 0 {
		segments = segments[:len(segments)-1]
	}

	keys := make([]string, 0, len(segments))
	for i := range segments {
		keys = append(keys, h.key(strings.Join(segments[:i+1], "/")))
	}

	return keys, true
}

// Parents reports the titles of the pages a document sits under, outermost
// first, as its directories imply them. A file outside the root has none.
func (h *Hierarchy) Parents(file string) ([]string, error) {
	keys, ok := h.directories(file)
	if !ok || len(keys) == 0 {
		return nil, nil
	}

	parents := make([]string, 0, len(keys))
	for _, key := range keys {
		title, err := h.title(key)
		if err != nil {
			return nil, err
		}
		parents = append(parents, title)
	}

	return parents, nil
}

// Title reports what a document standing for its directory is called: the
// directory's own title, which is also what the documents beneath it look for.
// Any other document, and one outside the root, gets "" and keeps its own.
//
// A document standing for the root itself is titled by the root directory. The
// only alternative was the filename, which is "Readme" wherever it applies.
func (h *Hierarchy) Title(file string) (string, error) {
	relative, ok := h.relative(file)
	if !ok || !isIndexFile(relative) {
		return "", nil
	}

	key := h.key(filepath.Dir(relative))
	if key == "" {
		return "", nil
	}

	return h.title(key)
}

// Directories reports the directories between the root and a document,
// outermost first, one for each parent Parents reports. They are the pages
// mark creates on the way to the document, and worth remembering: nothing else
// does, so when the last document under a directory goes, the page standing for
// it would otherwise be left behind with no one aware it was ever mark's.
func (h *Hierarchy) Directories(file string) []string {
	keys, _ := h.directories(file)

	return keys
}

// HasIndex reports whether a document in the run stands for the directory. Its
// page is that document's, recorded under the document's own path.
func (h *Hierarchy) HasIndex(directory string) bool {
	_, ok := h.index[directory]

	return ok
}

// title reports what the page standing for a directory is called, asking the
// resolver once per directory and falling back to the directory's own name.
//
// One answer, asked for in two places: by the document that is the directory's
// page, and by every document underneath it that has to name its parent. Worked
// out separately they disagreed, and a README that titled itself ended up
// beside an empty page named after its directory rather than being it.
func (h *Hierarchy) title(key string) (string, error) {
	h.mu.Lock()
	title, ok := h.titles[key]
	h.mu.Unlock()
	if ok {
		return title, nil
	}

	if h.resolve != nil {
		var err error
		title, err = h.resolve(key, h.index[key])
		if err != nil {
			return "", err
		}
	}

	if title == "" {
		title = metadata.TitleFromName(filepath.Base(key))
	}

	h.mu.Lock()
	h.titles[key] = title
	h.mu.Unlock()

	return title, nil
}

// Claim records that file publishes title in space, and reports the document
// that got there first, if any.
//
// Confluence allows one page of a given title per space, so two documents
// wanting the same title want the same page: the second overwrites the first
// and moves it under its own parents, leaving one page where two were meant
// and no sign that anything was lost. Deriving parents from the path makes that
// likely rather than unlucky -- every directory tends to hold a README, and
// "Overview" is a thing several of them will call a page -- so the claim is
// taken before anything is published, and the second document fails while the
// first keeps the page it already had.
//
// Titles are compared without regard to case, as Confluence compares them.
func (h *Hierarchy) Claim(space, title, file string) (string, bool) {
	if h == nil || title == "" {
		return "", false
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	key := space + "\x00" + strings.ToLower(title)
	if previous, taken := h.claims[key]; taken && previous != file {
		return previous, true
	}

	h.claims[key] = file

	return "", false
}
