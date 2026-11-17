package mark

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// titleScenario is a run of documents after a first run of others, drawn
// from a seed: titles kept, changed, changed in case or swapped, files
// renamed, deleted and added, and pages edited in Confluence.
type titleScenario struct {
	first, second map[string]string // file name -> title
	bodies        map[string]string // file name -> body, in the second run
	edited        []string          // files of the first run whose page is edited by hand
	trashed       string            // a file of the first run whose page is in the trash
	noOverwrite   bool
	steps         []string
}

func drawTitleScenario(seed int64) titleScenario {
	rng := rand.New(rand.NewSource(seed))
	titles := []string{"Alpha", "Beta", "Gamma", "Delta"}
	names := []string{"a.md", "b.md", "c.md", "d.md", "e.md", "f.md"}

	s := titleScenario{first: map[string]string{}, second: map[string]string{}, bodies: map[string]string{}}

	// The first run publishes documents of distinct titles.
	order := rng.Perm(len(titles))
	for i, name := range rng.Perm(len(names))[:2+rng.Intn(3)] {
		s.first[names[name]] = titles[order[i]]
	}

	used := map[string]bool{}
	for name := range s.first {
		used[name] = true
	}
	free := func() string {
		for _, i := range rng.Perm(len(names)) {
			if !used[names[i]] {
				used[names[i]] = true
				return names[i]
			}
		}
		return ""
	}

	firstNames := make([]string, 0, len(s.first))
	for name := range s.first {
		firstNames = append(firstNames, name)
	}
	sort.Strings(firstNames)

	for _, name := range firstNames {
		title, body := s.first[name], "Body of "+name+"."
		switch rng.Intn(6) {
		case 0: // kept
		case 1:
			title = titles[rng.Intn(len(titles))]
			s.steps = append(s.steps, fmt.Sprintf("%s retitled %q", name, title))
		case 2:
			title = strings.ToLower(title)
			s.steps = append(s.steps, fmt.Sprintf("%s retitled %q", name, title))
		case 3:
			s.steps = append(s.steps, name+" deleted")
			continue
		case 4:
			if to := free(); to != "" {
				s.steps = append(s.steps, name+" renamed "+to)
				name = to
			}
		case 5:
			if to := free(); to != "" {
				title = titles[rng.Intn(len(titles))]
				body = "Edited body of " + name + "."
				s.steps = append(s.steps, fmt.Sprintf("%s renamed %s, edited and retitled %q", name, to, title))
				name = to
			}
		}
		s.second[name], s.bodies[name] = title, body
	}

	for range rng.Intn(3) {
		if name := free(); name != "" {
			s.second[name], s.bodies[name] = titles[rng.Intn(len(titles))], "Body of new "+name+"."
			s.steps = append(s.steps, fmt.Sprintf("%s added as %q", name, s.second[name]))
		}
	}

	if rng.Intn(6) == 0 {
		s.trashed = firstNames[rng.Intn(len(firstNames))]
		s.steps = append(s.steps, "page of "+s.trashed+" trashed")
	}

	if len(s.second) == 0 {
		s.second["a.md"], s.bodies["a.md"] = titles[0], "Body of new a.md."
		s.steps = append(s.steps, "a.md written as "+titles[0])
	}

	if rng.Intn(3) == 0 {
		s.noOverwrite = true
		for _, name := range firstNames {
			if rng.Intn(2) == 0 {
				s.edited = append(s.edited, name)
			}
		}
		s.steps = append(s.steps, fmt.Sprintf("--no-overwrite, pages of %v edited", s.edited))
	}

	return s
}

// TestADryRunPredictsWhereTitlesChangingHandsGo: over generated runs, a dry
// run reports for each document what the real run then does: refused or not,
// and the page it publishes to, an existing one or a new one.
func TestADryRunPredictsWhereTitlesChangingHandsGo(t *testing.T) {
	for seed := int64(1); seed <= 300; seed++ {
		s := drawTitleScenario(seed)

		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Logf("first run %v; then %s", s.first, strings.Join(s.steps, "; "))

			server, _ := docsSpace(t)
			dir := t.TempDir()
			for name, title := range s.first {
				writeFile(t, dir, name, titledDoc(title, "Body of "+name+"."))
			}

			config := trackingConfig(server, filepath.Join(dir, "*.md"))
			config.ContinueOnError = true

			published, err := reportOf(t, config)
			require.NoError(t, err)

			existing, pageOf := map[string]bool{}, map[string]string{}
			for _, p := range published {
				existing[p.PageID] = true
				pageOf[filepath.Base(p.File)] = p.PageID
			}
			for _, name := range s.edited {
				server.EditPage(pageOf[name], "<p>Written by a person.</p>")
			}
			if s.trashed != "" {
				server.TrashPage(pageOf[s.trashed])
			}

			edited := map[string]bool{}
			for _, name := range s.edited {
				edited[pageOf[name]] = true
			}

			for name := range s.first {
				require.NoError(t, os.Remove(filepath.Join(dir, name)))
			}
			for name, title := range s.second {
				writeFile(t, dir, name, titledDoc(title, s.bodies[name]))
			}
			config.Tracking.NoOverwrite = s.noOverwrite
			// So that the dry run names the page it would write.
			config.Update.ChangesOnly = true

			config.Mode.DryRun = true
			previewed, _ := reportOf(t, config)
			config.Mode.DryRun = false
			done, _ := reportOf(t, config)

			require.Len(t, previewed, len(s.second))
			require.Len(t, done, len(s.second))

			for _, real := range done {
				name := filepath.Base(real.File)
				dry := statusOf(t, previewed, name)

				switch {
				case dry.Status == report.StatusSkipped && real.Status == report.StatusPublished &&
					edited[real.PageID] && real.PageID != pageOf[name]:
					// A page edited by hand that the record keeps for another
					// file is not held back by --no-overwrite when a document
					// publishes to it under its title, and the dry run says it
					// would be; that is apart from which document has a title.
					t.Logf("%s: page %s overwritten, previewed as left alone", name, real.PageID)
				case real.Status == report.StatusFailed && strings.Contains(real.Reason, "but is trashed"):
					// Only creating the page finds that the trash holds its title.
				case real.Status == report.StatusFailed:
					require.Equal(t, report.StatusFailed, dry.Status, "%s: %s", name, real.Reason)
					require.Equal(t, real.Reason, dry.Reason, name)
				case real.Status == report.StatusSkipped:
					require.Equal(t, report.StatusSkipped, dry.Status, "%s: %s", name, dry.Reason)
					require.Equal(t, real.PageID, dry.PageID, name)
				case !existing[real.PageID]:
					require.Equal(t, report.StatusWouldCreate, dry.Status, "%s: %s", name, dry.Reason)
				default:
					require.Contains(t, []report.Status{report.StatusWouldUpdate, report.StatusUnchanged}, dry.Status,
						"%s: %s", name, dry.Reason)
					require.Equal(t, real.PageID, dry.PageID, name)
				}
			}
		})
	}
}

// TestADryRunGivesATitleToTheDocumentPublishedAheadOfItsTurn: d.md takes the
// title f.md gives up, so f.md goes first and takes its new title before e.md,
// which declares it too, comes to it. The dry run refuses e.md, as the run
// does, not f.md.
func TestADryRunGivesATitleToTheDocumentPublishedAheadOfItsTurn(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "f.md", titledDoc("Gamma", "Body of f."))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	require.NoError(t, Run(context.Background(), config))

	writeFile(t, dir, "d.md", titledDoc("Gamma", "Body of d."))
	writeFile(t, dir, "e.md", titledDoc("Delta", "Body of e."))
	writeFile(t, dir, "f.md", titledDoc("Delta", "Body of f."))
	config.ContinueOnError = true

	for _, dryRun := range []bool{true, false} {
		config.Mode.DryRun = dryRun

		pages, err := reportOf(t, config)
		require.Error(t, err, "dry run %t", dryRun)

		require.Len(t, pages, 3, "dry run %t", dryRun)
		assert.NotEqual(t, report.StatusFailed, statusOf(t, pages, "d.md").Status, "dry run %t", dryRun)
		assert.NotEqual(t, report.StatusFailed, statusOf(t, pages, "f.md").Status, "dry run %t", dryRun)
		assert.Contains(t, statusOf(t, pages, "e.md").Reason, "f.md already publishes \"Delta\"", "dry run %t", dryRun)
	}
}
