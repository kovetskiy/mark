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
	taken map[string]similarRename
	// unclear holds the documents close to some gone key but taken for none,
	// with the keys they are close to.
	unclear map[string][]similar.Pair
}

// similarRename is a document taken for a gone key, with that key's page.
type similarRename struct {
	similar.Pair
	page *confluence.PageInfo
}

// unmatched is a document of the run with no page of its own to go to: none
// recorded under its key, none of its title, and no gone key of the same
// content.
type unmatched struct {
	key   string
	words []string
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
			"%s is not taken for a rename: its text is alike to %s, but not clearly closer than to the next best, "+
				"or clearly the closest of the run's documents to it; a new page is created",
			key, strings.Join(named, ", "),
		)

		return nil, nil
	}

	match, ok := pairing.taken[key]
	if !ok {
		return nil, nil
	}

	// Taken since the pairing was made, by a document found by its title.
	sources, err := r.tracker.RenameSources(ctx, space, key)
	if err != nil {
		return nil, fmt.Errorf("unable to check whether %q was renamed: %w", doc.file, err)
	}
	if entry, ok := sources[match.Old]; !ok || entry.PageID != match.page.ID {
		log.Info().Msgf("%s is no longer gone; %s is not taken for its rename", match.Old, key)

		return nil, nil
	}

	doc.warnings.Add(report.WarningRename,
		"%s is taken for %s renamed, and publishes to its page %s: their texts are %.2f alike",
		key, match.Old, match.page.ID, match.Score,
	)

	followed := *match.page
	followed.Body.Storage.Value = ""

	return &followed, nil
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

	documents, taken, err := s.unmatched(ctx, r, space)
	if err != nil || len(documents) == 0 {
		return none, err
	}

	gone, err := r.tracker.RenameSources(ctx, space, documents[0].key)
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

	pages := map[string]*confluence.PageInfo{}
	texts := map[string][]string{}
	for _, key := range keys {
		entry := gone[key]
		pg, err := r.api.GetPageByIDExpanded(ctx, entry.PageID, "ancestors,version,body.storage")
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

		pages[key] = pg
		texts[key] = similar.StorageWords(pg.Body.Storage.Value)
	}

	scores := map[string]map[string]float64{}
	for _, doc := range documents {
		scores[doc.key] = map[string]float64{}
		for key, text := range texts {
			scores[doc.key][key] = similar.Score(doc.words, text)
		}
	}

	pairs, unclear := similar.Match(scores, s.threshold, renameMargin)
	made := &similarPairing{taken: map[string]similarRename{}, unclear: unclear}
	for doc, pair := range pairs {
		made.taken[doc] = similarRename{Pair: pair, page: pages[pair.Old]}
	}

	return made, nil
}

// unmatched returns the run's documents publishing to space that neither
// their key, their title nor their exact content finds a page for, with their
// words. Taken holds what the others go to, which is no longer gone: the key
// of the same content, or the page of the title.
func (s *similarRenames) unmatched(ctx context.Context, r *publishRun, space string) ([]unmatched, map[string]bool, error) {
	var documents []unmatched
	taken := map[string]bool{}
	for _, in := range s.documents {
		source, markdown, _, err := in.read()
		if err != nil {
			continue
		}

		meta, markdown, err := documentMeta(r.config, in.file, markdown, r.hierarchy, nil)
		if err != nil || meta == nil || !meta.Publish() || meta.Space != space || meta.Title == "" {
			continue
		}

		key := manifest.Key(in.key)
		_, recorded, err := r.tracker.Recorded(ctx, space, key)
		if err != nil {
			return nil, nil, err
		}
		if recorded {
			continue
		}

		previous, _, same, err := r.tracker.ResolveRenamedFor(ctx, space, key, fingerprint.SHA1(string(source)))
		if err != nil {
			return nil, nil, err
		}
		if same {
			taken[previous] = true

			continue
		}

		// Memoised, and asked again at the document's own turn.
		titled, err := r.api.FindPage(ctx, space, meta.Title, confluence.ContentType(meta.Type))
		if err != nil {
			return nil, nil, fmt.Errorf("unable to look up page %q: %w", meta.Title, err)
		}
		if titled != nil {
			taken[titled.ID] = true

			continue
		}

		documents = append(documents, unmatched{key: key, words: similar.MarkdownWords(string(markdown))})
	}

	return documents, taken, nil
}
