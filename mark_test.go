package mark

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/mermaid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessFileFetchesAttachmentsOnce pins the fix for the duplicate
// GetAttachments. ProcessFile resolves attachments twice per page -- declared
// attachments, then diagrams found while rendering -- and each pass used to
// issue its own paginated fetch of the same remote list.
func TestProcessFileFetchesAttachmentsOnce(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	// A parentless page is only valid as the space homepage, so build a
	// realistic tree: Home is the homepage, Parent sits under it.
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	// A 1x1 PNG so the image-dimension probe has something valid to read.
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), png, 0o600))

	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Attachment Doc -->
<!-- Attachment: logo.png -->

# Attachment Doc

![logo](logo.png)
`)

	config := Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    file,
		Features: []string{"mention"},
		Output:   io.Discard,
	}

	server.ResetRequests()

	target, err := ProcessFile(file, api, config)
	require.NoError(t, err)
	require.NotNil(t, target)

	assert.Equal(t, 1, server.CountRequests("GET", "/child/attachment"),
		"the remote attachment list should be fetched once per page")

	stored := server.Attachments(target.ID)
	require.Len(t, stored, 1)
	assert.Equal(t, "logo.png", stored[0].Filename)
}

// TestProcessFileCreatesAndUpdatesPage is a smoke test for the whole
// orchestration path, which had no coverage at all: metadata, ancestry, page
// creation, body upload and labels in one run.
func TestProcessFileCreatesAndUpdatesPage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	// A parentless page is only valid as the space homepage, so build a
	// realistic tree: Home is the homepage, Parent sits under it.
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: New Page -->
<!-- Label: alpha -->

# New Page

Some **content**.
`)

	config := Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    file,
		Features: []string{"mention"},
		Output:   io.Discard,
	}

	target, err := ProcessFile(file, api, config)
	require.NoError(t, err)
	require.NotNil(t, target)

	stored := server.Page(target.ID)
	require.NotNil(t, stored)
	assert.Equal(t, "New Page", stored.Title)
	assert.Contains(t, stored.Body, "<strong>content</strong>")
	assert.Equal(t, []string{"alpha"}, stored.Labels)

	// The page was created under the declared parent.
	parent, err := api.FindPage("DOCS", "Parent", "page")
	require.NoError(t, err)
	assert.Equal(t, parent.ID, stored.ParentID)
}

func TestProcessFileAppliesFrontMatterRestrictions(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	file := writeFile(t, t.TempDir(), "doc.md", `---
space: DOCS
parents: [Parent]
title: Restricted
restrictions:
  read:
    groups: [docs-readers]
---

# Restricted
`)

	target, err := ProcessFile(file, api, Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"frontmatter"}, Output: io.Discard,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, server.CountRequests("PUT", "/restriction"))

	restrictionIndex, contentIndex := -1, -1
	for i, request := range server.Requests() {
		if request.Method == http.MethodPut && strings.HasSuffix(request.Path, "/restriction") {
			restrictionIndex = i
		}
		if request.Method == http.MethodPut && strings.HasSuffix(request.Path, "/content/"+target.ID) {
			contentIndex = i
		}
	}
	assert.Less(t, restrictionIndex, contentIndex,
		"restrictions must be set before content is uploaded")
}

// TestRootLevelPageIsAdoptedRatherThanRefused is issue #88's neighbourhood.
// Publishing to the space homepage works, and always has since the homepage is
// recognised. A page sitting beside it at the root did not: mark placed a
// document with no declared parents under the space root page, saw the existing
// page was not there, and refused.
//
// That left such a page unpublishable by any means, because declaring the
// parent it ought to have is exactly what produces the state being refused.
func TestRootLevelPageIsAdoptedRatherThanRefused(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	// Somebody created this at the space root, outside mark.
	orphan := server.AddPage("DOCS", "Standalone", "page", "")

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Standalone -->\n\nAdopted body.\n")
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
	}), "a page at the space root must be publishable")

	after := server.Page(orphan.ID)
	require.NotNil(t, after)
	assert.Contains(t, after.Body, "Adopted body.",
		"the existing page should have been updated, not passed over")
	assert.Equal(t, home.ID, after.ParentID,
		"and moved under the root page, which is where a document with no declared parents belongs")

	assert.Equal(t, 1, countPagesTitled(t, server, "Standalone"),
		"no second page should have appeared")
	_ = api
}

// TestHomepageItselfIsStillPublishable guards the case that already worked:
// the homepage has no ancestors either, and must not be mistaken for a page
// that needs moving under itself.
func TestHomepageItselfIsStillPublishable(t *testing.T) {
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Team Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "home.md", "<!-- Space: DOCS -->\n<!-- Title: Team Home -->\n\nWelcome!\n")
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
	}))

	after := server.Page(home.ID)
	require.NotNil(t, after)
	assert.Contains(t, after.Body, "Welcome!")
	assert.Empty(t, after.ParentID, "the homepage must stay at the root")
}

// TestIgnoredRegionIsNotPublished is the point of issue #317: a document that
// reads well in both places. Something that renders badly in Confluence -- a
// GitHub table of contents, a plain-text stand-in for a macro -- stays in the
// file and out of the page.
func TestIgnoredRegionIsNotPublished(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Doc -->

Published body.

<!-- ac:ignore -->
## Contents

- [One](#one)
- [Two](#two)
<!-- ac:ignore end -->

Also published.
`)
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
	}))

	page, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	body := server.Page(page.ID).Body
	assert.Contains(t, body, "Published body.")
	assert.Contains(t, body, "Also published.")
	assert.NotContains(t, body, "Contents", "the ignored region must not reach Confluence")
	assert.NotContains(t, body, "ac:ignore", "and neither must the markers")
}

// TestIgnoredRegionTakesItsAttachmentsWithIt: an image only referenced inside
// an ignored region must not be uploaded. Nothing on the page would point at
// it, so it would sit there as an orphan.
func TestIgnoredRegionTakesItsAttachmentsWithIt(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hidden.png"), png, 0o600))

	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Doc -->

Body.

<!-- ac:ignore -->
![hidden](hidden.png)
<!-- ac:ignore end -->
`)
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
	}))

	page, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	assert.Empty(t, server.Attachments(page.ID),
		"an image only referenced inside an ignored region should not be uploaded")
}

// TestIgnoredRegionInsideAnIncludeIsNotPublished: a fragment can mark regions
// as not for Confluence just as the document including it can. Without this the
// region and its markers both reached the page, which is the more surprising
// half -- the feature appeared to work until somebody moved the content into an
// include.
func TestIgnoredRegionInsideAnIncludeIsNotPublished(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	writeFile(t, dir, "part.md", `Included body.

<!-- ac:ignore -->
HIDDEN FROM CONFLUENCE
<!-- ac:ignore end -->
`)
	file := writeFile(t, dir, "doc.md", `<!-- Space: DOCS -->
<!-- Parent: Parent -->
<!-- Title: Doc -->

Top.

<!-- Include: part.md -->
`)
	require.NoError(t, Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: file, Features: []string{"mention"}, Output: io.Discard,
	}))

	page, err := api.FindPage("DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	body := server.Page(page.ID).Body
	assert.Contains(t, body, "Included body.")
	assert.NotContains(t, body, "HIDDEN FROM CONFLUENCE")
	assert.NotContains(t, body, "ac:ignore")
}

// processFileFixture is one publishable document and the server it goes to,
// for comparing what ProcessFile and Run make of the same settings.
func processFileFixture(t *testing.T, body string) (*confluencetest.Server, *confluence.API, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n"+body)

	config := Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    filepath.Join(dir, "doc.md"),
		Features: []string{"mention"},
		Output:   io.Discard,
	}

	return server, confluence.NewAPI(server.URL, "user", "token", false), file, config
}

// TestProcessFileRefusesWhatRunRefuses: ProcessFile is the library's other way
// in, and it used to skip every check Run makes of the settings. A MermaidOutput
// it did not recognise published a PNG without a word, and a bundle asked for
// alongside one was dropped with it.
func TestProcessFileRefusesWhatRunRefuses(t *testing.T) {
	cases := map[string]func(*Config){
		"an unknown mermaid engine": func(c *Config) { c.MermaidEngine = "graphviz" },
		"an unknown mermaid output": func(c *Config) { c.MermaidOutput = "jpeg" },
		"a bundle inside a png":     func(c *Config) { c.MermaidBundle = true },
		"an unknown d2 output":      func(c *Config) { c.D2Output = "jpeg" },
		"a d2 scale of nothing": func(c *Config) {
			c.Features = []string{"d2"}
			c.D2Scale = math.NaN()
		},
		"warn-only with nothing checked": func(c *Config) { c.CheckLinksWarnOnly = true },
		"an unknown link check":          func(c *Config) { c.CheckLinks = []string{"everything"} },
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			server, api, file, config := processFileFixture(t, "Body.\n")
			change(&config)

			runErr := Run(config)
			require.Error(t, runErr)

			server.ResetRequests()

			target, err := ProcessFile(file, api, config)
			require.Error(t, err)
			assert.Nil(t, target)
			assert.Equal(t, runErr.Error(), err.Error(), "the same setting is refused in the same words")
			assert.Empty(t, server.Requests(), "refused before Confluence is asked anything")
		})
	}
}

// TestProcessFileUsesTheMermaidEngine: the engine is a process-wide choice
// that Run made and ProcessFile did not, so a library caller asking for merman
// got whichever engine happened to be current -- chrome, unless something else
// in the process had run first.
func TestProcessFileUsesTheMermaidEngine(t *testing.T) {
	var logged bytes.Buffer

	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	t.Cleanup(func() {
		log.Logger = restore
		require.NoError(t, mermaid.UseEngine(""))
	})

	_, api, file, config := processFileFixture(t, "Body.\n")
	config.MermaidEngine = mermaid.EngineMerman

	_, err := ProcessFile(file, api, config)
	require.NoError(t, err)

	// The warning is said by choosing merman and by nothing else, so it is how
	// the choice shows from outside the package.
	assert.Contains(t, logged.String(), "merman render engine is experimental")
}

// TestProcessFileReportsMissingPagesAsRunDoes: the ac: links a run leaves
// unresolved are counted and introduced, and ProcessFile's were the bare list
// with nothing in front of it.
func TestProcessFileReportsMissingPagesAsRunDoes(t *testing.T) {
	_, api, file, config := processFileFixture(t, "See [it](ac:Nowhere).\n")
	config.CheckLinks = []string{"confluence"}

	runErr := Run(config)
	require.Error(t, runErr)

	_, err := ProcessFile(file, api, config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "1 link does not resolve:\n  ")
	assert.Contains(t, err.Error(), `"Nowhere"`)
	assert.Equal(t, runErr.Error(), err.Error())
}

// TestRunContextStopsBetweenFiles: Run is an exported entry point, so a library
// caller can be publishing hundreds of documents with no way to ask it to stop.
// Cancellation is checked between files rather than inside one, since part way
// through a page is the one place stopping would leave a page half written.
func TestRunContextStopsBetweenFiles(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("First"))
	writeFile(t, dir, "b.md", markdownWithTitle("Second"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RunContext(ctx, publishConfig(server.URL, filepath.Join(dir, "*.md")))

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, countPagesTitled(t, server, "First"),
		"a run cancelled before it starts publishes nothing")
}

// TestRunContextPublishesWhenNotCancelled is the control.
func TestRunContextPublishesWhenNotCancelled(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Published"))

	require.NoError(t, RunContext(context.Background(), publishConfig(server.URL, file)))
	assert.Equal(t, 1, countPagesTitled(t, server, "Published"))
}

// TestProcessFileContextIsCancellable covers the single-file entry point, which
// a caller loops over itself.
func TestProcessFileContextIsCancellable(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Not Published"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ProcessFileContext(ctx, file, api, publishConfig(server.URL, file))

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, countPagesTitled(t, server, "Not Published"))
}

// TestRunStillWorksWithoutAContext: the old entry points keep their signatures,
// since they are what every existing caller uses.
func TestRunStillWorksWithoutAContext(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Plain Run"))

	require.NoError(t, Run(publishConfig(server.URL, file)))
	assert.Equal(t, 1, countPagesTitled(t, server, "Plain Run"))
}

// TestTargetURLTakesOneFile: a page id names one page, so a pattern matching
// several files is refused before anything is published over it.
func TestTargetURLTakesOneFile(t *testing.T) {
	server, _ := docsSpace(t)
	target := server.AddPage("DOCS", "Target", "page", "")
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "# A\n\nFirst.\n")
	writeFile(t, dir, "b.md", "# B\n\nSecond.\n")

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.TrackPages = false
	config.PageID = target.ID

	err := Run(config)
	require.ErrorContains(t, err, "matched 2 files")
	assert.Equal(t, 0, server.CountRequests(http.MethodPut, "/content/"+target.ID), "nothing is published")

	config.Files = filepath.Join(dir, "a.md")
	require.NoError(t, Run(config))
	assert.Contains(t, server.Page(target.ID).Body, "First.")
}

// BenchmarkRunCompileOnly exercises the whole per-file loop in compile-only
// mode, which never reaches the network, so it measures the fixed per-file
// overhead that Run pays regardless of Confluence latency.
func BenchmarkRunCompileOnly(b *testing.B) {
	// Other tests in this package raise the global level; log formatting would
	// otherwise dominate the measurement.
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	b.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	for _, fileCount := range []int{1, 50} {
		b.Run(fmt.Sprintf("files=%d", fileCount), func(b *testing.B) {
			dir := b.TempDir()
			for i := range fileCount {
				body := fmt.Sprintf(
					"<!-- Space: TEST -->\n<!-- Title: Page %d -->\n\n"+
						"# Page %d\n\nSome **text** with a [link](https://example.com).\n\n"+
						"| a | b |\n| --- | --- |\n| 1 | 2 |\n\n```go\nfunc main() {}\n```\n",
					i, i,
				)
				path := filepath.Join(dir, fmt.Sprintf("page-%03d.md", i))
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					b.Fatal(err)
				}
			}

			config := Config{
				BaseURL:     "http://127.0.0.1:1",
				Files:       filepath.Join(dir, "*.md"),
				CompileOnly: true,
				Features:    []string{"mention"},
				Output:      io.Discard,
			}

			b.ReportAllocs()
			for b.Loop() {
				if err := Run(config); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
