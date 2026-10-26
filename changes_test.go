package mark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dryRunDocBody(body string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n" + body + "\n"
}

// changesOnlyConfig publishes file to server under --changes-only.
func changesOnlyConfig(server *confluencetest.Server, file string) Config {
	return Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: &bytes.Buffer{},
		ChangesOnly: true,
	}
}

func TestDryRunChangesOnlyReportsPerPage(t *testing.T) {
	publish := func(t *testing.T) (*confluencetest.Server, string, Config) {
		server, _ := docsSpace(t)
		file := writeFile(t, t.TempDir(), "doc.md", dryRunDocBody("stable body"))
		config := changesOnlyConfig(server, file)
		require.NoError(t, Run(config))
		return server, file, config
	}
	edit := func(t *testing.T, file string) {
		require.NoError(t, os.WriteFile(file, []byte(dryRunDocBody("different body")), 0o600))
	}

	t.Run("unchanged page is reported and its HTML is not printed", func(t *testing.T) {
		_, _, config := publish(t)
		out := previewOf(t, config, "json")

		assert.Contains(t, out, `"status": "unchanged"`)
		assert.NotContains(t, out, "<p>", "an unchanged page must not be dumped")
	})

	t.Run("changed page is reported as would-update, its HTML only without a report", func(t *testing.T) {
		_, file, config := publish(t)
		edit(t, file)
		out := previewOf(t, config, "json")

		// stdout is the report alone, so a CI step can parse it.
		assert.Equal(t, report.StatusWouldUpdate, decodeReport(t, []byte(out)).Pages[0].Status)
		assert.NotContains(t, out, "different body")

		// The HTML is printed with the url format, which has no report to break.
		assert.Contains(t, previewOf(t, config, ""), "different body")
	})

	t.Run("github format is the annotations alone", func(t *testing.T) {
		_, file, config := publish(t)
		edit(t, file)
		out := previewOf(t, config, "github")

		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			assert.True(t, strings.HasPrefix(line, "::"), "not a workflow command: %q", line)
		}
		assert.Contains(t, out, "would update")
	})

	t.Run("writes nothing", func(t *testing.T) {
		server, file, config := publish(t)
		before := len(nonGET(server))
		edit(t, file)
		previewOf(t, config, "json")
		assert.Len(t, nonGET(server), before)
	})
}

func TestDryRunChangesOnlyNewPageWouldBeCreated(t *testing.T) {
	server, _ := docsSpace(t)
	file := writeFile(t, t.TempDir(), "doc.md", dryRunDocBody("fresh"))
	config := changesOnlyConfig(server, file)

	assert.Equal(t, report.StatusWouldCreate, decodeReport(t, []byte(previewOf(t, config, "json"))).Pages[0].Status)
	assert.Contains(t, previewOf(t, config, ""), "fresh")

	api := confluence.NewAPI(server.URL, "user", "token", false)
	pg, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	assert.Nil(t, pg, "a dry run must not create the page")
}

func TestDryRunChangesOnlyRetitleCountsAsChange(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	body := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: %s -->\n\nsame\n"
	file := writeFile(t, dir, "doc.md", fmt.Sprintf(body, "First"))

	config := trackingConfig(server, file)
	config.ChangesOnly = true
	require.NoError(t, Run(config))

	writeFile(t, dir, "doc.md", fmt.Sprintf(body, "Second"))
	out := previewOf(t, config, "json")

	assert.Contains(t, out, `"status": "would-update"`)
}

func TestDryRunAloneStillPrintsEveryPage(t *testing.T) {
	server, _ := docsSpace(t)
	file := writeFile(t, t.TempDir(), "doc.md", dryRunDocBody("stable body"))
	config := changesOnlyConfig(server, file)
	require.NoError(t, Run(config))

	config.ChangesOnly = false
	assert.Contains(t, previewOf(t, config, ""), "stable body")
}

func TestDryRunChangesOnlyKnowsAboutAttachmentLinks(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), png, 0o600))
	file := writeFile(t, dir, "doc.md", dryRunDocBody("<!-- Attachment: logo.png -->\n\n[the logo](logo.png)\n\n![logo](logo.png)"))

	config := changesOnlyConfig(server, file)
	require.NoError(t, Run(config))

	out := previewOf(t, config, "github")

	assert.NotContains(t, out, "would update")
	assert.NotContains(t, out, "logo.png", "an unchanged page must not be dumped")

	assert.Contains(t, previewOf(t, config, "json"), `"status": "unchanged"`)
}

func TestDryRunChangesOnlyChangedAttachment(t *testing.T) {
	// A changed file is re-uploaded by a real run whether or not the body
	// changes with it, so every page that would upload one would change.
	preview := func(t *testing.T, body string) string {
		server, _ := docsSpace(t)
		dir := t.TempDir()
		logo := filepath.Join(dir, "logo.png")
		require.NoError(t, os.WriteFile(logo, []byte("one"), 0o600))
		file := writeFile(t, dir, "doc.md", dryRunDocBody(body))

		config := changesOnlyConfig(server, file)
		require.NoError(t, Run(config))

		require.NoError(t, os.WriteFile(logo, []byte("two"), 0o600))
		out := previewOf(t, config, "github")

		return out
	}

	t.Run("linked", func(t *testing.T) {
		assert.Contains(t, preview(t, "<!-- Attachment: logo.png -->\n\n[the logo](logo.png)"), "would update")
	})

	// The body names the file and is unchanged, but a real run still uploads
	// the new bytes, so the page is one that would change.
	t.Run("embedded", func(t *testing.T) {
		assert.Contains(t, preview(t, "![logo](logo.png)"), "would update")
	})

	t.Run("declared and not linked", func(t *testing.T) {
		assert.Contains(t, preview(t, "<!-- Attachment: logo.png -->\n\nno link"), "would update")
	})
}

func TestDryRunChangesOnlyNoOverwriteReportsAnEditedPageSkipped(t *testing.T) {
	server, id, config := noOverwriteFixture(t)
	server.EditPage(id, "<p>Written by a person.</p>")

	config.ChangesOnly = true
	out := previewOf(t, config, "json")

	assert.Contains(t, out, `"status": "skipped"`)
	assert.NotContains(t, out, "<p>From mark.</p>", "a page left alone must not be dumped")
}

// TestDryRunChangesOnlySkippedPageIsNotCompiled keeps the order of a real run:
// a page that --no-overwrite leaves alone is reported as skipped even when its
// source could not be compiled.
func TestDryRunChangesOnlySkippedPageIsNotCompiled(t *testing.T) {
	server, id, config := noOverwriteFixture(t)
	server.EditPage(id, "<p>Written by a person.</p>")

	writeFile(t, filepath.Dir(config.Files), "doc.md",
		dryRunDocBody("<!-- Attachment: missing.png -->\n\nFrom mark."))

	config.ChangesOnly = true
	out := previewOf(t, config, "json")

	assert.Contains(t, out, `"status": "skipped"`)
}

// TestDryRunChangesOnlyLiteralPlaceholderTextIsNotAChange covers a document that
// happens to contain the text an earlier version used to mark pending links.
func TestDryRunChangesOnlyLiteralPlaceholderTextIsNotAChange(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", dryRunDocBody("see /mark-dry-run-pending/ in the docs"))

	config := changesOnlyConfig(server, file)
	require.NoError(t, Run(config))

	out := previewOf(t, config, "github")

	assert.NotContains(t, out, "would update")
}

// TestDryRunChangesOnlyStopsWhenTheRecordedPageCannotBeLoaded keeps a failed
// lookup from being reported as a page that would be created.
func TestDryRunChangesOnlyStopsWhenTheRecordedPageCannotBeLoaded(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	body := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: %s -->\n\nsame\n"
	file := writeFile(t, dir, "doc.md", fmt.Sprintf(body, "First"))

	config := trackingConfig(server, file)
	config.ChangesOnly = true
	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	first, err := api.FindPage("DOCS", "First", "page")
	require.NoError(t, err)
	require.NotNil(t, first)

	server.SetFail(func(r *http.Request) (int, string, bool) {
		return http.StatusInternalServerError, "boom",
			r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/"+first.ID)
	})

	writeFile(t, dir, "doc.md", fmt.Sprintf(body, "Second"))
	var out bytes.Buffer
	config.DryRun = true
	config.OutputFormat = "json"
	config.Output = &out

	err = Run(config)
	require.Error(t, err)
	assert.NotContains(t, out.String(), "would-create")
}

// nonGET lists the requests that could have written something.
func nonGET(server *confluencetest.Server) []string {
	var w []string
	for _, r := range server.Requests() {
		if r.Method != http.MethodGet {
			w = append(w, r.Method+" "+r.Path)
		}
	}
	return w
}

// TestDryRunChangesOnlyReportsAMove covers a page whose body is unchanged but
// whose Parent header is not: a real run moves it, so it would change.
func TestDryRunChangesOnlyReportsAMove(t *testing.T) {
	server, _ := docsSpace(t)
	other := server.AddPage("DOCS", "Other", "page", mustFind(t, server, "Home"))
	dir := t.TempDir()
	body := "<!-- Space: DOCS -->\n<!-- Parent: %s -->\n<!-- Title: Doc -->\n\nsame\n"
	file := writeFile(t, dir, "doc.md", fmt.Sprintf(body, "Parent"))

	config := changesOnlyConfig(server, file)
	require.NoError(t, Run(config))
	id := mustFind(t, server, "Doc")

	writeFile(t, dir, "doc.md", fmt.Sprintf(body, "Other"))
	before := len(nonGET(server))

	out := previewOf(t, config, "json")

	assert.Contains(t, out, `"status": "would-update"`)
	assert.Contains(t, out, "moved")
	assert.Len(t, nonGET(server), before, "a dry run must not write")
	assert.NotEqual(t, other.ID, server.Page(id).ParentID, "a dry run must not move the page")

	// And the real run does move it, which is what the report promised.
	config.DryRun = false
	config.OutputFormat = ""
	config.Output = &bytes.Buffer{}
	require.NoError(t, Run(config))
	assert.Equal(t, other.ID, server.Page(id).ParentID)
}

// TestDryRunChangesOnlyReportsAMoveUnderAParentToBeCreated covers a Parent
// header naming a page that does not exist yet. A dry run creates nothing, so
// the parent it resolves is the one the page already sits under, and the move
// a real run makes once it has created the new parent must still be counted.
func TestDryRunChangesOnlyReportsAMoveUnderAParentToBeCreated(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", dryRunDocBody("same"))

	config := changesOnlyConfig(server, file)
	require.NoError(t, Run(config))

	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Parent: Missing -->\n<!-- Title: Doc -->\n\nsame\n")
	before := len(nonGET(server))

	out := previewOf(t, config, "json")

	assert.Contains(t, out, `"status": "would-update"`)
	assert.Len(t, nonGET(server), before, "a dry run must not write")
}

// TestDryRunChangesOnlyReportsChangedLabels covers a page whose body is
// unchanged but whose labels are not what its headers declare: a real run
// brings them in line whatever the body comparison finds.
func TestDryRunChangesOnlyReportsChangedLabels(t *testing.T) {
	body := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n%s\nsame\n"

	preview := func(t *testing.T, edit func(*confluencetest.Server, string, string)) string {
		server, _ := docsSpace(t)
		dir := t.TempDir()
		file := writeFile(t, dir, "doc.md", fmt.Sprintf(body, "<!-- Label: kept -->\n"))

		config := changesOnlyConfig(server, file)
		require.NoError(t, Run(config))
		id := mustFind(t, server, "Doc")
		edit(server, id, dir)
		before := len(nonGET(server))

		out := previewOf(t, config, "json")
		assert.Len(t, nonGET(server), before, "a dry run must not write")

		return out
	}

	t.Run("label header changed", func(t *testing.T) {
		out := preview(t, func(_ *confluencetest.Server, _, dir string) {
			writeFile(t, dir, "doc.md", fmt.Sprintf(body, "<!-- Label: kept -->\n<!-- Label: added -->\n"))
		})
		assert.Contains(t, out, `"status": "would-update"`)
		assert.Contains(t, out, "labels")
	})

	t.Run("label added in Confluence is removed by a real run", func(t *testing.T) {
		out := preview(t, func(server *confluencetest.Server, id, _ string) {
			server.AddLabel(id, "stray")
		})
		assert.Contains(t, out, `"status": "would-update"`)
	})

	t.Run("labels as declared", func(t *testing.T) {
		out := preview(t, func(*confluencetest.Server, string, string) {})
		assert.Contains(t, out, `"status": "unchanged"`)
	})
}

// TestDryRunChangesOnlyReportsTheURL gives an existing page the address a
// real run reports for it, and a page to be created none.
func TestDryRunChangesOnlyReportsTheURL(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", dryRunDocBody("same"))
	writeFile(t, dir, "new.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: New -->\n\nnew\n")

	var first bytes.Buffer
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: &first,
		ChangesOnly: true, OutputFormat: "json",
	}
	require.NoError(t, Run(config))
	published := decodeReport(t, first.Bytes())
	require.Len(t, published.Pages, 1)
	require.NotEmpty(t, published.Pages[0].URL)

	config.Files = filepath.Join(dir, "*.md")
	previewed := decodeReport(t, []byte(previewOf(t, config, "json")))
	require.Len(t, previewed.Pages, 2)
	for _, pg := range previewed.Pages {
		switch filepath.Base(pg.File) {
		case "doc.md":
			assert.Equal(t, report.StatusUnchanged, pg.Status)
			assert.Equal(t, published.Pages[0].URL, pg.URL)
		case "new.md":
			assert.Equal(t, report.StatusWouldCreate, pg.Status)
			assert.Empty(t, pg.URL)
		}
	}
}

// decodeReport reads a JSON report, failing the test if the output is not one.
func decodeReport(t *testing.T, data []byte) *report.Report {
	t.Helper()
	r := &report.Report{}
	require.NoError(t, json.Unmarshal(data, r), "output is not a JSON report: %s", data)
	return r
}

// TestDryRunChangesOnlyFailsOnAttachmentsSharingAName keeps the preview from
// passing a document a real run refuses: two different files that flatten to
// one attachment name.
func TestDryRunChangesOnlyFailsOnAttachmentsSharingAName(t *testing.T) {
	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprintf("page exists %v", existing), func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			file := writeFile(t, dir, "doc.md", dryRunDocBody("plain"))

			config := changesOnlyConfig(server, file)
			if existing {
				require.NoError(t, Run(config))
			}

			require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o700))
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "a_b"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b_c.txt"), []byte("one"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "a_b", "c.txt"), []byte("two"), 0o600))
			writeFile(t, dir, "doc.md", dryRunDocBody(
				"<!-- Attachment: a/b_c.txt -->\n<!-- Attachment: a_b/c.txt -->\n\nplain"))

			config.DryRun = true
			config.OutputFormat = "json"
			config.Output = &bytes.Buffer{}
			err := Run(config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "would both be uploaded")
		})
	}
}

// A page that does not exist has no attachment preview to settle against; the
// dry run must still compile it, links to local files included.
func TestDryRunChangesOnlyNewPageWithAttachments(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), []byte("png"), 0o600))
	file := writeFile(t, dir, "doc.md", dryRunDocBody(
		"<!-- Attachment: logo.png -->\n\n![logo](logo.png)\n\n[the logo](logo.png)"))
	before := len(nonGET(server))

	out := previewOf(t, changesOnlyConfig(server, file), "json")

	assert.Contains(t, out, `"status": "would-create"`)
	assert.Len(t, nonGET(server), before, "a dry run must not write")
}

// With nothing to add and --append-labels removing nothing, a page's labels
// cannot matter, so neither a real run nor the preview reads them.
func TestLabelLookupIsSkippedWhenLabelsCannotChange(t *testing.T) {
	labelRequests := func(t *testing.T, dryRun, appendLabels bool) int {
		server, _ := docsSpace(t)
		file := writeFile(t, t.TempDir(), "doc.md", dryRunDocBody("same"))

		config := changesOnlyConfig(server, file)
		config.ChangesOnly = dryRun
		config.AppendLabels = appendLabels
		require.NoError(t, Run(config))

		before := len(server.Requests())
		if dryRun {
			previewOf(t, config, "json")
		} else {
			require.NoError(t, Run(config))
		}

		n := 0
		for _, r := range server.Requests()[before:] {
			if strings.HasSuffix(r.Path, "/label") {
				n++
			}
		}
		return n
	}

	for _, dryRun := range []bool{true, false} {
		t.Run(fmt.Sprintf("dry run %v", dryRun), func(t *testing.T) {
			assert.Zero(t, labelRequests(t, dryRun, true))
			assert.NotZero(t, labelRequests(t, dryRun, false), "without --append-labels a stray label is removed")
		})
	}
}

// TestDryRunChangesOnlyReportsAFolderMoveUnderAParentToBeCreated covers a
// document with a Folder and a Parent chain whose last page does not exist yet.
// A dry run stops the chain at the deepest page that exists, and the folder
// found under it is the one the page already sits in; a real run creates the
// missing page and a new folder beneath it, then moves the page there.
func TestDryRunChangesOnlyReportsAFolderMoveUnderAParentToBeCreated(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownInFolder("Manuals", "Doc"))

	config := changesOnlyConfig(server, file)
	require.NoError(t, Run(config))
	id := mustFind(t, server, "Doc")
	require.Len(t, server.Folders(), 1)
	oldFolder := server.Folders()[0].ID
	require.Equal(t, oldFolder, server.Page(id).ParentID)

	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Parent: Missing -->\n"+
			"<!-- Folder: Manuals -->\n<!-- Title: Doc -->\n\nBody.\n")
	before := len(nonGET(server))

	out := previewOf(t, config, "json")

	assert.Equal(t, report.StatusWouldUpdate, decodeReport(t, []byte(out)).Pages[0].Status)
	assert.Len(t, nonGET(server), before, "a dry run must not write")

	// And the real run does move it, which is what the report promised.
	config.DryRun = false
	config.OutputFormat = ""
	config.Output = &bytes.Buffer{}
	require.NoError(t, Run(config))
	assert.NotEqual(t, oldFolder, server.Page(id).ParentID, "the real run should have moved the page")
}

// TestDryRunChangesOnlyFollowsARenamedParent covers --track-pages with a parent
// renamed in Confluence. A real run rewrites the stale title before resolving
// ancestry and finds the page where it already is, so it moves nothing; the
// preview has to do the same, or it reports a move every run that never comes.
func TestDryRunChangesOnlyFollowsARenamedParent(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	guide := handMadeParent(t, server, "Guide")
	file := writeFile(t, dir, "child.md", markdownUnder("Guide", "Child"))

	config := trackingConfig(server, file)
	config.ChangesOnly = true
	require.NoError(t, Run(config))

	server.RenamePage(guide.ID, "Guide Renamed")
	id := mustFind(t, server, "Child")
	before := len(nonGET(server))

	out := previewOf(t, config, "json")

	assert.Equal(t, report.StatusUnchanged, decodeReport(t, []byte(out)).Pages[0].Status)
	assert.Len(t, nonGET(server), before, "a dry run must not write")

	// The real run agrees: it leaves the page where it is and does not write
	// it. It may still save the manifest, which is not the page.
	config.DryRun = false
	config.OutputFormat = ""
	config.Output = &bytes.Buffer{}
	require.NoError(t, Run(config))
	assert.Equal(t, guide.ID, server.Page(id).ParentID)
	for _, w := range nonGET(server)[before:] {
		assert.NotContains(t, w, "/content/", "the real run should not have written a page: %s", w)
	}
}

// TestChangesOnlySeesPagePropertyHeaders: --changes-only compared a hash of
// the body alone, so a document whose only change was its Emoji or
// Content-Appearance header was reported "already up to date" and the page
// kept the old one. Both are published with the body.
func TestChangesOnlySeesPagePropertyHeaders(t *testing.T) {
	const header = "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n"

	tests := []struct {
		name  string
		extra string
	}{
		{"emoji", "<!-- Emoji: 🚀 -->\n"},
		{"content appearance", "<!-- Content-Appearance: fixed -->\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			file := writeFile(t, dir, "doc.md", header+"\nbody\n")

			config := Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: file, Features: []string{"mention"}, Output: io.Discard,
				ChangesOnly: true,
			}
			api := confluence.NewAPI(server.URL, "user", "token", false)

			target, err := ProcessFile(file, api, config)
			require.NoError(t, err)
			published := server.Page(target.ID).Version

			// Unchanged: skipped, which is the point of the flag.
			_, err = ProcessFile(file, confluence.NewAPI(server.URL, "user", "token", false), config)
			require.NoError(t, err)
			require.Equal(t, published, server.Page(target.ID).Version)

			writeFile(t, dir, "doc.md", header+tt.extra+"\nbody\n")
			_, err = ProcessFile(file, confluence.NewAPI(server.URL, "user", "token", false), config)
			require.NoError(t, err)
			assert.Greater(t, server.Page(target.ID).Version, published,
				"a changed %s header must update the page", tt.name)

			// And having been published, it is up to date again.
			updated := server.Page(target.ID).Version
			_, err = ProcessFile(file, confluence.NewAPI(server.URL, "user", "token", false), config)
			require.NoError(t, err)
			assert.Equal(t, updated, server.Page(target.ID).Version)
		})
	}
}

// TestContentFingerprintOfAPlainPageIsTheBodyHash pins what keeps an upgrade
// quiet: a page with neither header -- or with the appearance every page gets
// by default -- is fingerprinted exactly as it was before the headers counted,
// so the fingerprints already stamped on it still match.
func TestContentFingerprintOfAPlainPageIsTheBodyHash(t *testing.T) {
	const body = "<p>body</p>"

	assert.Equal(t, sha1Hash(body), contentFingerprint(body, "", "full-width", ""))
	assert.Equal(t, sha1Hash(body), contentFingerprint(body, "full-width", "full-width", ""))
	assert.NotEqual(t, sha1Hash(body), contentFingerprint(body, "fixed", "full-width", ""))
	assert.NotEqual(t, sha1Hash(body), contentFingerprint(body, "full-width", "full-width", "🚀"))
	assert.NotEqual(t, contentFingerprint(body, "fixed", "full-width", "🚀"), contentFingerprint(body, "default", "full-width", "🚀"))

	// --content-appearance fixed gives every page without the header a fixed
	// appearance: that is the default, and changes nothing about the hash.
	assert.Equal(t, sha1Hash(body), contentFingerprint(body, "fixed", "fixed", ""))
	assert.NotEqual(t, sha1Hash(body), contentFingerprint(body, "full-width", "fixed", ""))
}

// TestNoOverwriteSkipsAPageEditedInConfluence is the point of the flag: an edit
// made in the web UI is not silently replaced on the next publish.
func TestNoOverwriteSkipsAPageEditedInConfluence(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	server.EditPage(id, "<p>Written by a person.</p>")
	edited := server.Page(id)

	require.NoError(t, Run(config))

	after := server.Page(id)
	assert.Equal(t, "<p>Written by a person.</p>", after.Body,
		"the hand-written body must survive the run")
	assert.Equal(t, edited.Version, after.Version,
		"a skipped page must not gain a version")
}

// TestNoOverwriteKeepsReportingUntilResolved checks that the warning is not a
// one-off. The recorded version is deliberately left alone while a page is
// skipped, so every later run says so again.
func TestNoOverwriteKeepsReportingUntilResolved(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	server.EditPage(id, "<p>Written by a person.</p>")

	require.NoError(t, Run(config))
	require.NoError(t, Run(config))

	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body,
		"the second run must skip the page as well")
}

// TestNoOverwritePublishesAnUntouchedPage is the control. Without it the fix
// could be "stop publishing".
func TestNoOverwritePublishesAnUntouchedPage(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	before := server.Page(id)
	require.NoError(t, Run(config))

	after := server.Page(id)
	assert.Contains(t, after.Body, "From mark",
		"a page nobody touched must still be published")
	assert.Greater(t, after.Version, before.Version,
		"an untouched page is updated as usual")
}

// TestNoOverwriteDoesNotOrphanASkippedPage pins that a skipped page still
// counts as published. It is still on disk, and deleting it because a run
// declined to write to it would be the worst possible reading of the flag.
func TestNoOverwriteDoesNotOrphanASkippedPage(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	server.EditPage(id, "<p>Written by a person.</p>")
	require.NoError(t, Run(config))

	page := server.Page(id)
	require.NotNil(t, page, "the page must not be deleted")
	assert.Equal(t, "<p>Written by a person.</p>", page.Body,
		"the page must have been skipped, which is the case being pinned")
}

// TestNoOverwriteRequiresTrackPages: without the manifest there is nowhere to
// have remembered a version, so the flag could only appear to work.
func TestNoOverwriteRequiresTrackPages(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\nBody.\n")

	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), NoOverwrite: true, Output: io.Discard,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--track-pages")
}

// TestNoOverwriteSurvivesTheRunThatReportsIt covers the run after the warning,
// which is where the protection used to end.
//
// Reporting drift records the document as seen, so it is not mistaken for an
// orphan and deleted -- and recording it rebuilt the entry without the version,
// erasing the very baseline just reported against. The next run read a zero,
// concluded nothing had drifted, and overwrote the edit it had refused to touch
// the run before.
//
// The file has to change between the runs: an unchanged one takes an early
// return that never records, which is why the existing tests never saw this.
func TestNoOverwriteSurvivesTheRunThatReportsIt(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	server.EditPage(id, "<p>Written by a person.</p>")

	// The run that reports the drift, with the document edited too.
	writeFile(t, filepath.Dir(config.Files), "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nSecond version.\n")
	require.NoError(t, Run(config))

	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body,
		"the page must be left alone on the run that reports it")

	// And the run after that, which had the baseline it needed taken away.
	writeFile(t, filepath.Dir(config.Files), "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nThird version.\n")
	require.NoError(t, Run(config))

	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body,
		"the hand-written page must still be there")
}

// TestNoOverwriteDoesNotRetryOverAnEditMadeMidRun: an update refused as a
// conflict is retried against the page's current version, which overwrites
// whatever caused the conflict. An edit that lands after the drift check and
// before the update is the one --no-overwrite is there for, so the retry must
// not go over it.
func TestNoOverwriteDoesNotRetryOverAnEditMadeMidRun(t *testing.T) {
	server, id, config := noOverwriteFixture(t)

	writeFile(t, filepath.Dir(config.Files), "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nSecond version.\n")

	edited := false
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/content/"+id) && !edited {
			edited = true
			server.EditPage(id, "<p>Written by a person.</p>")
		}
		return 0, "", false
	})

	err := Run(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "edited in Confluence while mark was publishing it")
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body,
		"the edit made during the run must survive it")
}

// dataCenterMoveFixture publishes one document under "Parent" on the fake
// dressed as Server or Data Center -- no /api/v2, no content move endpoint and
// no movepage.action -- so that moving it later takes the update fallback,
// which writes a version of its own. It returns the server, the page's id, the
// "Other" parent's id and the config, whose Output the caller replaces as it
// needs.
func dataCenterMoveFixture(
	t *testing.T, configure func(*Config),
) (*confluencetest.Server, string, string, Config) {
	t.Helper()

	return dataCenterMoveFixtureWith(t, false, configure)
}

// dataCenterMoveFixtureWith is dataCenterMoveFixture, with movepage.action
// served when withAction is set.
func dataCenterMoveFixtureWith(
	t *testing.T, withAction bool, configure func(*Config),
) (*confluencetest.Server, string, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	server.SetDataCenter()
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "/move/") ||
			(!withAction && r.URL.Path == "/pages/movepage.action") {
			return http.StatusNotFound, `{"message":"no such endpoint"}`, true
		}
		return 0, "", false
	})

	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)
	other := server.AddPage("DOCS", "Other", "page", home.ID)

	dir := t.TempDir()
	writeMovingDoc(t, dir, "Parent", "From mark.")

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:      filepath.Join(dir, "*.md"),
		Features:   []string{"mention"},
		TrackPages: true,
		Output:     io.Discard,
	}
	configure(&config)

	require.NoError(t, Run(config))

	api := confluence.NewAPI(server.URL, "user", "token", false)
	doc, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, doc)

	return server, doc.ID, other.ID, config
}

func writeMovingDoc(t *testing.T, dir, parent, body string) {
	t.Helper()

	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: "+parent+" -->\n<!-- Title: Doc -->\n\n"+body+"\n")
}

// TestNoOverwriteDoesNotMistakeAMoveForAnEdit: on Server/DC a move is an update,
// and an update is a new version. It happens before --no-overwrite compares the
// page's version with the one mark recorded, so the version mark had just
// written by moving the page looked like somebody else's edit: the page was
// reported as edited in Confluence, left unpublished, and -- since a skipped
// page keeps its old baseline -- reported again on every run after.
func TestNoOverwriteDoesNotMistakeAMoveForAnEdit(t *testing.T) {
	server, id, other, config := dataCenterMoveFixture(t, func(c *Config) {
		c.NoOverwrite = true
	})
	dir := filepath.Dir(config.Files)

	writeMovingDoc(t, dir, "Other", "Moved.")
	moved := runReported(t, config)

	assert.NotEqual(t, report.StatusSkipped, moved.Status, moved.Reason)
	assert.Equal(t, other, server.Page(id).ParentID)
	assert.Contains(t, server.Page(id).Body, "Moved.", "the move run has to publish as well")

	writeMovingDoc(t, dir, "Other", "After the move.")
	after := runReported(t, config)

	assert.NotEqual(t, report.StatusSkipped, after.Status, after.Reason)
	assert.Contains(t, server.Page(id).Body, "After the move.")
}

// TestNoOverwriteAfterAChangesOnlyMove: a document whose only change is its
// parent compiles to the page it already is, so --changes-only moves the page
// and publishes nothing. The version the move wrote still has to become the
// baseline, or the next --no-overwrite run finds a version mark never
// recorded and takes it for an edit.
func TestNoOverwriteAfterAChangesOnlyMove(t *testing.T) {
	for _, noOverwrite := range []bool{false, true} {
		name := map[bool]string{false: "changes-only", true: "changes-only and no-overwrite"}[noOverwrite]
		t.Run(name, func(t *testing.T) {
			server, id, other, config := dataCenterMoveFixture(t, func(c *Config) {
				c.ChangesOnly = true
				c.NoOverwrite = noOverwrite
			})
			dir := filepath.Dir(config.Files)

			writeMovingDoc(t, dir, "Other", "From mark.")
			moved := runReported(t, config)
			assert.NotEqual(t, report.StatusSkipped, moved.Status, moved.Reason)
			require.Equal(t, other, server.Page(id).ParentID)

			config.ChangesOnly = false
			config.NoOverwrite = true
			writeMovingDoc(t, dir, "Other", "After the move.")
			after := runReported(t, config)

			assert.NotEqual(t, report.StatusSkipped, after.Status, after.Reason)
			assert.Contains(t, server.Page(id).Body, "After the move.")
		})
	}
}

// TestNoOverwriteStillSeesAnEditMadeBeforeAMove is the other half: the version
// a move writes is discounted only when the page was at the version mark
// recorded before it. An edit made in Confluence and then carried along by the
// move is still an edit, and is still reported on every run.
func TestNoOverwriteStillSeesAnEditMadeBeforeAMove(t *testing.T) {
	server, id, other, config := dataCenterMoveFixture(t, func(c *Config) {
		c.NoOverwrite = true
	})
	dir := filepath.Dir(config.Files)

	server.EditPage(id, "<p>Written by a person.</p>")

	writeMovingDoc(t, dir, "Other", "Moved.")
	moved := runReported(t, config)

	assert.Equal(t, report.StatusSkipped, moved.Status)
	assert.Contains(t, moved.Reason, "edited in Confluence")
	assert.Equal(t, other, server.Page(id).ParentID,
		"moving the page overwrites nothing, so it is still moved")
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body)

	writeMovingDoc(t, dir, "Other", "After the move.")
	after := runReported(t, config)

	assert.Equal(t, report.StatusSkipped, after.Status, "and it is said again until resolved")
	assert.Equal(t, "<p>Written by a person.</p>", server.Page(id).Body)
}

// TestNoOverwriteAfterAMoveByAction: movepage.action moves the page without
// an update, and the publish that follows is not taken for a conflict.
func TestNoOverwriteAfterAMoveByAction(t *testing.T) {
	server, id, other, config := dataCenterMoveFixtureWith(t, true, func(c *Config) {
		c.NoOverwrite = true
	})
	dir := filepath.Dir(config.Files)

	writeMovingDoc(t, dir, "Other", "Moved.")
	moved := runReported(t, config)

	assert.NotEqual(t, report.StatusSkipped, moved.Status, moved.Reason)
	assert.Equal(t, other, server.Page(id).ParentID)
	assert.Contains(t, server.Page(id).Body, "Moved.")
	assert.Equal(t, 1, server.CountRequests(http.MethodPost, "/pages/movepage.action"))
}

// TestFormatVersionMessage covers the round trip --changes-only depends on:
// whatever formatVersionMessage writes must be readable by
// readContentHash on the next run, or the page updates every time.
func TestFormatVersionMessage(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"

	t.Run("hash leads so truncation cannot eat it", func(t *testing.T) {
		// Confluence bounds the version message. With the tag appended, a long
		// operator message pushed it past the limit and silently disabled
		// change detection; leading with the tag costs the prose instead.
		long := strings.Repeat("a very long commit subject ", 40)
		got := formatVersionMessage(long, hash)

		assert.True(t, strings.HasPrefix(got, "[v"+hash+"]"),
			"the fingerprint must come first")

		for _, limit := range []int{255, 128, 64, 43} {
			truncated := got
			if len(truncated) > limit {
				truncated = truncated[:limit]
			}
			matches := readContentHash(truncated)
			assert.Equal(t, hash, matches, "hash must survive truncation at %d", limit)
		}
	})

	t.Run("empty operator message leaves no stray separator", func(t *testing.T) {
		assert.Equal(t, "[v"+hash+"]", formatVersionMessage("", hash))
	})

	t.Run("operator message is preserved", func(t *testing.T) {
		got := formatVersionMessage("published by CI", hash)
		assert.Equal(t, "[v"+hash+"] published by CI", got)
	})

	t.Run("round trips through the reader", func(t *testing.T) {
		for _, msg := range []string{"", "published by CI", "[brackets] and (parens)"} {
			assert.Equal(t, hash, readContentHash(formatVersionMessage(msg, hash)),
				"message %q must round trip", msg)
		}
	})
}

// TestContentHashPatternReadsLegacyTrailingTag pins backward compatibility:
// pages stamped by an older mark carry the tag at the end. If those stopped
// matching, every page in every space would take one spurious update on the
// first run after upgrading.
func TestContentHashPatternReadsLegacyTrailingTag(t *testing.T) {
	const hash = "89abcdef0123456789abcdef0123456789abcdef"

	legacy := fmt.Sprintf("%s [v%s]", "published by CI", hash)
	assert.Equal(t, hash, readContentHash(legacy),
		"the old trailing format must still be recognised")
}

// TestContentHashPatternIgnoresNonHashes guards against a version message that
// merely looks tag-shaped being read as a fingerprint.
func TestContentHashPatternIgnoresNonHashes(t *testing.T) {
	for _, msg := range []string{
		"[v123]", // too short
		"[vZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ]", // not hex
		"[v0123456789abcdef0123456789abcdef0123456]",  // 39 chars
		"no tag at all",
	} {
		assert.Empty(t, readContentHash(msg),
			"%q must not be read as a fingerprint", msg)
	}
}

// TestReadContentHashIgnoresTagsInsideTheMessage pins the reason both patterns
// are anchored.
//
// An operator's --version-message can itself contain something tag-shaped --
// someone quoting a previous version message, most plausibly. An unanchored
// search would find that copy first and prefer it over the real fingerprint at
// the end. The result is only an update that was not needed, never a change
// that was missed, but position is free information and discarding it makes
// the wrong answer reachable for no benefit.
func TestReadContentHashIgnoresTagsInsideTheMessage(t *testing.T) {
	const real = "0123456789abcdef0123456789abcdef01234567"
	const quoted = "89abcdef0123456789abcdef0123456789abcdef"

	// A page still carrying the legacy trailing layout, whose operator message
	// quotes an older tag.
	legacy := fmt.Sprintf("re-publish of [v%s] [v%s]", quoted, real)
	assert.Equal(t, real, readContentHash(legacy),
		"the trailing fingerprint is the real one, not the quoted copy")

	// The layout mark writes now: its own tag leads, so it wins outright.
	current := fmt.Sprintf("[v%s] re-publish of [v%s]", real, quoted)
	assert.Equal(t, real, readContentHash(current))

	// A tag that is neither leading nor trailing is not a fingerprint at all.
	buried := fmt.Sprintf("see [v%s] for context", quoted)
	assert.Empty(t, readContentHash(buried))
}
