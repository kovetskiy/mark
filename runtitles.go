package mark

import (
	"strings"

	"github.com/kovetskiy/mark/v16/page"
	"github.com/rs/zerolog/log"
)

// runTitles records which of the run's own documents declare which title.
//
// A parent is named by title, and a document that names a parent which has
// been retitled in the same run cannot find it: the page still carries the old
// title, and the document that will rename it may sort after the child. The
// run's documents know the answer, because the one declaring the missing title
// is the parent, and the manifest knows which page that document published.
//
// Read on the first parent that cannot be found, since a run where every parent
// is where it says it is never needs it.
type runTitles struct {
	config    Config
	files     []string
	hierarchy *page.Hierarchy

	// documents maps a title's key to the files declaring it; nil until read.
	documents map[string][]string
}

func runTitleKey(space, kind, title string) string {
	// Titles are matched without regard to case everywhere else (FindPage, the
	// claim on a title), so a child naming "parent renamed" still means
	// "Parent Renamed". The type is part of it as it is of FindPage: a blog
	// post of that title is not the page a child names.
	return space + "\x00" + kind + "\x00" + strings.ToLower(title)
}

// index reads the title, type and space each of the run's files publishes
// under. Anything that cannot be read or has no title is left out: processing
// the file reports the problem properly, and a document missing here only
// means its title is not recognised as the run's own.
func (r *runTitles) index() {
	r.documents = map[string][]string{}

	for _, file := range r.files {
		_, markdown, err := readSource(file)
		if err != nil {
			continue
		}

		meta, _, err := documentMeta(r.config, file, markdown, r.hierarchy)
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
	if r.documents == nil {
		r.index()
	}

	documents := r.documents[runTitleKey(space, kind, title)]
	if len(documents) != 1 {
		return "", false
	}

	return documents[0], true
}
