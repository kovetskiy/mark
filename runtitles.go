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
	inputs    []input
	hierarchy *page.Hierarchy

	// documents maps a title's key to the documents declaring it; nil until read.
	documents map[string][]input

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
	r.documents = map[string][]input{}

	for _, in := range r.inputs {
		_, markdown, _, err := in.read()
		if err != nil {
			continue
		}

		meta, _, err := documentMeta(r.config, in.file, markdown, r.hierarchy, nil)
		if err != nil || meta == nil || !meta.Publish() || meta.Title == "" || meta.Space == "" {
			continue
		}

		key := runTitleKey(meta.Space, meta.Type, meta.Title)
		r.documents[key] = append(r.documents[key], in)
	}

	log.Debug().Msgf("indexed %d title(s) declared by the run's documents", len(r.documents))
}

// declaredBy returns the one document of the run that publishes content of
// that type under the title, and false when none does or more than one does.
func (r *runTitles) declaredBy(space, kind, title string) (input, bool) {
	documents := r.declaring(space, kind, title)
	if len(documents) != 1 {
		return input{}, false
	}

	return documents[0], true
}

// declaring returns the documents of the run that publish content of that type
// under the title.
func (r *runTitles) declaring(space, kind, title string) []input {
	if r == nil {
		return nil
	}
	if r.documents == nil {
		r.index()
	}

	return r.documents[runTitleKey(space, kind, title)]
}

// warnAmbiguous says, when more than one of the run's documents declare the
// page title a document's parent names and nothing else found that parent,
// that it is found by its title alone. That is all it can say: on a first
// publish no page may have the title yet, and the page the parent then becomes
// is the one the declarers go on to publish to. It is said against the
// document, in warnings, and once a run against each that declares it, since
// any of them is the one to retitle.
func (r *runTitles) warnAmbiguous(space, title string, warnings *report.Warnings) {
	documents := r.declaring(space, "page", title)
	if len(documents) < 2 {
		return
	}

	names := make([]string, 0, len(documents))
	for _, in := range documents {
		names = append(names, in.file)
	}
	declarers := strings.Join(names, ", ")
	warnings.Add(report.WarningPlacement,
		"parent %q is declared by %d documents (%s), so it is found by its title alone, "+
			"not through the pages --track-pages recorded for them",
		title, len(documents), declarers,
	)

	key := runTitleKey(space, "page", title)
	if r.warned[key] {
		return
	}
	if r.warned == nil {
		r.warned = map[string]bool{}
	}
	r.warned[key] = true

	for _, file := range names {
		var declarer *report.Warnings
		if r.warningsFor != nil {
			declarer = r.warningsFor(file)
		}
		if declarer == nil {
			declarer = report.NewWarnings(file)
		}

		declarer.Add(report.WarningPlacement,
			"parent %q is declared by %d documents (%s); a document under it follows none of them",
			title, len(documents), declarers,
		)
	}
}
