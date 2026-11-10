package mark

import (
	"strings"

	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
)

// runTitles records which of the run's own documents declare which title, so a
// parent retitled in this run can be found before its own turn. Read lazily:
// most runs never miss a parent.
type runTitles struct {
	config    PublishConfig
	files     []string
	hierarchy *page.Hierarchy

	// documents maps a title's key to the files declaring it; nil until read.
	documents map[string][]string

	// warningsFor gives a document's collector, for a title declared by more
	// than one; nil only logs. warned holds the titles already said so about.
	warningsFor func(file string) *report.Warnings
	warned      map[string]bool
}

func runTitleKey(space, kind, title string) string {
	// Case folds as in FindPage; the type is keyed because a blog post is not a parent.
	return space + "\x00" + kind + "\x00" + strings.ToLower(title)
}

// index leaves out any file that cannot be read; processing it reports why.
func (r *runTitles) index() {
	r.documents = map[string][]string{}

	for _, file := range r.files {
		_, markdown, _, err := readSource(file)
		if err != nil {
			continue
		}

		meta, _, err := documentMeta(r.config, file, markdown, r.hierarchy, nil)
		if err != nil || meta == nil || !meta.Publish() || meta.Title == "" || meta.Space == "" {
			continue
		}

		key := runTitleKey(meta.Space, meta.Type, meta.Title)
		r.documents[key] = append(r.documents[key], file)
	}

	log.Debug().Msgf("indexed %d title(s) declared by the run's documents", len(r.documents))
}

// declaredBy returns the one document of the run that publishes content of
// that type under the title, and false when none does or more than one does.
func (r *runTitles) declaredBy(space, kind, title string) (string, bool) {
	if r == nil {
		return "", false
	}
	if r.documents == nil {
		r.index()
	}

	key := runTitleKey(space, kind, title)
	documents := r.documents[key]
	if len(documents) > 1 && !r.warned[key] {
		if r.warned == nil {
			r.warned = map[string]bool{}
		}
		r.warned[key] = true

		// Said against each, since any of them is the one to retitle.
		for _, file := range documents {
			var warnings *report.Warnings
			if r.warningsFor != nil {
				warnings = r.warningsFor(file)
			}
			if warnings == nil {
				warnings = report.NewWarnings(file)
			}

			warnings.Add(report.WarningPlacement,
				"parent %q is declared by %d documents (%s); a document under it follows none of them",
				title, len(documents), strings.Join(documents, ", "),
			)
		}
	}
	if len(documents) != 1 {
		return "", false
	}

	return documents[0], true
}
