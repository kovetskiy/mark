package mark

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// previewOf runs config as a dry run with the given output format and returns
// what it wrote.
func previewOf(t *testing.T, config Config, format string) string {
	t.Helper()
	var out bytes.Buffer
	config.DryRun = true
	config.OutputFormat = format
	config.Output = &out
	require.NoError(t, Run(config))
	return out.String()
}

// decodeReport reads a JSON report, failing the test if the output is not one.
func decodeReport(t *testing.T, data []byte) *report.Report {
	t.Helper()
	r := &report.Report{}
	require.NoError(t, json.Unmarshal(data, r), "output is not a JSON report: %s", data)
	return r
}

// mustFind returns the id of the page titled title in DOCS.
func mustFind(t *testing.T, server *confluencetest.Server, title string) string {
	t.Helper()
	api := confluence.NewAPI(server.URL, "user", "token", false)
	pg, err := api.FindPage("DOCS", title, "page")
	require.NoError(t, err)
	require.NotNil(t, pg)
	return pg.ID
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
