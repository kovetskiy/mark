// Building the page hierarchy --parents-from-path derives from directories.

package mark

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v16/fingerprint"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/rs/zerolog/log"
	"go.yaml.in/yaml/v3"
)

// newHierarchy lays the run's files out under the root the flags name, or the
// one the --files pattern implies.
func newHierarchy(config Config, files []string) (*page.Hierarchy, error) {
	root := config.ParentsFromPathRoot
	if root == "" {
		root = page.GlobRoot(config.Files)
	}

	hierarchy := page.NewHierarchy(root, files, directoryTitleReader(config))

	// A root given by hand that covers none of the files is a mistake, and
	// quietly deriving no parents at all is the wrong way to find out. One it
	// covers only partly is said once per file, since the rest of the run
	// carries on.
	outside := hierarchy.Outside()
	if config.ParentsFromPathRoot != "" && len(files) > 0 && len(outside) == len(files) {
		return nil, fmt.Errorf(
			"--parents-from-path-root %q covers none of the files: it has to be a directory the --files pattern lies under",
			config.ParentsFromPathRoot,
		)
	}

	for _, file := range outside {
		log.Warn().Msgf("%s is outside --parents-from-path-root %q; its path says nothing about its parents", file, root)
	}

	return hierarchy, nil
}

// hashDirectoryTitles appends the generated hash to the titles of the pages
// standing for directories: the parents a document's path derives, outermost
// first, and the directory title its own document takes, if it is one.
//
// Each is hashed as AppendGeneratedHash would hash a document there: over the
// --parents prefix, the hashed directories above it, the space and its title.
// That is what a directory's own document comes to when it declares no parents
// of its own, so its title is the one its children look for. One that does
// declare them is still the directory's page, and is titled by its path rather
// than by where the header moves it, or its children would lose it.
func hashDirectoryTitles(prefix []string, space string, derived []string, title string) ([]string, string) {
	var chain []string
	// ExtractMeta ignores a --parents whose first entry is empty.
	if len(prefix) > 0 && prefix[0] != "" {
		chain = slices.Clone(prefix)
	}

	hashed := make([]string, 0, len(derived))
	for _, parent := range derived {
		parent = metadata.GeneratedHashTitle(chain, space, parent)
		hashed = append(hashed, parent)
		chain = append(chain, parent)
	}

	if title != "" {
		title = metadata.GeneratedHashTitle(chain, space, title)
	}

	return hashed, title
}

// directoryHash gives a directory entry a fingerprint of its own.
//
// A directory has no content to fingerprint, and an entry sharing a hash with a
// document could be mistaken for that document under a previous name.
func directoryHash(key string) string {
	return fingerprint.SHA1("mark:directory:" + key)
}

// directoryTitleReader works out what a directory's page should be called.
//
// A directory's own document says so first, in the same order publishing would
// read it: a Title header or front matter, then a leading heading when
// --title-from-h1 asked for that. A .pages file beside the documents says so
// for a directory that has no document of its own. Failing both, the directory
// is named after itself.
//
// The filename is deliberately not consulted. It is "README" in every directory
// that has one, which is a name for a file rather than for a page.
func directoryTitleReader(config Config) page.TitleResolver {
	return func(directory, indexFile string) (string, error) {
		if indexFile != "" {
			_, source, err := readSource(indexFile)
			if err != nil {
				return "", err
			}

			meta, _, err := metadata.ExtractMeta(
				source, "", config.TitleFromH1, false, indexFile, nil, false, "",
				slices.Contains(config.Features, "frontmatter"),
			)
			if err != nil {
				return "", fmt.Errorf("unable to read the title of %q: %w", indexFile, err)
			}

			if meta != nil && meta.Title != "" {
				return meta.Title, nil
			}
		}

		return directoryTitleFromPagesFile(directory)
	}
}

// directoryTitleFromPagesFile reads a .pages file, the convention MkDocs and
// others already use for naming a directory.
func directoryTitleFromPagesFile(directory string) (string, error) {
	source, err := os.ReadFile(filepath.Join(directory, ".pages"))
	if errors.Is(err, fs.ErrNotExist) {
		// Absence is the ordinary case.
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("unable to read %s: %w", filepath.Join(directory, ".pages"), err)
	}

	var pages struct {
		Title string `yaml:"title"`
	}
	if err := yaml.Unmarshal(source, &pages); err != nil {
		return "", fmt.Errorf("unable to parse %s: %w", filepath.Join(directory, ".pages"), err)
	}

	return strings.TrimSpace(pages.Title), nil
}
