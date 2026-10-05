package mark

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dryRunDocBody(body string) string {
	return "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n" + body + "\n"
}

func TestDryRunChangesOnlyReportsPerPage(t *testing.T) {
	publish := func(t *testing.T) (string, Config, *bytes.Buffer, func() []string) {
		server, _ := docsSpace(t)
		dir := t.TempDir()
		file := writeFile(t, dir, "doc.md", dryRunDocBody("stable body"))

		config := Config{
			BaseURL: server.URL, Username: "user", Password: "token",
			Files: file, Features: []string{"mention"}, Output: &bytes.Buffer{},
			ChangesOnly: true,
		}
		require.NoError(t, Run(config))

		var out bytes.Buffer
		config.Output = &out
		config.DryRun = true
		config.OutputFormat = "json"

		writes := func() []string {
			var w []string
			for _, r := range server.Requests() {
				if r.Method != http.MethodGet {
					w = append(w, r.Method+" "+r.Path)
				}
			}
			return w
		}
		return file, config, &out, writes
	}

	t.Run("unchanged page is reported and its HTML is not printed", func(t *testing.T) {
		_, config, out, _ := publish(t)
		require.NoError(t, Run(config))

		assert.Contains(t, out.String(), `"status": "unchanged"`)
		assert.NotContains(t, out.String(), "<p>", "an unchanged page must not be dumped")
	})

	t.Run("changed page is reported as would-update and printed", func(t *testing.T) {
		file, config, out, _ := publish(t)
		require.NoError(t, os.WriteFile(file, []byte(dryRunDocBody("different body")), 0o600))
		require.NoError(t, Run(config))

		assert.Contains(t, out.String(), `"status": "would-update"`)
		assert.Contains(t, out.String(), "different body")
	})

	t.Run("writes nothing", func(t *testing.T) {
		file, config, _, writes := publish(t)
		before := len(writes())
		require.NoError(t, os.WriteFile(file, []byte(dryRunDocBody("different body")), 0o600))
		require.NoError(t, Run(config))
		assert.Len(t, writes(), before)
	})
}

func TestDryRunChangesOnlyNewPageWouldBeCreated(t *testing.T) {
	server, _ := docsSpace(t)
	file := writeFile(t, t.TempDir(), "doc.md", dryRunDocBody("fresh"))

	var out bytes.Buffer
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: &out,
		ChangesOnly: true, DryRun: true, OutputFormat: "json",
	}
	require.NoError(t, Run(config))

	assert.Contains(t, out.String(), `"status": "would-create"`)
	assert.Contains(t, out.String(), "fresh")

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
	var out bytes.Buffer
	config.DryRun = true
	config.OutputFormat = "json"
	config.Output = &out
	require.NoError(t, Run(config))

	assert.Contains(t, out.String(), `"status": "would-update"`)
}

func TestDryRunAloneStillPrintsEveryPage(t *testing.T) {
	server, _ := docsSpace(t)
	file := writeFile(t, t.TempDir(), "doc.md", dryRunDocBody("stable body"))
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: &bytes.Buffer{},
		ChangesOnly: true,
	}
	require.NoError(t, Run(config))

	var out bytes.Buffer
	config.ChangesOnly = false
	config.DryRun = true
	config.Output = &out
	require.NoError(t, Run(config))

	assert.Contains(t, out.String(), "stable body")
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

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: &bytes.Buffer{},
		ChangesOnly: true,
	}
	require.NoError(t, Run(config))

	var out bytes.Buffer
	config.Output = &out
	config.DryRun = true
	config.OutputFormat = "github"
	require.NoError(t, Run(config))

	assert.NotContains(t, out.String(), "would update")
	assert.NotContains(t, out.String(), "logo.png", "an unchanged page must not be dumped")

	config.OutputFormat = "json"
	out.Reset()
	require.NoError(t, Run(config))

	assert.Contains(t, out.String(), `"status": "unchanged"`)
}

func TestDryRunChangesOnlyChangedAttachment(t *testing.T) {
	// A changed file is re-uploaded under a new download link, so only a page
	// that holds the link differs; an image found while rendering names the file.
	preview := func(t *testing.T, body string) string {
		server, _ := docsSpace(t)
		dir := t.TempDir()
		logo := filepath.Join(dir, "logo.png")
		require.NoError(t, os.WriteFile(logo, []byte("one"), 0o600))
		file := writeFile(t, dir, "doc.md", dryRunDocBody(body))

		config := Config{
			BaseURL: server.URL, Username: "user", Password: "token",
			Files: file, Features: []string{"mention"}, Output: &bytes.Buffer{},
			ChangesOnly: true,
		}
		require.NoError(t, Run(config))

		require.NoError(t, os.WriteFile(logo, []byte("two"), 0o600))
		var out bytes.Buffer
		config.Output = &out
		config.DryRun = true
		config.OutputFormat = "github"
		require.NoError(t, Run(config))

		return out.String()
	}

	t.Run("linked", func(t *testing.T) {
		assert.Contains(t, preview(t, "<!-- Attachment: logo.png -->\n\n[the logo](logo.png)"), "would update")
	})

	t.Run("embedded", func(t *testing.T) {
		assert.NotContains(t, preview(t, "![logo](logo.png)"), "would update")
	})
}

func TestDryRunChangesOnlyNoOverwriteReportsAnEditedPageSkipped(t *testing.T) {
	server, id, config := noOverwriteFixture(t)
	server.EditPage(id, "<p>Written by a person.</p>")

	var out bytes.Buffer
	config.ChangesOnly = true
	config.DryRun = true
	config.OutputFormat = "json"
	config.Output = &out
	require.NoError(t, Run(config))

	assert.Contains(t, out.String(), `"status": "skipped"`)
	assert.NotContains(t, out.String(), "<p>From mark.</p>", "a page left alone must not be dumped")
}

// TestDryRunChangesOnlySkippedPageIsNotCompiled keeps the order of a real run:
// a page that --no-overwrite leaves alone is reported as skipped even when its
// source could not be compiled.
func TestDryRunChangesOnlySkippedPageIsNotCompiled(t *testing.T) {
	server, id, config := noOverwriteFixture(t)
	server.EditPage(id, "<p>Written by a person.</p>")

	writeFile(t, filepath.Dir(config.Files), "doc.md",
		dryRunDocBody("<!-- Attachment: missing.png -->\n\nFrom mark."))

	var out bytes.Buffer
	config.ChangesOnly = true
	config.DryRun = true
	config.OutputFormat = "json"
	config.Output = &out
	require.NoError(t, Run(config))

	assert.Contains(t, out.String(), `"status": "skipped"`)
}

// TestDryRunChangesOnlyLiteralPlaceholderTextIsNotAChange covers a document that
// happens to contain the text an earlier version used to mark pending links.
func TestDryRunChangesOnlyLiteralPlaceholderTextIsNotAChange(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", dryRunDocBody("see /mark-dry-run-pending/ in the docs"))

	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: &bytes.Buffer{},
		ChangesOnly: true,
	}
	require.NoError(t, Run(config))

	var out bytes.Buffer
	config.Output = &out
	config.DryRun = true
	config.OutputFormat = "github"
	require.NoError(t, Run(config))

	assert.NotContains(t, out.String(), "would update")
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
