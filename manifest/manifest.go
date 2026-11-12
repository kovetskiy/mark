// Package manifest records which Confluence page each source file publishes to,
// so that a page can be found again after its title changes.
//
// mark identifies a page by its title. That title is derived from three
// independently mutable things -- the `Title` header, the leading H1, and the
// filename -- so editing any of them makes the existing page unfindable and mark
// publishes a second one beside it. The fix is to stop asking "what is this
// page called" and start asking "which page did this file publish to last
// time", which needs somewhere to remember the answer.
//
// The mapping lives in Confluence rather than in the repository, keyed on the
// source path. Nothing is written back to the working tree.
//
// # Where it is kept
//
// On Cloud, a space property. On Server and Data Center, which have no such
// thing -- space properties exist only in the v2 API -- a content property on
// the space homepage instead.
//
// Whatever holds it has to be reachable from a space key alone, with no search.
// That rules out the arrangement that first suggests itself, a property on each
// page naming its own source file: content properties are not CQL-searchable
// without a Connect or Forge app, so finding the property that names a page id
// would require already knowing that page id. A single manifest is reachable
// either way -- a space by its key, a homepage through FindHomePage.
//
// Cloud keeps the space property rather than using the homepage for both,
// because content properties are a v1 endpoint and v1 is exactly what a scoped
// API token cannot reach. Cloud has a v2 option; it should not inherit that
// failure mode for no reason.
//
// Anchoring to the homepage on Server does mean a space whose homepage is
// changed loses its manifest and starts tracking over. That costs the rename
// protection until the next run rebuilds it, and nothing else -- no page is
// touched by it.
//
// Labels are the other candidate, and are searchable, but they are visible in
// the UI and any reader can remove them. Identity a passer-by can delete is not
// identity.
//
// # Why it is split
//
// Confluence bounds the size of a property value. A single blob would therefore
// impose a ceiling on how many files a repository may have -- and would fail by
// publishing every page and then erroring on the write, which is the worst
// moment to find out. The mapping is spread over a fixed set of properties
// instead, each holding the paths that hash to it, so the bound applies per
// shard rather than to the repository.
//
// Each entry holds the page id and the title the path was last published under.
// The title is not decoration: mark resolves parent pages and ancestry by
// title, so a page being renamed strands every reference to its old name, and
// the recorded title is the only way to connect the two.
package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/internal/globpattern"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
)

// Key normalises a path or glob into the form the mapping is keyed by.
//
// The key has to mean the same thing wherever mark is run from, and the paths
// it is handed do not: a glob of "$PWD/docs/*.md" yields absolute paths and one
// of "docs/*.md" yields relative ones, for exactly the same files. Keyed as
// given, a CI job and a developer keep two disjoint mappings of one repository,
// neither aware of the other, and an absolute key additionally embeds a
// checkout directory that does not survive being moved.
//
// Anything at or below the working directory is stored relative to it, which
// makes the two forms agree whenever they are run from the same place -- the
// case that actually happens. A path outside it has no better anchor available
// and is kept as it came: mark has no notion of a project root to measure from,
// and inventing one from the glob would move every key the first time somebody
// widened it.
func Key(path string) string {
	cleaned := filepath.Clean(path)

	if filepath.IsAbs(cleaned) {
		if cwd, err := os.Getwd(); err == nil {
			rel, err := filepath.Rel(cwd, cleaned)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				cleaned = rel
			}
		}
	}

	// Stored with forward slashes whatever the platform. The separator is a
	// property of the machine, not of the repository, and leaving it in would
	// give a team publishing from Windows and from Linux CI two entries for
	// every file -- the same split this function exists to close, along a
	// different axis.
	return filepath.ToSlash(cleaned)
}

// PropertyKeyPrefix is the common prefix of the properties the manifest is
// stored under unless a run asks for another; see SetPropertyKeyPrefix. Each
// shard is the prefix plus a dot and its index.
const PropertyKeyPrefix = "mark.manifest"

// FolderPropertyKey holds the folder mapping, under the default prefix.
//
// Kept apart from the page shards rather than mixed in: folders are keyed by
// the titles declared in a header rather than by a source path, they are few
// enough that splitting them buys nothing, and a folder key appearing among the
// page keys would be reported as a source file that had gone missing.
const FolderPropertyKey = PropertyKeyPrefix + folderSuffix

// ParentPropertyKey holds the parent mapping, under the default prefix.
//
// Kept apart from the page shards for the reasons the folder mapping is: it is
// keyed by the chain of titles a document declares rather than by a source
// path, there are few enough of them that splitting them buys nothing, and a
// parent key among the page keys would be reported as a source file that had
// gone missing.
const ParentPropertyKey = PropertyKeyPrefix + parentSuffix

const (
	folderSuffix = ".folders"
	parentSuffix = ".parents"
)

// FolderPropertyKeyFor and ParentPropertyKeyFor name the folder and parent
// mappings under a prefix of the caller's choosing.
func FolderPropertyKeyFor(prefix string) string { return prefix + folderSuffix }
func ParentPropertyKeyFor(prefix string) string { return prefix + parentSuffix }

// validPrefix is what a property key prefix may be made of: the characters
// the default is made of. Confluence is not forthcoming about what a key may
// contain, and a prefix that reads as a key nobody would mistake for another
// program's is the point of choosing one.
var validPrefix = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

// ValidatePropertyKeyPrefix reports whether prefix can name a manifest.
func ValidatePropertyKeyPrefix(prefix string) error {
	if !validPrefix.MatchString(prefix) {
		return fmt.Errorf(
			"%q cannot prefix a property key: use letters, digits, '_', '-' and single dots, as in %q",
			prefix, PropertyKeyPrefix,
		)
	}

	return nil
}

// NormalizePropertyKeyPrefix returns prefix as a Store uses it: trimmed, with
// blank meaning PropertyKeyPrefix, and validated.
func NormalizePropertyKeyPrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return PropertyKeyPrefix, nil
	}

	if err := ValidatePropertyKeyPrefix(prefix); err != nil {
		return "", err
	}

	return prefix, nil
}

// ShardCount is how many properties the mapping is spread over.
//
// Fixed rather than grown on demand: the shard a path belongs to is derived
// from the path, so changing the count would move every entry and orphan the
// lot. Sixteen keeps one page of a property listing sufficient to read them all
// in a single request, while giving the per-property size bound sixteen times
// the room a single blob had.
const ShardCount = 16

// shardSizeWarning is the encoded size at which a shard is reported as getting
// large.
//
// This is not Confluence's limit. The real cap is a property of the instance
// and not something this code can know, so the number here is a conservative
// point at which to say something while there is still room to act, rather than
// a boundary that is enforced.
const shardSizeWarning = 24 * 1024

// PropertyKey returns the property key for a shard index under the default
// prefix.
func PropertyKey(shard int) string {
	return PropertyKeyFor(PropertyKeyPrefix, shard)
}

// PropertyKeyFor returns the property key for a shard index under prefix.
func PropertyKeyFor(prefix string, shard int) string {
	return fmt.Sprintf("%s.%d", prefix, shard)
}

// isManifestKey reports whether key is one of the properties a manifest under
// prefix is kept in: a shard, or the folder or parent mapping.
func isManifestKey(prefix, key string) bool {
	_, shard := shardIndex(prefix, key)
	return shard || key == FolderPropertyKeyFor(prefix) || key == ParentPropertyKeyFor(prefix)
}

// shardIndex recovers a shard number from a property key, reporting false for
// any key that is not one of ours -- a space or page may hold properties put
// there by something else entirely, or by a run keeping its manifest under
// another prefix in the same place.
func shardIndex(prefix, key string) (int, bool) {
	suffix, ok := strings.CutPrefix(key, prefix+".")
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(suffix)
	if err != nil || index < 0 || index >= ShardCount {
		return 0, false
	}
	return index, true
}

// ShardFor reports which shard holds a path, so that a caller reading the
// properties directly -- with PropertyKey -- can find the one a given path's
// mapping lives in without duplicating the hash.
func ShardFor(path string) int { return shardFor(path) }

// shardFor picks the shard a path belongs to.
//
// FNV rather than anything cryptographic: this only has to spread paths evenly,
// and paths in a repository share long prefixes, which a checksum would smear
// out but a truncation would not.
func shardFor(path string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(path))
	return int(h.Sum32() % ShardCount)
}

// formatVersion guards the stored shape. A manifest written by a future mark
// with a shape this one does not understand is ignored rather than
// misinterpreted, which costs a run of title-based resolution instead of
// corrupting the mapping.
const formatVersion = 1

// Entry is what one source file published to.
//
// Title is the title the path was last published under, which is not the same
// as the page's title now -- the page may have been retitled since, and the
// point of recording it is to notice exactly that. It is what lets a stale
// reference to the old title be followed to the page that carries it; see
// ResolveStaleTitle.
type Entry struct {
	PageID string `json:"pageId"`
	Title  string `json:"title"`

	// Hash fingerprints the document's source. It is what makes a renamed file
	// recognisable: a rename changes the key, so the entry can only be found
	// again by what it holds rather than by where it was.
	Hash string `json:"hash,omitempty"`

	// Glob is the --files pattern that published this path. Absence from a run
	// only means the file is gone if the run was looking where the file would
	// have been; a run narrowed to one directory says nothing about any other.
	Glob string `json:"glob,omitempty"`

	// Version is the page version mark last wrote. A page whose version has
	// moved on since is one somebody else has edited, which is the whole basis
	// of --no-overwrite. Zero means mark has never recorded one -- an entry
	// written before this was tracked -- and nothing can be concluded from it.
	Version int64 `json:"version,omitempty"`
}

// folderDocument is the folder mapping as stored.
type folderDocument struct {
	Version int               `json:"version"`
	Folders map[string]string `json:"folders"`
}

// parentDocument is the parent mapping as stored.
type parentDocument struct {
	Version int               `json:"version"`
	Parents map[string]string `json:"parents"`
}

// document is one shard as stored.
type document struct {
	Version int              `json:"version"`
	Pages   map[string]Entry `json:"pages"`
}

// Store is the manifest for one or more spaces, loaded lazily and written back
// once at the end of a run.
//
// Spaces are loaded on first use rather than up front because which spaces a
// run touches is only known as its files are read: the `Space` header is
// per-document, so a single run can publish into several.
type Store struct {
	api *confluence.API

	// mu guards everything below. Run publishes one file at a time today, so
	// nothing contends -- but Store is reachable from exported entry points and
	// an unsynchronised map is a hard throw rather than a wrong answer, which
	// is a poor thing to leave for whoever parallelises publishing.
	mu sync.Mutex

	// runGlob is the pattern this run was given, recorded with each entry so a
	// later run can tell whether it was looking where a missing file used to be.
	runGlob string

	// named holds the keys of the exact paths SetRunPaths was given. One the
	// pattern does not match is recorded under its own path, escaped, so an
	// entry's pattern never grows with the number of paths a run names.
	named map[string]bool

	// readOnly makes Save a no-op. A dry run has to resolve exactly as a real
	// one does or its preview is fiction, but resolving records what it finds,
	// and a recording would otherwise be written. Refusing at the point of
	// writing is the only guard that holds however the store is used -- the
	// alternative, not passing it to the parts that record, was tried and let a
	// dry run write to Confluence.
	readOnly bool

	// runFiles is every path this run will publish, known up front because the
	// whole file set is globbed before any of it is processed. A recorded path
	// missing from it is either a deleted file or a renamed one, and telling
	// those apart is the whole of rename detection.
	runFiles map[string]bool

	// documentScopes holds, for each path SetRunDocuments named, the scope
	// its entry is recorded under in place of runGlob; "" is none.
	documentScopes map[string]string

	// scopeKeys holds, for each scope SetScopeKeys gave the whole key set of,
	// as recorded, the keys it named as still existing.
	scopeKeys map[string]map[string]bool

	// scopes holds every scope the run publishes or names keys in, as it is
	// recorded.
	scopes map[string]bool

	// warningsFor gives the collector of the document a key names; see
	// SetWarningsFor.
	warningsFor func(key string) *report.Warnings

	// partial says the run's files are some of a pattern's, not all of them:
	// see SetPartial and covers.
	partial bool

	// sharedWarned holds the shared pages already warned of, by space, page
	// and the two paths, so that a document recorded again in the run does
	// not tell either of them twice.
	sharedWarned map[sharedPage]bool

	// spaces holds the manifest for each space key touched so far, alongside
	// the property it was read from -- the version in that property is what a
	// write has to supersede.
	spaces map[string]*spaceState

	// manifestPage names a page, by id or by title, to keep the manifest on as
	// content properties instead of where the deployment would otherwise put
	// it. Empty means the default backend; see SetManifestPage.
	manifestPage string

	// manifestPageOptional lets a space whose manifest page does not exist run
	// untracked instead of failing; see SetManifestPageOptional.
	manifestPageOptional bool

	// prefix is what every property of this manifest is named under; see
	// SetPropertyKeyPrefix.
	prefix string
}

// backend is where a space's manifest lives.
type backend int

const (
	// spaceProperties are the Cloud default: properties of the space itself,
	// addressed by spaceID.
	spaceProperties backend = iota
	// pageProperties are content properties of the page contentID names,
	// written through the v2 API. Cloud, when a page was asked for.
	pageProperties
	// contentProperties are the same properties written through v1, which is
	// all Server and Data Center have.
	contentProperties
)

type spaceState struct {
	backend   backend
	spaceID   string
	contentID string

	// untracked marks a space whose manifest page was missing when it was
	// loaded, under SetManifestPageOptional. It starts from an empty manifest,
	// which is what it has: no manifest exists where it was asked to be kept.
	untracked bool
	// lateChecked records that Save has looked for the manifest page once
	// more and found nowhere to save to, so the save on the way out of a run
	// does not ask, or warn, again.
	lateChecked bool

	// prefix is what this space's properties were read under, and so what
	// they are written back under.
	prefix string

	shards [ShardCount]shard

	// byPage is the reverse of the mapping, used to notice two paths claiming
	// the same page. Rebuilt on load and maintained by Record.
	byPage map[string]string

	// folders maps a declared folder path to the folder it resolved to, so a
	// folder renamed in Confluence is found again instead of being recreated
	// beside itself.
	folders        map[string]string
	folderProperty *confluence.Property
	foldersDirty   bool

	// parents maps a declared parent chain to the page it resolved to, so a
	// parent renamed in Confluence is found again instead of a second page
	// being created under the title the document still declares.
	//
	// Distinct from titles, which only knows pages mark published from a source
	// file. Most parents are not that: they are pages somebody made in the UI,
	// or the empty ones mark created to hold a hierarchy, and neither has ever
	// been in the title index.
	parents        map[string]string
	parentProperty *confluence.Property
	parentsDirty   bool

	// titles maps the title each path was published under *as read*, and is
	// deliberately never updated during a run. Resolving a stale reference asks
	// "what did the last run call this page", and Record answers a different
	// question -- so if this tracked the live map, a document processed after
	// the page it points at would find the title already rewritten and the
	// reference unresolvable. An empty page id marks a title that more than one
	// path claimed, which is ambiguous and not followed.
	titles map[string]string

	// seen records the paths this run published, which is what makes the
	// difference between "this file is gone" and "this run did not look at it".
	seen map[string]bool

	// claimed records pages already taken by a file this run, so a rename can
	// never rebind onto a page another document is publishing to.
	claimed map[string]bool

	// added records the paths this run recorded that were not tracked under
	// the same pattern or scope before it, under their own path or one they
	// moved or were renamed from.
	added map[string]bool

	// byHash names, for each content fingerprint, the paths recorded with it,
	// so that a rename is looked for among those alone rather than over every
	// entry for each document. It may name a path since dropped or recorded
	// again with another fingerprint, so what it names is checked against the
	// entry; every path whose entry has the fingerprint is in it.
	byHash map[string]map[string]bool
}

// shard is one property's worth of the mapping.
type shard struct {
	// property is what this shard was read from, and is what a write has to
	// supersede. Nil means the property does not exist yet.
	property *confluence.Property
	pages    map[string]Entry
	dirty    bool
}

// NewStore returns a Store that reads and writes through api.
func NewStore(api *confluence.API) *Store {
	return &Store{
		api:      api,
		spaces:   map[string]*spaceState{},
		runFiles: map[string]bool{},
		prefix:   PropertyKeyPrefix,
	}
}

// SetPropertyKeyPrefix names the manifest's properties under prefix instead of
// the default, so that two projects publishing into one space -- which is not
// always avoidable -- keep manifests of their own rather than reading and
// rewriting each other's, and reporting each other's files as gone.
//
// Set before the first space is loaded: what has been read under one prefix
// would otherwise be written back under another.
func (s *Store) SetPropertyKeyPrefix(prefix string) error {
	prefix, err := NormalizePropertyKeyPrefix(prefix)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefix = prefix

	return nil
}

// NewReadOnlyStore returns a Store that answers every question the writing one
// does and never persists anything.
func NewReadOnlyStore(api *confluence.API) *Store {
	store := NewStore(api)
	store.readOnly = true
	return store
}

// SetManifestPage keeps the manifest as content properties of one page, named
// by id or by title, instead of where the deployment would otherwise put it.
//
// On Cloud the default is a space property, and creating one takes space
// administration -- a lot to grant a publisher for the sake of a bookkeeping
// record, and something a scoped API token is refused outright. A content
// property needs only the right to edit the page it sits on, which a publisher
// has by definition, and on Cloud it is written through the v2 API, which a
// scoped token can reach. A space holding several independent mirrors can also
// give each its own manifest this way.
//
// A title is looked up in each space the run touches; an id is used as it is,
// and so names the same page for every space -- which a run publishing to more
// than one is refused, since one page holds one space's manifest.
func (s *Store) SetManifestPage(page string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manifestPage = strings.TrimSpace(page)
}

// SetManifestPageOptional lets a space whose manifest page, set with
// SetManifestPage, does not exist run untracked rather than fail: nothing is
// found through an earlier mapping there, since none exists, and none is
// written unless the page exists by the time Save is called -- a run may
// publish it itself, after the documents that belong under it. Untracked and
// UntrackedSpaces say which spaces ran so. Without it, a missing manifest page
// is a *MissingPageError from the first call that needs the space.
//
// Only a page that is not there is excused. Any other failure to look it up is
// still an error.
func (s *Store) SetManifestPageOptional(optional bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manifestPageOptional = optional
}

// MissingPageError is the error a Store returns when the page SetManifestPage
// names is not a page in a space it is asked about.
type MissingPageError struct {
	// Page is the title or id the manifest page was given as.
	Page string
	// Space is the key of the space it was looked for in.
	Space string
}

func (e *MissingPageError) Error() string {
	return fmt.Sprintf("--manifest-page names %q, which is not a page in space %q", e.Page, e.Space)
}

// Untracked reports whether spaceKey, as loaded so far, is running untracked
// because its manifest page is missing; see SetManifestPageOptional. A space
// not loaded yet is not, and neither is any space of a nil Store, which tracks
// nothing to begin with.
func (s *Store) Untracked(spaceKey string) bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.spaces[spaceKey]
	return ok && state.untracked
}

// UntrackedSpaces returns, sorted, the spaces running untracked because their
// manifest page is missing. Called after Save, it is the spaces whose mapping
// was not saved, a space whose page the run published itself being tracked
// from then on. A nil Store has none.
func (s *Store) UntrackedSpaces() []string {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var spaces []string
	for _, key := range s.sortedSpaces() {
		if s.spaces[key].untracked {
			spaces = append(spaces, key)
		}
	}

	return spaces
}

// SetRunFiles tells the store every path this run intends to publish.
//
// Without it a recorded path that is absent from the run cannot be told apart
// from one the run simply did not cover, and nothing can be concluded from its
// absence -- least of all that the file it names was renamed.
func (s *Store) SetRunFiles(glob string, paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runGlob = Key(glob)
	s.runFiles = make(map[string]bool, len(paths))
	s.documentScopes = nil
	s.scopeKeys = nil
	s.scopes = nil
	for _, path := range paths {
		s.runFiles[Key(path)] = true
	}
}

// SetRunPaths tells the store the exact paths this run names besides the
// pattern given to SetRunFiles, deleted ones included. They are taken as
// written, with no glob meaning. One the pattern does not match is recorded
// under a pattern naming only itself, and a run naming it covers an entry
// recorded so; a partial run covers each of them whatever recorded it. The
// files among them that exist still go to SetRunFiles.
func (s *Store) SetRunPaths(paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.named = make(map[string]bool, len(paths))
	for _, path := range paths {
		s.named[Key(path)] = true
	}
}

// recordGlob is the pattern a path this run publishes is recorded under, but
// for what a partial run keeps from an existing entry: the run's pattern, or
// for a named path it does not match, the path's own.
func (s *Store) recordGlob(path string) string {
	if s.named[path] && !matches(s.runGlob, path) {
		return globpattern.Escape(path)
	}

	return s.runGlob
}

// documentScopePrefix starts the pattern a document scope is recorded under.
// Key never leaves "//" in a path or a pattern, so no --files pattern, and no
// path recorded under its own, can be one.
const documentScopePrefix = "documents://"

// documentScope is what an entry of the documents of scope is recorded under;
// none for no scope.
func documentScope(scope string) string {
	if scope == "" {
		return ""
	}

	return documentScopePrefix + scope
}

// IsDocumentScope reports whether an entry recorded under glob, its
// Entry.Glob, belongs to a document scope, a run's or another's, rather than
// to a --files pattern.
func IsDocumentScope(glob string) bool {
	return strings.HasPrefix(glob, documentScopePrefix)
}

// documentDirectoryPrefix starts the key of a page standing for a directory
// that only documents lie in, as DocumentDirectoryKey spells it.
const documentDirectoryPrefix = "documents:"

// DocumentDirectoryKey is what the page standing for directory is recorded
// under when only documents of scope lie in it, documents a library caller
// handed over from memory rather than files: a key of its own, so that it is
// never the entry of a directory of files with the same path, which belongs
// to a --files pattern rather than to the scope. Pass it to SetRunDocuments
// with the run's other keys of the scope.
//
// The scope and the directory are each escaped into one path segment, so
// that Key, which cleans every path it records, leaves the key as it is and
// no file path can be one: a file path's first segment would have to be
// "documents:" followed by the scope.
func DocumentDirectoryKey(scope, directory string) string {
	return documentDirectoryPrefix + url.PathEscape(scope) + "/" + url.PathEscape(Key(directory))
}

// IsDocumentDirectoryKey reports whether key is spelled as DocumentDirectoryKey
// spells one, and so would be taken for a directory's if a document were
// recorded under it.
func IsDocumentDirectoryKey(key string) bool {
	return isDocumentDirectory(Key(key))
}

// isDocumentDirectory reports whether a recorded key is one
// DocumentDirectoryKey made.
func isDocumentDirectory(path string) bool {
	return strings.HasPrefix(path, documentDirectoryPrefix)
}

// SetRunDocuments adds to the run's paths ones that no --files pattern
// matched, such as documents a library caller handed over from memory, each
// recorded under scope rather than the run's pattern. Call it after
// SetRunFiles, which starts the run's paths afresh.
//
// scope stands in for the pattern in everything the pattern decides: an
// entry recorded under it is an orphan to a later run with the same scope
// that does not publish it, and only entries under it are taken for a
// rename or a move of one of these paths. An empty scope records the paths
// under none, so they are never orphans and never renames, and are found
// again only by their own path. A run of files never covers an entry under
// a scope, nor a run of documents a file's.
//
// Under SetPartial a run covers only the keys of a scope it publishes, and
// leaves the scope's other entries alone, unless SetScopeKeys names the rest.
func (s *Store) SetRunDocuments(scope string, paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	glob := s.addScope(scope)
	if s.documentScopes == nil {
		s.documentScopes = make(map[string]string, len(paths))
	}

	for _, path := range paths {
		key := Key(path)
		s.runFiles[key] = true
		s.documentScopes[key] = glob
	}
}

// SetScopeKeys names, for a partial run, every key of scope that still exists
// besides those of the documents the run publishes; theirs may be named too.
// The run then knows the scope's whole key set and covers all of it, as a run
// without SetPartial does: an entry of the scope under a key neither published
// nor named is gone, so it is an orphan, the previous key of a document with
// the same content, or the owner a moved document's page is followed from. A
// named key is none of those and is left as it is, tracked.
//
// Without it a partial run covers only the keys it publishes in a scope, so it
// finds no orphans there and follows a document by its own key alone. No keys,
// or no scope, name nothing. Call it after SetRunFiles.
func (s *Store) SetScopeKeys(scope string, keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if scope == "" || len(keys) == 0 {
		return
	}

	glob := s.addScope(scope)
	if s.scopeKeys == nil {
		s.scopeKeys = map[string]map[string]bool{}
	}

	listed := make(map[string]bool, len(keys))
	for _, key := range keys {
		listed[Key(key)] = true
	}
	s.scopeKeys[glob] = listed
}

// addScope notes scope as one of the run's and returns what it is recorded
// under. Called with s.mu held.
func (s *Store) addScope(scope string) string {
	glob := documentScope(scope)
	if glob != "" {
		if s.scopes == nil {
			s.scopes = map[string]bool{}
		}
		s.scopes[glob] = true
	}

	return glob
}

// coversDocument is covers for an entry recorded under a document scope: one
// of the run's scopes, and under SetPartial only a key the run publishes in
// it. Given the scope's whole key set by SetScopeKeys, it covers every key but
// those named as still existing, which are not gone and so neither orphans,
// renamed nor moved. A partial run covers no directory of documents: the
// keys it is given say nothing of where their documents lie, and one it lays
// out holds a document of its own, which may never record it. Called with
// s.mu held.
func (s *Store) coversDocument(path string, entry Entry) bool {
	if !s.scopes[entry.Glob] {
		return false
	}

	listed := s.scopeKeys[entry.Glob]
	if isDocumentDirectory(path) && (s.partial || listed != nil) {
		return false
	}

	if listed != nil {
		return !listed[path]
	}

	if !s.partial {
		return true
	}

	scope, ok := s.documentScopes[path]
	return ok && scope == entry.Glob
}

// sameScope reports whether entry belongs where path does: under the scope
// of the document path names, or, for a file, under no document scope.
// Called with s.mu held.
func (s *Store) sameScope(path string, entry Entry) bool {
	if scope, document := s.documentScopes[path]; document {
		return scope != "" && entry.Glob == scope
	}

	return !IsDocumentScope(entry.Glob)
}

// SetWarningsFor has what Record finds wrong with a document added to the
// collector warningsFor gives for its key, as Key spells it; a nil function,
// or a nil collector for a key, only logs it. warningsFor is called with the
// store's lock held, so it must not call the store.
func (s *Store) SetWarningsFor(warningsFor func(key string) *report.Warnings) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.warningsFor = warningsFor
}

// warn adds a warning about the document key names. Called with s.mu held.
func (s *Store) warn(key string, kind report.WarningKind, format string, args ...any) {
	var warnings *report.Warnings
	if s.warningsFor != nil {
		warnings = s.warningsFor(key)
	}

	if warnings == nil {
		warnings = report.NewWarnings(key)
	}

	warnings.Add(kind, format, args...)
}

// SetPartial says this run publishes only some of the files of the pattern
// that recorded them, such as the files a change touched.
//
// A partial run is about the tracked paths its own pattern matches, whatever
// pattern recorded them: one of those missing from the run is gone, and is an
// orphan or the previous name of a renamed file, while every other path is
// simply not this run's business. Recording a page leaves its entry under the
// pattern it already had, or the one the file it moved from had, so the run
// that publishes the whole pattern still notices when the file is deleted. A
// path with neither is recorded under the run's pattern when entries already
// carry it, the pattern being a full run's, and otherwise under a pattern
// naming only itself until such a run publishes it, so every partial run
// naming it covers it.
//
// The tracked paths recorded under the very pattern a partial run was given,
// when it holds glob syntax, are left to the run publishing them all unless
// SetRunPaths names them: on a checkout holding only some of the files, the
// absence of the rest says nothing, so they are neither orphans nor the old
// names of moved or renamed files.
func (s *Store) SetPartial(partial bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.partial = partial
}

// warnShared tells both paths that they publish to one page, once a run,
// naming them in an order that does not depend on which was recorded last.
// hint, which may be empty, is added to the message as it is; it is no part of
// what makes two warnings the same, since only a pair's first warning can carry
// it: it needs one path absent from the run, and by a second warning both have
// been recorded. Called with s.mu held.
func (s *Store) warnShared(spaceKey, pageID, one, other, hint string) {
	if other < one {
		one, other = other, one
	}

	key := sharedPage{space: spaceKey, page: pageID, one: one, other: other}
	if s.sharedWarned[key] {
		return
	}
	if s.sharedWarned == nil {
		s.sharedWarned = map[sharedPage]bool{}
	}
	s.sharedWarned[key] = true

	for _, claimant := range []string{one, other} {
		s.warn(claimant, report.WarningSharedPage,
			"%s and %s both publish to page %s in space %q; "+
				"renaming either will move a page the other also claims%s",
			one, other, pageID, spaceKey, hint,
		)
	}
}

// sharedPage is a page two paths both publish to, the paths in sorted order.
type sharedPage struct {
	space, page, one, other string
}

// normaliseKeys rewrites entries stored under a key that predates
// normalisation, so a mapping written by an earlier mark keeps working instead
// of being silently abandoned.
//
// Without this an upgrade strands every entry: the lookup normalises, misses,
// and the page is re-recorded under the new key, leaving the old one behind
// with no pattern to report it and no fingerprint to match it -- dead weight
// that arrives once per upgrade and never leaves.
//
// A normalised key can belong to a different shard than the one it was read
// from, since the shard is derived from the key, so this cannot be done in
// place while reading.
func (state *spaceState) normaliseKeys() {
	type move struct {
		from, to string
		entry    Entry
	}

	var moves []move
	for i := range state.shards {
		for path, entry := range state.shards[i].pages {
			if key := Key(path); key != path {
				moves = append(moves, move{from: path, to: key, entry: entry})
			}
		}
	}

	for _, m := range moves {
		delete(state.shards[shardFor(m.from)].pages, m.from)
		state.shards[shardFor(m.from)].dirty = true

		// A normalised key already holding an entry means both forms were
		// recorded, which is exactly the split being repaired. The one that was
		// already normalised wins; it is the form everything writes now.
		target := &state.shards[shardFor(m.to)]
		if _, taken := target.pages[m.to]; !taken {
			target.pages[m.to] = m.entry
		}
		target.dirty = true
	}
}

// buildIndexes derives the reverse, fingerprint and title lookups from the
// loaded entries.
func (state *spaceState) buildIndexes() {
	for i := range state.shards {
		for path, entry := range state.shards[i].pages {
			state.byPage[entry.PageID] = path
			state.indexHash(path, entry.Hash)
			if entry.Title == "" {
				continue
			}
			if seen, ok := state.titles[entry.Title]; ok && seen != entry.PageID {
				state.titles[entry.Title] = "" // ambiguous
				continue
			}
			state.titles[entry.Title] = entry.PageID
		}
	}
}

// indexHash notes that path is now recorded with hash.
func (state *spaceState) indexHash(path, hash string) {
	paths := state.byHash[hash]
	if paths == nil {
		paths = map[string]bool{}
		state.byHash[hash] = paths
	}
	paths[path] = true
}

// resolveManifestPage finds the page the manifest was asked to live on: by id
// when it looks like one and something is there, by title in the space
// otherwise. A page that cannot be found is an error rather than a fallback,
// since the alternative is a manifest quietly kept somewhere else.
func (s *Store) resolveManifestPage(ctx context.Context, spaceKey string) (string, error) {
	var byIDErr error
	if isNumeric(s.manifestPage) {
		found, err := s.api.GetPageByID(ctx, s.manifestPage)
		if err == nil && found != nil {
			return found.ID, nil
		}
		byIDErr = err
	}

	found, err := s.api.FindPage(ctx, spaceKey, s.manifestPage, confluence.ContentPage)
	if err != nil {
		return "", fmt.Errorf("unable to find the manifest page %q in space %q: %w", s.manifestPage, spaceKey, err)
	}
	if found == nil {
		// A page that could not be read by its id is not one known to be
		// missing, and only a missing one may run untracked.
		if byIDErr != nil && !errors.Is(byIDErr, confluence.ErrNotFound) {
			return "", fmt.Errorf("unable to find the manifest page %q: %w", s.manifestPage, byIDErr)
		}
		return "", &MissingPageError{Page: s.manifestPage, Space: spaceKey}
	}

	return found.ID, nil
}

// refuseSharedPage reports an error when the manifest page has already been
// resolved for another space in this run, which an id does whatever the space.
//
// One page holds one manifest under a prefix: every space on it reads the same
// properties as its own and writes them back. Two spaces in a run would each
// take the other's entries for theirs -- and report them as gone -- and then
// write the same shard twice, the second write against a version the first
// has already superseded. Refused when the second space is first touched,
// before anything is published to it, so the run says so instead.
func (s *Store) refuseSharedPage(spaceKey, contentID string) error {
	for _, other := range s.sortedSpaces() {
		if other == spaceKey || s.spaces[other].contentID != contentID {
			continue
		}
		return fmt.Errorf(
			"--manifest-page %q keeps the manifest of space %q, and this run also publishes to space %q: "+
				"one page holds one space's manifest, and two would overwrite each other's. "+
				"Name the page by title so each space finds a page of its own, "+
				"or publish each space in a run of its own with its own --manifest-prefix",
			s.manifestPage, other, spaceKey,
		)
	}

	return nil
}

// pageManifest resolves the manifest page of spaceKey, and returns the page's
// id and the backend that reaches a manifest kept on it.
func (s *Store) pageManifest(ctx context.Context, spaceKey string) (string, backend, error) {
	contentID, err := s.resolveManifestPage(ctx, spaceKey)
	if err != nil {
		return "", 0, err
	}

	if err := s.refuseSharedPage(spaceKey, contentID); err != nil {
		return "", 0, err
	}

	cloud, err := s.api.Cloud(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("unable to tell where the manifest of space %q is kept: %w", spaceKey, err)
	}

	if cloud {
		return contentID, pageProperties, nil
	}
	return contentID, contentProperties, nil
}

// attachLatePage looks once more, at Save, for the manifest page of a space
// that ran untracked, and keeps the manifest there when the run has published
// it. What the run recorded is what a tracked run would have recorded: it
// started from an empty manifest, and there was none. It reports whether the
// space now has somewhere to save to.
func (s *Store) attachLatePage(ctx context.Context, spaceKey string, state *spaceState) (bool, error) {
	if state.lateChecked {
		return false, nil
	}

	contentID, backend, err := s.pageManifest(ctx, spaceKey)
	var missing *MissingPageError
	if errors.As(err, &missing) {
		state.lateChecked = true
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// A manifest already there was kept by another run while this one ran
	// untracked; one built from an empty start is not written over it, nor
	// merged shard by shard into it.
	where := &spaceState{contentID: contentID, backend: backend}
	properties, err := where.list(ctx, s.api)
	if err != nil {
		return false, err
	}
	for _, property := range properties {
		if isManifestKey(s.prefix, property.Key) {
			log.Warn().Msgf(
				"manifest page %q of space %q already holds a page mapping this run did not read; "+
					"the mapping of this run is not saved",
				s.manifestPage, spaceKey,
			)
			state.lateChecked = true
			return false, nil
		}
	}

	state.contentID, state.backend = contentID, backend
	state.untracked = false

	log.Info().Msgf("manifest page %q now exists in space %q; the page mapping is saved there", s.manifestPage, spaceKey)

	return true, nil
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return s != ""
}

// list fetches every property from whichever backend holds this space's
// manifest. One request covers all shards.
func (state *spaceState) list(ctx context.Context, api *confluence.API) ([]confluence.Property, error) {
	switch state.backend {
	case spaceProperties:
		return api.ListSpaceProperties(ctx, state.spaceID)
	case pageProperties:
		return api.ListPageProperties(ctx, state.contentID)
	default:
		return api.ListContentProperties(ctx, state.contentID)
	}
}

// write stores one shard in whichever backend holds this space's manifest.
func (state *spaceState) write(ctx context.Context, api *confluence.API, index int, value []byte) error {
	return state.writeProperty(ctx, api, PropertyKeyFor(state.prefix, index), value, state.shards[index].property)
}

// writeProperty stores one property in whichever backend holds this space's
// manifest.
func (state *spaceState) writeProperty(
	ctx context.Context,
	api *confluence.API, key string, value []byte, existing *confluence.Property,
) error {
	switch state.backend {
	case spaceProperties:
		return api.SetSpaceProperty(ctx, state.spaceID, key, value, existing)
	case pageProperties:
		return api.SetPageProperty(ctx, state.contentID, key, value, existing)
	default:
		return api.SetContentProperty(ctx, state.contentID, key, value, existing)
	}
}

// writeMapping stores one of the mappings that covers the whole space -- the
// folders or the parents -- and reports whether it was written.
//
// A conflict is warned about rather than failed on: another run wrote between
// this run's read and its write, and losing the mapping is not worth failing a
// publish that has already succeeded.
func (state *spaceState) writeMapping(
	ctx context.Context,
	api *confluence.API,
	spaceKey, key string,
	value []byte,
	existing *confluence.Property,
	what string,
) (bool, error) {
	if err := state.writeProperty(ctx, api, key, value, existing); err != nil {
		if errors.Is(err, confluence.ErrPropertyUnseen) {
			log.Warn().Err(err).Msgf(
				"%s for space %q was not saved", what, spaceKey,
			)
			return false, nil
		}
		if errors.Is(err, confluence.ErrPropertyConflict) {
			log.Warn().Msgf(
				"%s for space %q was updated by a concurrent run; it was not saved",
				what, spaceKey,
			)
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// load returns the state for a space, reading it from Confluence on first use.
func (s *Store) load(ctx context.Context, spaceKey string) (*spaceState, error) {
	if state, ok := s.spaces[spaceKey]; ok {
		return state, nil
	}

	state := &spaceState{
		prefix:  s.prefix,
		byPage:  map[string]string{},
		titles:  map[string]string{},
		folders: map[string]string{},
		parents: map[string]string{},
		seen:    map[string]bool{},
		claimed: map[string]bool{},
		added:   map[string]bool{},
		byHash:  map[string]map[string]bool{},
	}
	for i := range state.shards {
		state.shards[i].pages = map[string]Entry{}
	}

	cloud, err := s.api.Cloud(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to tell where the manifest of space %q is kept: %w", spaceKey, err)
	}

	switch {
	case s.manifestPage != "":
		state.contentID, state.backend, err = s.pageManifest(ctx, spaceKey)
		var missing *MissingPageError
		if s.manifestPageOptional && errors.As(err, &missing) {
			log.Warn().Msgf(
				"manifest page %q is not a page in space %q: publishing there untracked; "+
					"the page mapping is saved only if this run publishes that page",
				s.manifestPage, spaceKey,
			)
			state.untracked = true
			s.spaces[spaceKey] = state
			return state, nil
		}
		if err != nil {
			return nil, err
		}

	case cloud:
		state.backend = spaceProperties
		state.spaceID, err = s.api.GetSpaceID(ctx, spaceKey)
		if err != nil {
			return nil, fmt.Errorf("unable to resolve space %q: %w", spaceKey, err)
		}

	default:
		state.backend = contentProperties
		homepage, err := s.api.FindHomePage(ctx, spaceKey)
		if err != nil {
			return nil, fmt.Errorf("unable to resolve home page of space %q: %w", spaceKey, err)
		}
		state.contentID = homepage.ID
	}

	properties, err := state.list(ctx, s.api)
	if err != nil {
		return nil, err
	}

	for i := range properties {
		if properties[i].Key == FolderPropertyKeyFor(s.prefix) {
			state.folderProperty = &properties[i]
			var doc folderDocument
			if err := json.Unmarshal(properties[i].Value, &doc); err != nil {
				log.Warn().Err(err).Msgf(
					"ignoring unreadable %s property of space %q; folders are no longer tracked",
					FolderPropertyKeyFor(s.prefix), spaceKey,
				)
			} else if doc.Version <= formatVersion && doc.Folders != nil {
				state.folders = doc.Folders
			}
			continue
		}

		if properties[i].Key == ParentPropertyKeyFor(s.prefix) {
			state.parentProperty = &properties[i]
			var doc parentDocument
			if err := json.Unmarshal(properties[i].Value, &doc); err != nil {
				log.Warn().Err(err).Msgf(
					"ignoring unreadable %s property of space %q; parents are no longer tracked",
					ParentPropertyKeyFor(s.prefix), spaceKey,
				)
			} else if doc.Version <= formatVersion && doc.Parents != nil {
				state.parents = doc.Parents
			}
			continue
		}

		index, ok := shardIndex(s.prefix, properties[i].Key)
		if !ok {
			// Some other property of the same space or page. Not ours.
			continue
		}
		state.shards[index].property = &properties[i]

		var doc document
		if err := json.Unmarshal(properties[i].Value, &doc); err != nil {
			// A shard that cannot be parsed is treated as empty. Failing the run
			// would let a corrupted property block publishing, where carrying on
			// merely costs the rename protection for the paths it held until the
			// next write repairs it.
			log.Warn().Err(err).Msgf(
				"ignoring unreadable %s property of space %q; those pages are no longer tracked",
				properties[i].Key, spaceKey,
			)
			continue
		}
		if doc.Version > formatVersion {
			log.Warn().Msgf(
				"%s property of space %q was written by a newer mark (format %d); ignoring it",
				properties[i].Key, spaceKey, doc.Version,
			)
			continue
		}
		if doc.Pages != nil {
			state.shards[index].pages = doc.Pages
		}
	}

	state.normaliseKeys()
	state.buildIndexes()

	s.spaces[spaceKey] = state
	return state, nil
}

// Lookup returns the entry recorded for a source path, and whether one exists.
//
// Calling it marks the path as seen, so a path that is looked up but turns out
// to have no entry still counts as present for the purposes of Orphans.
func (s *Store) Lookup(ctx context.Context, spaceKey, path string) (Entry, bool, error) {
	return s.lookup(ctx, spaceKey, path, true)
}

// lookup is Lookup, marking path seen only when asked to.
func (s *Store) lookup(ctx context.Context, spaceKey, path string, see bool) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = Key(path)

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return Entry{}, false, err
	}

	if see {
		state.seen[path] = true
	}
	entry, ok := state.shards[shardFor(path)].pages[path]
	return entry, ok, nil
}

// Record notes which page a source path published to, under which title and
// with what source fingerprint.
func (s *Store) Record(ctx context.Context, spaceKey, path, pageID, title, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = Key(path)

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return err
	}

	state.seen[path] = true

	// Two paths claiming one page means one of them will lose: a rename of
	// either retitles a page the other still believes is its own. mark cannot
	// tell which was intended, so it says so and records the latest -- silently
	// keeping one would make the next surprise harder to explain.
	moved, movedOK := s.movedOwner(state, path, pageID)
	if owner, ok := state.byPage[pageID]; ok && owner != path {
		if movedOK {
			// The path that owned this page is absent from this run -- covered
			// by it, yet not among the files it publishes and not seen by it --
			// and this path now publishes to the same page: the document moved
			// (a plain rename of the file, title unchanged), and the page
			// follows it. Left recorded, the old path would read as an orphan
			// whose page is the very one just published -- and --on-orphan
			// delete would trash it.
			log.Info().Msgf("%s moved to %s; page %s follows it", owner, path, pageID)
			delete(state.shards[shardFor(owner)].pages, owner)
			state.shards[shardFor(owner)].dirty = true
		} else {
			// A partial run follows a move only from a path it names, so the
			// usual cause here is a moved file whose old path was left out. An
			// entry with no pattern is never covered, so naming it would not help.
			hint := ""
			ownerEntry := state.shards[shardFor(owner)].pages[owner]
			if s.partial && s.absent(state, owner) && ownerEntry.Glob != "" &&
				!s.covers(owner, ownerEntry) && s.sameScope(path, ownerEntry) &&
				s.scopeKeys[ownerEntry.Glob] == nil {
				if IsDocumentScope(ownerEntry.Glob) {
					hint = fmt.Sprintf(
						"; if %s moved to %s, name every key of its scope that still exists to follow the move",
						owner, path,
					)
				} else {
					hint = fmt.Sprintf(
						"; if %s moved to %s, name both paths in --files or --files-from to follow the move",
						owner, path,
					)
				}
			}

			s.warnShared(spaceKey, pageID, owner, path, hint)
		}
	}

	// The same path recorded in another space means the document moved between
	// spaces. The page left behind is not deleted and not linked to the new one.
	for otherKey, other := range s.spaces {
		if otherKey == spaceKey {
			continue
		}
		if entry, ok := other.shards[shardFor(path)].pages[path]; ok {
			s.warn(path, report.WarningPlacement,
				"it previously published to space %q as page %s and now publishes to %q; "+
					"the page in %q is left behind",
				otherKey, entry.PageID, spaceKey, otherKey,
			)
		}
	}

	state.claimed[pageID] = true

	// Set before the early return below, not after it: a path whose entry is
	// already exactly right can still have just taken the page over from a
	// moved owner, and leaving the reverse lookup naming the deleted path
	// makes the next Record of this page warn about a document that is gone.
	state.byPage[pageID] = path

	sh := &state.shards[shardFor(path)]
	existing, ok := sh.pages[path]
	// A document is recorded under its run's scope, partial or not: the
	// scope names the documents' owner, not a pattern some of them lie in.
	glob, document := s.documentScopes[path]
	if !document {
		glob = s.recordGlob(path)
	}
	if s.partial && !document {
		switch {
		case ok && existing.Glob != "" && !IsDocumentScope(existing.Glob):
			glob = existing.Glob
		case movedOK && matches(moved.Glob, path):
			// Only a pattern the new path still lies in: a file moved into
			// another job's pattern is that job's now, and the run owning the
			// old pattern would read it as deleted.
			glob = moved.Glob
		case recordsUnder(state, s.runGlob):
			// The pattern of a run publishing all of it, given to a partial
			// run on a checkout holding only some of its files: that run
			// covers the entry, and later partial runs given the pattern
			// leave it to that run rather than read it as deleted. The glob
			// stays the one a full run records.
		default:
			// Not a pattern any run publishes all of: the entry would be
			// covered by no run but one given that very pattern.
			glob = globpattern.Escape(path)
		}
	}
	if carried := ok && existing.Glob == glob || movedOK && moved.Glob == glob; !carried {
		state.added[path] = true
	}
	if ok && existing.PageID == pageID && existing.Title == title &&
		existing.Hash == hash && existing.Glob == glob {
		return nil
	}

	entry := Entry{PageID: pageID, Title: title, Hash: hash, Glob: glob}

	// The version is carried across rather than rebuilt away. It is written by
	// RecordVersion alone, and this rebuilds the entry from its arguments --
	// none of which is the version -- so recording anything about a page
	// silently forgot which version of it mark had written.
	//
	// What that cost is --no-overwrite: the run that finds a page edited in
	// Confluence records the document as seen, so it is not mistaken for an
	// orphan, and by doing so erased the very baseline it had just reported
	// against. The next run read a zero, concluded nothing had drifted, and
	// overwrote the edit it had refused to touch the run before.
	//
	// Zeroed only when the page id changes, where a version belonging to a
	// different page would mean nothing.
	//
	// A document that moved has no entry of its own to carry from, so the
	// version comes from the path it moved from, whose entry was dropped
	// above. Starting it at zero would hand the next run the same blank
	// baseline, and --no-overwrite would wave through an edit it exists to
	// keep.
	switch {
	case ok && existing.PageID == pageID:
		entry.Version = existing.Version
	case movedOK:
		entry.Version = moved.Version
	}

	sh.pages[path] = entry
	sh.dirty = true
	state.indexHash(path, hash)
	return nil
}

// covers reports whether a recorded path is one this run was looking for, so
// that its absence from the run means the file is gone.
//
// A full run looks for what its pattern published last time, and for a path
// it names exactly or its pattern matches, what was recorded under that path
// alone. A partial run
// looks at the paths it names and where its own pattern points, whichever run
// recorded them, but for those left to the owning run (see leftToOwner). An
// entry with no recorded pattern predates patterns, and nothing can say what
// it was in scope of.
func (s *Store) covers(path string, entry Entry) bool {
	if entry.Glob == "" {
		return false
	}

	if IsDocumentScope(entry.Glob) {
		return s.coversDocument(path, entry)
	}

	if !s.partial {
		// An entry recorded under its own path is one no full run has
		// published yet, and this run's pattern, if it matches it, is the
		// one to notice the file deleted.
		return entry.Glob == s.runGlob ||
			entry.Glob == globpattern.Escape(path) && (s.named[path] || matches(s.runGlob, path))
	}

	return s.named[path] || matches(s.runGlob, path) && !s.leftToOwner(path, entry)
}

// leftToOwner reports whether a partial run leaves a tracked path to the run
// publishing all of its pattern: one recorded under the very pattern the
// partial run was given, which holds glob syntax, and not named exactly. That
// pattern is no narrower than the owning run's, and on a checkout holding only
// some of its files reads every other one as gone.
func (s *Store) leftToOwner(path string, entry Entry) bool {
	return s.partial && !s.named[path] && entry.Glob == s.runGlob && globpattern.HasMeta(s.runGlob)
}

// matches reports whether a recorded path lies in a pattern; both are spelled
// as Key spells them. An empty or malformed pattern matches nothing.
func matches(pattern, path string) bool {
	if pattern == "" {
		return false
	}

	matched, err := doublestar.Match(pattern, path)
	return err == nil && matched
}

// movedOwner returns the entry of the path a page belongs to in the manifest,
// when that path is absent from this run and path is not it: one the run
// covers, yet neither among the files the run publishes nor seen by it. A page
// found for a document under another path whose owner is gone like that is the
// same document, moved.
//
// An owner the run publishes, or one it does not cover, is not gone -- it
// is a second document claiming the page, which is a conflict to report and
// not a move to follow.
func (s *Store) movedOwner(state *spaceState, path, pageID string) (Entry, bool) {
	owner, ok := state.byPage[pageID]
	if !ok || owner == path {
		return Entry{}, false
	}

	entry, recorded := state.shards[shardFor(owner)].pages[owner]
	if !recorded || entry.PageID != pageID || !s.gone(state, path, owner, entry) {
		return Entry{}, false
	}

	return entry, true
}

// gone reports whether candidate's entry is one path could have moved or been
// renamed from: in path's scope, covered by the run and absent from it.
// Called with s.mu held.
func (s *Store) gone(state *spaceState, path, candidate string, entry Entry) bool {
	return s.sameScope(path, entry) && s.covers(candidate, entry) && s.absent(state, candidate)
}

// recordsUnder reports whether any entry in the space was recorded under
// pattern.
func recordsUnder(state *spaceState, pattern string) bool {
	for i := range state.shards {
		for _, entry := range state.shards[i].pages {
			if entry.Glob == pattern {
				return true
			}
		}
	}

	return false
}

// absent reports whether a recorded path is neither among the files this run
// publishes nor seen by it.
func (s *Store) absent(state *spaceState, path string) bool {
	return !s.runFiles[path] && !state.seen[path]
}

// LookupMoved returns the entry a page was recorded under before the document
// publishing to it moved to path, for a path with no entry of its own.
//
// The run on which a document moves is the one run where its own path knows
// nothing, and Record has not yet carried the old entry across -- it cannot
// until the page has been dealt with. Whatever has to be answered from the
// manifest before then, --no-overwrite's version baseline above all, is in the
// entry of the path it moved from.
//
// Unlike Lookup it does not mark anything as seen: the path it answers for is
// marked by its own Lookup, and the one it reads from is, by definition, not
// part of this run.
func (s *Store) LookupMoved(ctx context.Context, spaceKey, path, pageID string) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return Entry{}, false, err
	}

	entry, ok := s.movedOwner(state, Key(path), pageID)
	return entry, ok, nil
}

// ResolveRenamed reports the page a document published to under a previous
// path, for a document whose own path is not recorded.
//
// A rename changes the key, so the entry cannot be found by where it was; the
// only thing connecting the two is what the document contains.
//
// Candidates are restricted to the paths this run covers (see covers) -- what
// its own --files pattern published, or for a partial run what that pattern
// names -- and to paths it is not publishing now. Both restrictions matter, and the
// second alone is not enough: several mark invocations commonly publish
// different folders into one space, and every path belonging to another
// invocation is missing from this one. Without the pattern check they all look
// like deleted files this document might be a rename of, so a new file that
// happens to share content with another invocation's document takes over its
// page -- retitling it and dropping its entry, which leaves that document with
// no page at all and publishing a duplicate on its next run.
//
// The cost is that a file moved from one pattern to another reads as a deletion
// in the first and a new page in the second. That is already how deletion
// reporting behaves, and it is the safe direction: a duplicate is a nuisance,
// where rebinding onto the wrong page overwrites a document nobody asked to
// change.
//
// Deliberately unforgiving beyond that. A match must be unique, and the page
// must not already be claimed by another document this run.
//
// It looks under the --files pattern alone; in a run with SetRunDocuments,
// use ResolveRenamedFor, which looks under each path's own scope.
func (s *Store) ResolveRenamed(ctx context.Context, spaceKey, hash string) (string, Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.resolveRenamed(ctx, spaceKey, "", hash)
}

// ResolveRenamedFor is ResolveRenamed for the document path names, looking
// only among the entries of its own scope: those the run covers under its
// --files pattern for a file, or under the scope SetRunDocuments gave a
// document. A document given no scope is never taken for a rename, since
// nothing says which other paths it could have been.
func (s *Store) ResolveRenamedFor(ctx context.Context, spaceKey, path, hash string) (string, Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.resolveRenamed(ctx, spaceKey, Key(path), hash)
}

// resolveRenamed is ResolveRenamed among the entries in the scope of path, a
// key; "" is a file's. Called with s.mu held.
func (s *Store) resolveRenamed(ctx context.Context, spaceKey, path, hash string) (string, Entry, bool, error) {
	// A document with no scope is never anything's rename.
	if scope, document := s.documentScopes[path]; hash == "" || document && scope == "" {
		return "", Entry{}, false, nil
	}

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return "", Entry{}, false, err
	}

	var (
		foundPath  string
		foundEntry Entry
		count      int
	)
	for candidate := range state.byHash[hash] {
		entry, ok := state.shards[shardFor(candidate)].pages[candidate]
		if !ok || entry.Hash != hash || !s.sameScope(path, entry) || !s.covers(candidate, entry) ||
			s.runFiles[candidate] || state.claimed[entry.PageID] {
			continue
		}
		foundPath, foundEntry = candidate, entry
		count++
	}

	if count != 1 {
		if count > 1 {
			log.Info().Msgf(
				"%d unpublished documents in space %q share this document's content; "+
					"not treating any of them as its previous name",
				count, spaceKey,
			)
		}
		return "", Entry{}, false, nil
	}

	return foundPath, foundEntry, true, nil
}

// RenameSources returns the entries a document published under path could
// have been renamed from, whatever its content: those of its own document
// scope that the run covers, that it neither publishes nor has seen, and whose
// page no document of the run has claimed. None for a file, or for a document
// with no scope. It is what a caller comparing documents by more than their
// fingerprint chooses among.
func (s *Store) RenameSources(ctx context.Context, spaceKey, path string) (map[string]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = Key(path)
	if scope := s.documentScopes[path]; scope == "" {
		return nil, nil
	}

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return nil, err
	}

	sources := map[string]Entry{}
	for i := range state.shards {
		for candidate, entry := range state.shards[i].pages {
			if isDocumentDirectory(candidate) {
				continue
			}
			if s.gone(state, path, candidate, entry) && !state.claimed[entry.PageID] {
				sources[candidate] = entry
			}
		}
	}

	return sources, nil
}

// Recorded is Lookup without marking path as seen, for asking about a
// document before its turn: seen counts what the run published.
func (s *Store) Recorded(ctx context.Context, spaceKey, path string) (Entry, bool, error) {
	return s.lookup(ctx, spaceKey, path, false)
}

// Forget drops a recorded path: one whose page no longer exists, or an orphan
// that has been dealt with. A document followed to its new name is not
// forgotten here; Record hands its old entry over, version included.
func (s *Store) Forget(ctx context.Context, spaceKey, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = Key(path)

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return err
	}

	sh := &state.shards[shardFor(path)]
	if _, ok := sh.pages[path]; !ok {
		return nil
	}

	delete(sh.pages, path)
	sh.dirty = true
	return nil
}

// ResolveStaleTitle reports the page that a title used to belong to, for a
// title that no longer resolves.
//
// mark names parent pages by title, so renaming a page strands every reference
// to its old name: the lookup finds nothing, and an empty page is created under
// the old title with the real one's children moved beneath it. The manifest
// knows the title each path was last published under, so a reference to a title
// mark itself published can be followed to the page that now carries it.
//
// Returns the page id and true only when exactly one path was published under
// the title. More than one is ambiguous and is left alone.
//
// The answer comes from what was read at the start of the run, not from what
// has been recorded during it. A document is resolved against the state its
// author was looking at, which is also the only state in which the old title
// still exists.
func (s *Store) ResolveStaleTitle(ctx context.Context, spaceKey, title string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if title == "" {
		return "", false, nil
	}

	// The error is returned rather than folded into "nothing known". A manifest
	// that cannot be read is not the same as a title that was never published,
	// and treating it as such would create a duplicate page because the network
	// hiccuped -- the exact confusion this package exists to avoid.
	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return "", false, err
	}

	pageID, ok := state.titles[title]
	return pageID, ok && pageID != "", nil
}

// RecordVersion notes the version a page is at once mark is done with it: the
// one mark has just written, or the one it found when there was nothing to
// write. It is what --no-overwrite compares against on the next run.
//
// Separate from Record because not every Record should move it: a page skipped
// for having been edited in Confluence is still recorded as seen, but keeps the
// version mark last wrote, so that every run reports the difference again. It
// only updates an existing entry, so it has to follow Record, not replace it.
func (s *Store) RecordVersion(ctx context.Context, spaceKey, path string, version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = Key(path)

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return err
	}

	sh := &state.shards[shardFor(path)]
	entry, ok := sh.pages[path]
	if !ok || entry.Version == version {
		return nil
	}

	entry.Version = version
	sh.pages[path] = entry
	sh.dirty = true

	return nil
}

// RecordFolder notes which Confluence folder a declared folder path resolved
// to. The path is the chain of titles from the document's headers, joined so
// that the same chain under a different ancestor is a different key.
func (s *Store) RecordFolder(ctx context.Context, spaceKey, folderPath, folderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return err
	}

	if existing, ok := state.folders[folderPath]; ok && existing == folderID {
		return nil
	}

	state.folders[folderPath] = folderID
	state.foldersDirty = true
	return nil
}

// LookupFolder returns the folder a declared folder path resolved to last time.
func (s *Store) LookupFolder(ctx context.Context, spaceKey, folderPath string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return "", false, err
	}

	folderID, ok := state.folders[folderPath]
	return folderID, ok, nil
}

// RecordParent notes which Confluence page a declared parent chain resolved to.
// The path is the chain of titles from the document's headers, joined so that
// the same title at a different depth is a different key.
func (s *Store) RecordParent(ctx context.Context, spaceKey, parentPath, pageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return err
	}

	if existing, ok := state.parents[parentPath]; ok && existing == pageID {
		return nil
	}

	state.parents[parentPath] = pageID
	state.parentsDirty = true
	return nil
}

// LookupParent returns the page a declared parent chain resolved to last time.
//
// A chain is never forgotten, including after the parent has been renamed and
// the new title recorded alongside it. The old key still names the same page,
// and a document that has not been updated to the new title is exactly the one
// this exists for.
func (s *Store) LookupParent(ctx context.Context, spaceKey, parentPath string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load(ctx, spaceKey)
	if err != nil {
		return "", false, err
	}

	pageID, ok := state.parents[parentPath]
	return pageID, ok, nil
}

// Orphan is a tracked page whose source file was not seen in the run, together
// with what is known about it.
type Orphan struct {
	Path  string
	Entry Entry
}

// OrphanEntries is Orphans with the page each path published to, for a caller
// that means to do something about them rather than only say so.
//
// Like Orphans it is not a pure query: it forgets the stale entries whose page
// another document published this run. See Orphans.
func (s *Store) OrphanEntries(spaceKey string) []Orphan {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.spaces[spaceKey]
	if !ok {
		return nil
	}

	var orphans []Orphan
	for _, path := range s.orphans(spaceKey) {
		orphans = append(orphans, Orphan{
			Path:  path,
			Entry: state.shards[shardFor(path)].pages[path],
		})
	}

	return orphans
}

// Published reports how many pages this run actually published in a space.
//
// Nothing published means nothing to compare against: a checkout that failed,
// or a pattern that matched no files, looks exactly like every document having
// been deleted at once, and acting on that reading would empty a space.
func (s *Store) Published(spaceKey string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.spaces[spaceKey]
	if !ok {
		return 0
	}

	return len(state.seen)
}

// Survivors is Published narrowed to the side of an entry recorded under
// glob, the guard against removing its pages when nothing says they are gone
// rather than unseen. A run whose documents publish to a space says nothing
// about its files' pages there, nor one scope's documents about another's.
//
// For any glob but a scope SetScopeKeys listed it counts the pages the run's
// files, or the scope's documents, published. For a listed scope, where the
// caller's list decides what is gone, it counts the keys of the scope tracked
// there before the run that are still there: published again, followed to a
// new key, or listed. None of them left means every key of the scope there is
// gone at once, which a failed render or a bad key list looks exactly like,
// whatever new keys the run published.
func (s *Store) Survivors(spaceKey, glob string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.spaces[spaceKey]
	if !ok {
		return 0
	}

	survivors := 0
	listed := s.scopeKeys[glob]
	if listed == nil {
		for path := range state.seen {
			if s.sameScope(path, Entry{Glob: glob}) {
				survivors++
			}
		}

		return survivors
	}

	for i := range state.shards {
		for path, entry := range state.shards[i].pages {
			// A directory is still there only because a document published
			// into it is, which says nothing of the keys tracked before.
			if isDocumentDirectory(path) {
				continue
			}
			if entry.Glob == glob && (state.seen[path] && !state.added[path] || listed[path]) {
				survivors++
			}
		}
	}

	return survivors
}

// Orphans returns the recorded paths this run was looking for and did not find.
//
// Only entries the run covers count: those published by the same --files
// pattern, or for a partial run those its pattern names. A run narrowed to
// one directory says nothing about any other, and reporting everything outside
// it as missing -- which an unscoped version does -- buries the handful of
// genuine deletions in a list of files that are perfectly present. Entries with
// no recorded pattern predate this and are never reported, because there is no
// way to know what they were in scope of.
//
// It is not a pure query. A recorded path that is gone from the run, and whose
// page another document published this run, is not returned but forgotten:
// its entry is dropped and its shard marked for writing. The change is in
// memory until Save, so a dry run, whose read-only store never writes, leaves
// the stored manifest as it was.
func (s *Store) Orphans(spaceKey string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.orphans(spaceKey)
}

// orphans is Orphans without the lock, for callers that already hold it. It
// forgets the same stale entries Orphans does.
func (s *Store) orphans(spaceKey string) []string {
	state, ok := s.spaces[spaceKey]
	// An untracked space has no earlier mapping for a page to be missing from.
	if !ok || state.untracked {
		return nil
	}

	var orphans, left []string
	for i := range state.shards {
		for path, entry := range state.shards[i].pages {
			if state.seen[path] {
				continue
			}
			if s.leftToOwner(path, entry) {
				left = append(left, path)
				continue
			}
			if !s.covers(path, entry) {
				continue
			}
			// A page another document published this run is that document's
			// now, whatever this stale entry says: never offer it for removal.
			//
			// Nor keep it. Only one path per page is in byPage, so a move only
			// ever drops the owner it names, and any other stale path for the
			// page would otherwise stay forever -- offered to ResolveRenamed by
			// its fingerprint, to ResolveStaleTitle by its title, and behind a
			// warning that two documents share a page one of which is gone.
			// Kept only for a path this run still publishes: unseen there means
			// it failed before it was recorded, not that it went away.
			if state.claimed[entry.PageID] {
				if !s.runFiles[path] {
					log.Info().Msgf(
						"%s is gone and its page %s is now published by %s; forgetting it",
						path, entry.PageID, state.byPage[entry.PageID],
					)
					delete(state.shards[i].pages, path)
					state.shards[i].dirty = true
				}
				continue
			}
			orphans = append(orphans, path)
		}
	}

	if len(left) > 0 {
		sort.Strings(left)
		log.Info().Msgf(
			"space %q: %d tracked page(s) recorded under %q, the pattern given to --partial, "+
				"are left to the run publishing them all, since on a partial checkout "+
				"their files' absence says nothing; name the paths a change touched with --files-from: %s",
			spaceKey, len(left), s.runGlob, strings.Join(left, ", "),
		)
	}

	sort.Strings(orphans)
	return orphans
}

// Spaces returns the space keys this store has loaded, sorted.
func (s *Store) Spaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sortedSpaces()
}

// sortedSpaces is Spaces without the lock, for callers that already hold it.
func (s *Store) sortedSpaces() []string {
	keys := make([]string, 0, len(s.spaces))
	for key := range s.spaces {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Save writes back every space whose manifest changed.
//
// A space that gained no new mapping is not written at all, so a repeat run over
// unchanged files leaves the property -- and its version number -- alone.
func (s *Store) Save(ctx context.Context) error {
	if s.readOnly {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Each property describes pages or parents the others say nothing about,
	// so one Confluence refuses keeps no other from being saved, nor is its
	// failure lost to a later one's. A write that got no answer about its
	// property ends the save: the next would wait as long for the same.
	var errs []error
	for _, spaceKey := range s.sortedSpaces() {
		state := s.spaces[spaceKey]

		if state.untracked {
			attached, err := s.attachLatePage(ctx, spaceKey, state)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !attached {
				continue
			}
		}

		if state.foldersDirty {
			written, err := s.saveMapping(
				ctx, spaceKey, state, FolderPropertyKeyFor(s.prefix),
				folderDocument{Version: formatVersion, Folders: state.folders},
				state.folderProperty, "folder mapping",
			)
			if err != nil {
				errs = append(errs, err)
				if !refused(err) {
					return errors.Join(errs...)
				}
			}
			state.foldersDirty = !written
		}

		if state.parentsDirty {
			written, err := s.saveMapping(
				ctx, spaceKey, state, ParentPropertyKeyFor(s.prefix),
				parentDocument{Version: formatVersion, Parents: state.parents},
				state.parentProperty, "parent mapping",
			)
			if err != nil {
				errs = append(errs, err)
				if !refused(err) {
					return errors.Join(errs...)
				}
			}
			state.parentsDirty = !written
		}

		for i := range state.shards {
			if !state.shards[i].dirty {
				continue
			}

			if err := s.saveShard(ctx, spaceKey, state, i); err != nil {
				errs = append(errs, err)
				if !refused(err) {
					return errors.Join(errs...)
				}
			}
		}
	}

	return errors.Join(errs...)
}

// refused reports whether err is Confluence's answer about the one property
// written, which says nothing of the next. Anything else -- no answer at all,
// or a login that is not accepted -- would meet every later write the same.
func refused(err error) bool {
	if errors.Is(err, confluence.ErrUnauthorized) {
		return false
	}

	var status *confluence.StatusError
	return errors.As(err, &status) ||
		errors.Is(err, confluence.ErrPropertyConflict) || errors.Is(err, confluence.ErrPropertyUnseen)
}

// saveMapping encodes and writes one of the mappings that covers the whole
// space, and reports whether it was written.
func (s *Store) saveMapping(
	ctx context.Context,
	spaceKey string,
	state *spaceState,
	key string,
	mapping any,
	existing *confluence.Property,
	what string,
) (bool, error) {
	value, err := json.Marshal(mapping)
	if err != nil {
		return false, fmt.Errorf("unable to encode %s for space %q: %w", what, spaceKey, err)
	}

	return state.writeMapping(ctx, s.api, spaceKey, key, value, existing, what)
}

// saveShard writes one shard. When another run wrote it since this run read
// it, the shard is read again and this run's changes are written over what is
// there, once.
func (s *Store) saveShard(ctx context.Context, spaceKey string, state *spaceState, index int) error {
	err := s.writeShard(ctx, spaceKey, state, index)
	if !errors.Is(err, confluence.ErrPropertyConflict) && !errors.Is(err, confluence.ErrPropertyUnseen) {
		return err
	}

	key := PropertyKeyFor(state.prefix, index)
	changed, mergeErr := s.mergeShard(ctx, state, index)
	if mergeErr != nil {
		return fmt.Errorf(
			"manifest shard %s of space %q was not saved: %w",
			key, spaceKey, errors.Join(err, mergeErr),
		)
	}

	if errors.Is(err, confluence.ErrPropertyUnseen) {
		// Not known to be another run's doing: the shard may have been there
		// all along, and its pages published as though untracked.
		log.Warn().Err(err).Msgf(
			"manifest shard %s of space %q was read again; saving this run's mappings over it",
			key, spaceKey,
		)
	} else {
		log.Info().Msgf(
			"manifest shard %s of space %q was saved by another run meanwhile; saving this run's mappings over it",
			key, spaceKey,
		)
	}

	if !changed {
		state.shards[index].dirty = false
		return nil
	}

	if err := s.writeShard(ctx, spaceKey, state, index); err != nil {
		return fmt.Errorf("manifest shard %s of space %q was not saved: %w", key, spaceKey, err)
	}

	return nil
}

// writeShard encodes one shard and writes it over the property it was read
// from.
func (s *Store) writeShard(ctx context.Context, spaceKey string, state *spaceState, index int) error {
	sh := &state.shards[index]

	value, err := json.Marshal(document{
		Version: formatVersion,
		Pages:   sh.pages,
	})
	if err != nil {
		return fmt.Errorf("unable to encode manifest for space %q: %w", spaceKey, err)
	}

	if len(value) > shardSizeWarning {
		// Said while there is still room to act. Confluence's own limit
		// is not knowable from here, so this is a heads-up rather than a
		// refusal, and the write is attempted regardless.
		log.Warn().Msgf(
			"manifest shard %s of space %q is %d bytes and holds %d pages; "+
				"it may be approaching the property size Confluence will accept",
			PropertyKeyFor(state.prefix, index), spaceKey, len(value), len(sh.pages),
		)
	}

	if err := state.write(ctx, s.api, index, value); err != nil {
		return err
	}

	sh.dirty = false
	return nil
}

// mergeShard reads one shard again and puts this run's changes over what it
// now holds: the entries this run recorded or changed, and the removal of
// those it dropped. An entry this run dropped that the other run has since
// recorded differently is kept, as that run's. So is one both runs recorded
// for a page the other run wrote last: see wroteLater.
//
// What this run changed is what differs from the property it read, so nothing
// has to be kept about each change as it is made. It reports whether the
// result differs from what the shard now holds, and so is worth writing.
func (s *Store) mergeShard(ctx context.Context, state *spaceState, index int) (bool, error) {
	properties, err := state.list(ctx, s.api)
	if err != nil {
		return false, err
	}

	var current *confluence.Property
	for i := range properties {
		if properties[i].Key == PropertyKeyFor(state.prefix, index) {
			current = &properties[i]
			break
		}
	}

	sh := &state.shards[index]
	read := shardPages(sh.property)
	merged := shardPages(current)

	for path, entry := range read {
		if _, kept := sh.pages[path]; !kept && merged[path] == entry {
			delete(merged, path)
		}
	}
	for path, entry := range sh.pages {
		before, ok := read[path]
		if ok && before == entry {
			continue
		}
		if theirs, saved := merged[path]; saved && (!ok || theirs != before) && wroteLater(theirs, entry) {
			continue
		}
		merged[path] = entry
	}

	for path, entry := range merged {
		if held, ok := sh.pages[path]; ok && held == entry {
			continue
		}
		if _, taken := state.byPage[entry.PageID]; !taken {
			state.byPage[entry.PageID] = path
		}
		state.indexHash(path, entry.Hash)
	}

	sh.property = current
	sh.pages = merged
	return !maps.Equal(merged, shardPages(current)), nil
}

// wroteLater reports whether theirs records a later write of the page ours
// records. A page's version only grows, so of two runs that both published a
// document the one with the higher version wrote last, and its entry is the
// one that describes the page: saving the lower version over it would have
// --no-overwrite read the page as edited in Confluence on every later run.
func wroteLater(theirs, ours Entry) bool {
	return theirs.PageID == ours.PageID && theirs.Version > ours.Version
}

// shardPages returns the entries a shard's property holds, as load reads
// them: none when the property is absent, unreadable or of a newer format.
func shardPages(property *confluence.Property) map[string]Entry {
	pages := map[string]Entry{}
	if property == nil {
		return pages
	}

	var doc document
	if err := json.Unmarshal(property.Value, &doc); err != nil || doc.Version > formatVersion || doc.Pages == nil {
		return pages
	}
	return doc.Pages
}
