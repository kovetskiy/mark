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
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kovetskiy/mark/v16/confluence"
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
// stored under. Each shard is this plus a dot and its index.
const PropertyKeyPrefix = "mark.manifest"

// FolderPropertyKey holds the folder mapping.
//
// Kept apart from the page shards rather than mixed in: folders are keyed by
// the titles declared in a header rather than by a source path, they are few
// enough that splitting them buys nothing, and a folder key appearing among the
// page keys would be reported as a source file that had gone missing.
const FolderPropertyKey = PropertyKeyPrefix + ".folders"

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

// PropertyKey returns the property key for a shard index.
func PropertyKey(shard int) string {
	return fmt.Sprintf("%s.%d", PropertyKeyPrefix, shard)
}

// shardIndex recovers a shard number from a property key, reporting false for
// any key that is not one of ours -- a space or page may hold properties put
// there by something else entirely.
func shardIndex(key string) (int, bool) {
	suffix, ok := strings.CutPrefix(key, PropertyKeyPrefix+".")
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(suffix)
	if err != nil || index < 0 || index >= ShardCount {
		return 0, false
	}
	return index, true
}

// ShardFor reports which shard holds a path. Exported so a caller -- in
// practice a test -- can find the property a given path's mapping lives in
// without duplicating the hash.
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
}

// folderDocument is the folder mapping as stored.
type folderDocument struct {
	Version int               `json:"version"`
	Folders map[string]string `json:"folders"`
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

	// spaces holds the manifest for each space key touched so far, alongside
	// the property it was read from -- the version in that property is what a
	// write has to supersede.
	spaces map[string]*spaceState
}

type spaceState struct {
	// backend is where this space's manifest lives. cloud reads and writes
	// space properties addressed by spaceID; otherwise they are content
	// properties on the homepage addressed by contentID.
	cloud     bool
	spaceID   string
	contentID string

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
	return &Store{api: api, spaces: map[string]*spaceState{}, runFiles: map[string]bool{}}
}

// NewReadOnlyStore returns a Store that answers every question the writing one
// does and never persists anything.
func NewReadOnlyStore(api *confluence.API) *Store {
	store := NewStore(api)
	store.readOnly = true
	return store
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
	for _, path := range paths {
		s.runFiles[Key(path)] = true
	}
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

// buildIndexes derives the reverse and title lookups from the loaded entries.
func (state *spaceState) buildIndexes() {
	for i := range state.shards {
		for path, entry := range state.shards[i].pages {
			state.byPage[entry.PageID] = path
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

// list fetches every property from whichever backend holds this space's
// manifest. One request covers all shards.
func (state *spaceState) list(api *confluence.API) ([]confluence.Property, error) {
	if state.cloud {
		return api.ListSpaceProperties(state.spaceID)
	}
	return api.ListContentProperties(state.contentID)
}

// write stores one shard in whichever backend holds this space's manifest.
func (state *spaceState) write(api *confluence.API, index int, value []byte) error {
	return state.writeProperty(api, PropertyKey(index), value, state.shards[index].property)
}

// writeProperty stores one property in whichever backend holds this space's
// manifest.
func (state *spaceState) writeProperty(
	api *confluence.API, key string, value []byte, existing *confluence.Property,
) error {
	if state.cloud {
		return api.SetSpaceProperty(state.spaceID, key, value, existing)
	}
	return api.SetContentProperty(state.contentID, key, value, existing)
}

// load returns the state for a space, reading it from Confluence on first use.
func (s *Store) load(spaceKey string) (*spaceState, error) {
	if state, ok := s.spaces[spaceKey]; ok {
		return state, nil
	}

	state := &spaceState{
		cloud:   s.api.IsCloud(),
		byPage:  map[string]string{},
		titles:  map[string]string{},
		folders: map[string]string{},
		seen:    map[string]bool{},
		claimed: map[string]bool{},
	}
	for i := range state.shards {
		state.shards[i].pages = map[string]Entry{}
	}

	var err error
	if state.cloud {
		state.spaceID, err = s.api.GetSpaceID(spaceKey)
		if err != nil {
			return nil, fmt.Errorf("unable to resolve space %q: %w", spaceKey, err)
		}
	} else {
		homepage, err := s.api.FindHomePage(spaceKey)
		if err != nil {
			return nil, fmt.Errorf("unable to resolve home page of space %q: %w", spaceKey, err)
		}
		state.contentID = homepage.ID
	}

	properties, err := state.list(s.api)
	if err != nil {
		return nil, err
	}

	for i := range properties {
		if properties[i].Key == FolderPropertyKey {
			state.folderProperty = &properties[i]
			var doc folderDocument
			if err := json.Unmarshal(properties[i].Value, &doc); err != nil {
				log.Warn().Err(err).Msgf(
					"ignoring unreadable %s property of space %q; folders are no longer tracked",
					FolderPropertyKey, spaceKey,
				)
			} else if doc.Version <= formatVersion && doc.Folders != nil {
				state.folders = doc.Folders
			}
			continue
		}

		index, ok := shardIndex(properties[i].Key)
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
func (s *Store) Lookup(spaceKey, path string) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = Key(path)

	state, err := s.load(spaceKey)
	if err != nil {
		return Entry{}, false, err
	}

	state.seen[path] = true
	entry, ok := state.shards[shardFor(path)].pages[path]
	return entry, ok, nil
}

// Record notes which page a source path published to, under which title and
// with what source fingerprint.
func (s *Store) Record(spaceKey, path, pageID, title, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = Key(path)

	state, err := s.load(spaceKey)
	if err != nil {
		return err
	}

	state.seen[path] = true

	// Two paths claiming one page means one of them will lose: a rename of
	// either retitles a page the other still believes is its own. mark cannot
	// tell which was intended, so it says so and records the latest -- silently
	// keeping one would make the next surprise harder to explain.
	if owner, ok := state.byPage[pageID]; ok && owner != path {
		log.Warn().Msgf(
			"%s and %s both publish to page %s in space %q; "+
				"renaming either will move a page the other also claims",
			owner, path, pageID, spaceKey,
		)
	}

	// The same path recorded in another space means the document moved between
	// spaces. The page left behind is not deleted and not linked to the new one.
	for otherKey, other := range s.spaces {
		if otherKey == spaceKey {
			continue
		}
		if entry, ok := other.shards[shardFor(path)].pages[path]; ok {
			log.Warn().Msgf(
				"%s previously published to space %q as page %s and now publishes to %q; "+
					"the page in %q is left behind",
				path, otherKey, entry.PageID, spaceKey, otherKey,
			)
		}
	}

	state.claimed[pageID] = true

	sh := &state.shards[shardFor(path)]
	existing, ok := sh.pages[path]
	if ok && existing.PageID == pageID && existing.Title == title &&
		existing.Hash == hash && existing.Glob == s.runGlob {
		return nil
	}

	sh.pages[path] = Entry{PageID: pageID, Title: title, Hash: hash, Glob: s.runGlob}
	state.byPage[pageID] = path
	sh.dirty = true
	return nil
}

// ResolveRenamed reports the page a document published to under a previous
// path, for a document whose own path is not recorded.
//
// A rename changes the key, so the entry cannot be found by where it was; the
// only thing connecting the two is what the document contains.
//
// Candidates are restricted to what this run's own --files pattern published,
// and to paths it is not publishing now. Both restrictions matter, and the
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
func (s *Store) ResolveRenamed(spaceKey, hash string) (string, Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if hash == "" {
		return "", Entry{}, false, nil
	}

	state, err := s.load(spaceKey)
	if err != nil {
		return "", Entry{}, false, err
	}

	var (
		foundPath  string
		foundEntry Entry
		count      int
	)
	for i := range state.shards {
		for path, entry := range state.shards[i].pages {
			if entry.Hash != hash || entry.Glob != s.runGlob ||
				s.runFiles[path] || state.claimed[entry.PageID] {
				continue
			}
			foundPath, foundEntry = path, entry
			count++
		}
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

// Forget drops a recorded path, used when a document has been followed to its
// new name and the old entry would otherwise linger as a deleted file forever.
func (s *Store) Forget(spaceKey, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path = Key(path)

	state, err := s.load(spaceKey)
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
func (s *Store) ResolveStaleTitle(spaceKey, title string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if title == "" {
		return "", false, nil
	}

	// The error is returned rather than folded into "nothing known". A manifest
	// that cannot be read is not the same as a title that was never published,
	// and treating it as such would create a duplicate page because the network
	// hiccuped -- the exact confusion this package exists to avoid.
	state, err := s.load(spaceKey)
	if err != nil {
		return "", false, err
	}

	pageID, ok := state.titles[title]
	return pageID, ok && pageID != "", nil
}

// RecordFolder notes which Confluence folder a declared folder path resolved
// to. The path is the chain of titles from the document's headers, joined so
// that the same chain under a different ancestor is a different key.
func (s *Store) RecordFolder(spaceKey, folderPath, folderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load(spaceKey)
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
func (s *Store) LookupFolder(spaceKey, folderPath string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load(spaceKey)
	if err != nil {
		return "", false, err
	}

	folderID, ok := state.folders[folderPath]
	return folderID, ok, nil
}

// Orphans returns the recorded paths this run was looking for and did not find.
//
// Only entries published by the same --files pattern count. A run narrowed to
// one directory says nothing about any other, and reporting everything outside
// it as missing -- which an unscoped version does -- buries the handful of
// genuine deletions in a list of files that are perfectly present. Entries with
// no recorded pattern predate this and are never reported, because there is no
// way to know what they were in scope of.
func (s *Store) Orphans(spaceKey string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.orphans(spaceKey)
}

// orphans is Orphans without the lock, for callers that already hold it.
func (s *Store) orphans(spaceKey string) []string {
	state, ok := s.spaces[spaceKey]
	if !ok {
		return nil
	}

	var orphans []string
	for i := range state.shards {
		for path, entry := range state.shards[i].pages {
			if state.seen[path] || entry.Glob == "" || entry.Glob != s.runGlob {
				continue
			}
			orphans = append(orphans, path)
		}
	}
	sort.Strings(orphans)
	return orphans
}

// PruneOrphans drops the entries Orphans reports and returns what it dropped.
//
// Without this the mapping only ever grows, and a file deleted once is reported
// as missing on every run thereafter -- which trains people to ignore the one
// message that matters. Reporting a deletion once and then forgetting it is the
// honest bookkeeping: mark never deleted the page and does not claim to know
// what became of it.
//
// Only what orphans would report is dropped, so the same scoping applies: a run
// that was not looking where a file used to be cannot forget it.
func (s *Store) PruneOrphans(spaceKey string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	pruned := s.orphans(spaceKey)
	state := s.spaces[spaceKey]
	for _, path := range pruned {
		sh := &state.shards[shardFor(path)]
		delete(sh.pages, path)
		sh.dirty = true
	}
	return pruned
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
func (s *Store) Save() error {
	if s.readOnly {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, spaceKey := range s.sortedSpaces() {
		state := s.spaces[spaceKey]

		if state.foldersDirty {
			value, err := json.Marshal(folderDocument{
				Version: formatVersion,
				Folders: state.folders,
			})
			if err != nil {
				return fmt.Errorf("unable to encode folder mapping for space %q: %w", spaceKey, err)
			}

			err = state.writeProperty(s.api, FolderPropertyKey, value, state.folderProperty)
			if err != nil {
				if errors.Is(err, confluence.ErrPropertyConflict) {
					log.Warn().Msgf(
						"folder mapping for space %q was updated by a concurrent run; it was not saved",
						spaceKey,
					)
				} else {
					return err
				}
			} else {
				state.foldersDirty = false
			}
		}

		for i := range state.shards {
			if !state.shards[i].dirty {
				continue
			}

			value, err := json.Marshal(document{
				Version: formatVersion,
				Pages:   state.shards[i].pages,
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
					PropertyKey(i), spaceKey, len(value), len(state.shards[i].pages),
				)
			}

			if err := state.write(s.api, i, value); err != nil {
				if errors.Is(err, confluence.ErrPropertyConflict) {
					// Another run wrote between this run's read and its write.
					// The two are not mergeable from here without re-reading,
					// and losing the mapping is not worth failing a publish that
					// has already succeeded -- the pages are live either way.
					log.Warn().Msgf(
						"manifest shard %s of space %q was updated by a concurrent run; "+
							"those mappings were not saved",
						PropertyKey(i), spaceKey,
					)
					continue
				}
				return err
			}

			state.shards[i].dirty = false
		}
	}

	return nil
}
