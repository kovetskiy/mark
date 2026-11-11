// Following a document renamed and edited at once: Tracking.RenameSimilarity.

package mark

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/fingerprint"
	"github.com/kovetskiy/mark/v17/internal/similar"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
)

// renameMargin is how far a pair has to lead the next best of either side to
// be taken: a close call is as likely to be the wrong page as the right one.
const renameMargin = 0.1

// similarRenames pairs a run's documents with gone keys of their scope by how
// alike their texts are. The pairing has to be one-to-one, so it is made for a
// whole space at once, when its first document asks, rather than one document
// at a time.
type similarRenames struct {
	threshold float64
	documents []input

	// spaces holds each space's pairing once made.
	spaces map[string]*similarPairing
}

// similarPairing is what a space's documents are taken for, by manifest key.
type similarPairing struct {
	taken map[string]similar.Pair
	// unclear holds the documents close to some gone key but taken for none,
	// with the keys they are close to.
	unclear map[string][]similar.Pair
}

// runText is a document of the run as the pairing sees it.
type runText struct {
	key   string
	words []string
	// unmatched says no page of its own is found for it: none recorded
	// under its key, none of its title, and no gone key of the same content.
	unmatched bool
}

// resolveRenamed returns the page a document published to under another key:
// a gone key with the same content, or with RenameSimilarity, a gone key
// whose page's text is clearly the closest to its own.
func (r *publishRun) resolveRenamed(ctx context.Context, doc *document) (*confluence.PageInfo, error) {
	pg, err := resolveRenamedFile(ctx, r.tracker, r.api, doc)
	if pg != nil || err != nil || r.similar == nil || !doc.document || doc.meta == nil {
		return pg, err
	}

	space, key := doc.meta.Space, manifest.Key(doc.key)
	pairing, err := r.similar.pairing(ctx, r, space)
	if err != nil {
		return nil, fmt.Errorf("unable to compare %q with the documents gone from its scope: %w", doc.file, err)
	}

	if close, ok := pairing.unclear[key]; ok {
		named := make([]string, 0, len(close))
		for _, pair := range close {
			named = append(named, fmt.Sprintf("%s (%.2f)", pair.Old, pair.Score))
		}
		doc.warnings.Add(report.WarningRename,
			"%s is not taken for a rename: its text is alike to %s, but not clearly closer than to the next best "+
				"or to another of the run's documents, or not clearly the closest of the run's documents to it; "+
				"a new page is created",
			key, strings.Join(named, ", "),
		)

		return nil, nil
	}

	match, ok := pairing.taken[key]
	if !ok {
		return nil, nil
	}

	notTaken := func(why string, args ...any) (*confluence.PageInfo, error) {
		doc.warnings.Add(report.WarningRename,
			"%s is %.2f alike to %s but not taken for its rename: %s; a new page is created",
			append([]any{key, match.Score, match.Old}, fmt.Sprintf(why, args...))...,
		)

		return nil, nil
	}

	// Read again at its turn: the pairing may have been made many documents
	// earlier, and the page is published over with what is read here.
	sources, err := r.tracker.RenameSources(ctx, space, key)
	if err != nil {
		return nil, fmt.Errorf("unable to check whether %q was renamed: %w", doc.file, err)
	}
	entry, ok := sources[match.Old]
	if !ok {
		return notTaken("another document of the run has taken it since")
	}

	pg, err = r.api.GetPageByID(ctx, entry.PageID)
	if errors.Is(err, confluence.ErrNotFound) {
		return notTaken("its page %s no longer exists", entry.PageID)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to load page %s recorded for %q: %w", entry.PageID, match.Old, err)
	}
	if r.noOverwrite && pg.Version.Number != entry.Version {
		return notTaken("its page %s was edited in Confluence since it was compared", pg.ID)
	}

	log.Info().Msgf("%s is %.2f alike to %s, published as page %s; treating it as a rename", key, match.Score, match.Old, pg.ID)
	doc.warnings.Add(report.WarningRename,
		"%s is taken for %s renamed, and publishes to its page %s: their texts are %.2f alike",
		key, match.Old, pg.ID, match.Score,
	)

	return pg, nil
}

// pairing is the space's pairing, made on first use.
func (s *similarRenames) pairing(ctx context.Context, r *publishRun, space string) (*similarPairing, error) {
	if made, ok := s.spaces[space]; ok {
		return made, nil
	}

	made, err := s.pair(ctx, r, space)
	if err != nil {
		return nil, err
	}
	if s.spaces == nil {
		s.spaces = map[string]*similarPairing{}
	}
	s.spaces[space] = made

	return made, nil
}

// pair compares the space's unmatched documents with the pages of the gone
// keys of their scope. Pages are read only when there are both.
func (s *similarRenames) pair(ctx context.Context, r *publishRun, space string) (*similarPairing, error) {
	none := &similarPairing{}

	documents, taken, err := s.texts(ctx, r, space)
	if err != nil {
		return nil, err
	}
	first := slices.IndexFunc(documents, func(doc runText) bool { return doc.unmatched })
	if first < 0 {
		return none, nil
	}

	gone, err := r.tracker.RenameSources(ctx, space, documents[first].key)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(gone))
	for key, entry := range gone {
		if !taken[key] && !taken[entry.PageID] {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return none, nil
	}
	slices.Sort(keys)

	texts := map[string][]string{}
	for _, key := range keys {
		entry := gone[key]
		pg, err := r.api.GetPageByIDExpanded(ctx, entry.PageID, "version,body.storage")
		if errors.Is(err, confluence.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("unable to read page %s recorded for %q: %w", entry.PageID, key, err)
		}

		// Taking the page would carry the old key's baseline across, and
		// the edit would then be overwritten as though it were mark's own.
		if r.noOverwrite && (entry.Version == 0 || pg.Version.Number != entry.Version) {
			log.Info().Msgf(
				"%s is not compared for a rename: its page %s was edited in Confluence since mark wrote it, "+
					"or has no version mark wrote, and --no-overwrite keeps it",
				key, pg.ID,
			)

			continue
		}

		texts[key] = similar.StorageWords(pg.Body.Storage.Value)
	}

	scores := map[string]map[string]float64{}
	rivals := map[string]float64{}
	for _, doc := range documents {
		if !doc.unmatched {
			continue
		}

		scores[doc.key] = map[string]float64{}
		for key, text := range texts {
			scores[doc.key][key] = similar.Score(doc.words, text)
		}
		for _, other := range documents {
			if other.key != doc.key {
				rivals[doc.key] = max(rivals[doc.key], similar.Score(doc.words, other.words))
			}
		}
	}

	pairs, unclear := similar.Match(scores, rivals, s.threshold, renameMargin)

	return &similarPairing{taken: pairs, unclear: unclear}, nil
}

// texts returns the words of the run's documents, saying which of those
// publishing to space no key, title or exact content finds a page for. Taken
// holds what the others there go to, which is no longer gone: the key of the
// same content, or the page of the title.
func (s *similarRenames) texts(ctx context.Context, r *publishRun, space string) ([]runText, map[string]bool, error) {
	var documents []runText
	taken := map[string]bool{}
	for _, in := range s.documents {
		source, markdown, _, err := in.read()
		if err != nil {
			continue
		}

		meta, markdown, err := documentMeta(r.config, in.file, markdown, r.hierarchy, nil)
		if err != nil || meta == nil || !meta.Publish() || meta.Title == "" {
			continue
		}

		doc := runText{key: manifest.Key(in.key), words: similar.MarkdownWords(string(markdown))}
		if meta.Space == space {
			doc.unmatched, err = s.unmatched(ctx, r, space, doc.key, source, meta.Title, meta.Type, taken)
			if err != nil {
				return nil, nil, err
			}
		}
		documents = append(documents, doc)
	}

	return documents, taken, nil
}

// unmatched reports whether the document of key finds no page of its own, and
// adds to taken what it finds otherwise.
func (s *similarRenames) unmatched(
	ctx context.Context, r *publishRun, space, key string, source []byte, title, kind string, taken map[string]bool,
) (bool, error) {
	_, recorded, err := r.tracker.Recorded(ctx, space, key)
	if err != nil || recorded {
		return false, err
	}

	previous, _, same, err := r.tracker.ResolveRenamedFor(ctx, space, key, fingerprint.SHA1(string(source)))
	if err != nil {
		return false, err
	}
	if same {
		taken[previous] = true

		return false, nil
	}

	// Memoised, and asked again at the document's own turn.
	titled, err := r.api.FindPage(ctx, space, title, confluence.ContentType(kind))
	if err != nil {
		return false, fmt.Errorf("unable to look up page %q: %w", title, err)
	}
	if titled != nil {
		taken[titled.ID] = true

		return false, nil
	}

	return true, nil
}
