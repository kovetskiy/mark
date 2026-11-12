// Following a document renamed and edited at once: Tracking.RenameSimilarity.

package mark

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/internal/similar"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/metadata"
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

	// spaces holds each space's pairing once made.
	spaces map[string]*similarPairing
}

func newSimilarRenames(threshold float64) *similarRenames {
	return &similarRenames{threshold: threshold, spaces: map[string]*similarPairing{}}
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
	key  string
	text similar.Text
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
	// One pairing per space, however its documents spell the key.
	paired := strings.ToLower(space)
	pairing, ok := r.similar.spaces[paired]
	if !ok {
		pairing, err = r.pairSimilar(ctx, space, key)
		if err != nil {
			return nil, fmt.Errorf("unable to compare %q with the documents gone from its scope: %w", doc.file, err)
		}
		r.similar.spaces[paired] = pairing
	}

	if near, ok := pairing.unclear[key]; ok {
		named := make([]string, 0, len(near))
		for _, pair := range near {
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

	notTaken := func(why string) (*confluence.PageInfo, error) {
		doc.warnings.Add(report.WarningRename,
			"%s is %.2f alike to %s but not taken for its rename: %s; a new page is created",
			key, match.Score, match.Old, why,
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

	pg, inSpace, err := recordedPage(ctx, r.api, space, entry.PageID)
	if errors.Is(err, confluence.ErrNotFound) {
		return notTaken(fmt.Sprintf("its page %s no longer exists", entry.PageID))
	}
	if err != nil {
		return nil, fmt.Errorf("unable to load page %s recorded for %q: %w", entry.PageID, match.Old, err)
	}
	if !inSpace {
		return notTaken(fmt.Sprintf("its page %s is in %s, not space %q", pg.ID, pageSpace(pg), space))
	}
	if r.noOverwrite && pg.Version.Number != entry.Version {
		return notTaken(fmt.Sprintf("its page %s was edited in Confluence since it was compared", pg.ID))
	}

	log.Info().Msgf("%s is %.2f alike to %s, published as page %s; treating it as a rename", key, match.Score, match.Old, pg.ID)
	doc.warnings.Add(report.WarningRename,
		"%s is taken for %s renamed, and publishes to its page %s: their texts are %.2f alike",
		key, match.Old, pg.ID, match.Score,
	)

	return pg, nil
}

// pairSimilar pairs the space's unmatched documents with the gone keys of
// their scope, for the document of key asking first. It reads the run's
// documents only when some key is gone, and the gone keys' pages only when
// some document is unmatched too.
func (r *publishRun) pairSimilar(ctx context.Context, space, key string) (*similarPairing, error) {
	none := &similarPairing{}

	gone, err := r.tracker.RenameSources(ctx, space, key)
	if err != nil || len(gone) == 0 {
		return none, err
	}

	documents, taken, err := r.runTexts(ctx, space)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(documents, func(doc runText) bool { return doc.unmatched }) {
		return none, nil
	}

	keys := make([]string, 0, len(gone))
	for key, entry := range gone {
		if !taken[entry.PageID] {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)

	texts := map[string]similar.Text{}
	for _, key := range keys {
		entry := gone[key]
		pg, err := r.api.GetPageByIDExpanded(ctx, entry.PageID, "version,body.storage,space")
		if errors.Is(err, confluence.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("unable to read page %s recorded for %q: %w", entry.PageID, key, err)
		}

		// A page elsewhere is not this manifest's to publish over, and
		// compared, it could take a match from a gone key of this space.
		inSpace, known, err := pageInSpace(ctx, r.api, pg, space)
		if err != nil {
			return nil, err
		}
		if !known {
			// As recordedPage: passed over, the renamed document would be
			// published a second time.
			return nil, fmt.Errorf("unable to tell which space page %s, recorded for %q, is in", pg.ID, key)
		}
		if !inSpace {
			log.Info().Msgf("%s is not compared for a rename: its page %s is in %s, not space %q",
				key, pg.ID, pageSpace(pg), space)

			continue
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

		texts[key] = similar.Shingles(similar.StorageWords(pg.Body.Storage.Value))
	}

	scores := map[string]map[string]float64{}
	textOf := map[string]similar.Text{}
	for _, doc := range documents {
		textOf[doc.key] = doc.text
		if !doc.unmatched {
			continue
		}

		scores[doc.key] = map[string]float64{}
		for key, text := range texts {
			scores[doc.key][key] = doc.text.Similarity(text)
		}
	}

	rival := func(key string) float64 {
		best := 0.0
		for _, other := range documents {
			if other.key != key {
				best = max(best, textOf[key].Similarity(other.text))
			}
		}

		return best
	}

	pairs, unclear := similar.Match(scores, rival, r.similar.threshold, renameMargin)

	return &similarPairing{taken: pairs, unclear: unclear}, nil
}

// runTexts returns the texts of the run's documents, saying which of those
// publishing to space find no page of their own, and the pages the others
// there find, which are no longer any gone key's to give. Each document is
// read once a run, whichever space asks.
func (r *publishRun) runTexts(ctx context.Context, space string) ([]runText, map[string]bool, error) {
	var documents []runText
	taken := map[string]bool{}
	for _, in := range r.inputs.documents {
		ahead := r.titles.readAhead(in)
		meta := ahead.meta
		if meta == nil || !meta.Publish() || meta.Title == "" {
			continue
		}

		doc := runText{key: manifest.Key(in.key), text: r.titles.textOf(in)}
		if meta.Space == space {
			pageID, found, err := r.ownPage(ctx, doc.key, ahead.sourceHash, meta)
			if err != nil {
				return nil, nil, err
			}
			if found {
				taken[pageID] = true
			}
			doc.unmatched = !found
		}
		documents = append(documents, doc)
	}

	return documents, taken, nil
}

// ownPage is the page the document of key finds at its turn before any
// similarity is asked: recorded under its key, of the same content as a gone
// key, or of its title.
func (r *publishRun) ownPage(ctx context.Context, key, sourceHash string, meta *metadata.Meta) (string, bool, error) {
	entry, recorded, err := r.tracker.Recorded(ctx, meta.Space, key)
	if err != nil || recorded {
		return entry.PageID, recorded, err
	}

	_, entry, same, err := r.tracker.ResolveRenamedFor(ctx, meta.Space, key, sourceHash)
	if err != nil || same {
		return entry.PageID, same, err
	}

	// Memoised, and asked again at the document's own turn.
	titled, err := r.api.FindPage(ctx, meta.Space, meta.Title, confluence.ContentType(meta.Type))
	if err != nil {
		return "", false, fmt.Errorf("unable to look up page %q: %w", meta.Title, err)
	}
	if titled != nil {
		return titled.ID, true, nil
	}

	return "", false, nil
}
