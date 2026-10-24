package mark

import (
	"slices"

	"github.com/kovetskiy/mark/v16/metadata"
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
type runTitles struct {
	files map[string][]string
}

func runTitleKey(space, title string) string {
	return space + "\x00" + title
}

// newRunTitles reads the title and space each of the run's files declares.
// Anything that cannot be read or has no title is left out: processing the file
// reports the problem properly, and a document missing here only means its
// title is not recognised as the run's own.
func newRunTitles(config Config, files []string, hierarchy *page.Hierarchy) *runTitles {
	index := &runTitles{files: map[string][]string{}}
	frontMatter := slices.Contains(config.Features, "frontmatter")

	for _, file := range files {
		_, markdown, err := readSource(file)
		if err != nil {
			continue
		}

		meta, _, err := metadata.ExtractMeta(
			markdown,
			config.Space,
			config.TitleFromH1,
			config.TitleFromFilename,
			file,
			config.Parents,
			false,
			config.ContentAppearance,
			frontMatter,
		)
		if err != nil || meta == nil || !meta.Publish() {
			continue
		}

		title := meta.Title
		if hierarchy != nil {
			derived, err := hierarchy.Title(file)
			if err != nil {
				continue
			}
			if derived != "" {
				title = derived
			}
		}
		if title == "" || meta.Space == "" {
			continue
		}

		key := runTitleKey(meta.Space, title)
		index.files[key] = append(index.files[key], file)
	}

	log.Debug().Msgf("indexed the titles of %d document(s) in the run", len(index.files))

	return index
}

// declaredBy returns the one file of the run that declares the title, and false
// when none does or when more than one does.
func (r *runTitles) declaredBy(space, title string) (string, bool) {
	if r == nil {
		return "", false
	}

	files := r.files[runTitleKey(space, title)]
	if len(files) != 1 {
		return "", false
	}

	return files[0], true
}
