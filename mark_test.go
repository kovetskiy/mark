package mark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/mermaid"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublisherFetchesAttachmentsOnce pins the fix for the duplicate
// GetAttachments. A Publisher resolves attachments twice per page -- declared
// attachments, then diagrams found while rendering -- and each pass used to
// issue its own paginated fetch of the same remote list.
func TestPublisherFetchesAttachmentsOnce(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: file,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}

	server.ResetRequests()

	target, err := publishOne(context.Background(), file, api, config.PublishConfig)
	require.NoError(t, err)
	require.NotNil(t, target)

	assert.Equal(t, 1, server.CountRequests("GET", "/child/attachment"),
		"the remote attachment list should be fetched once per page")

	stored := server.Attachments(target.ID)
	require.Len(t, stored, 1)
	assert.Equal(t, "logo.png", stored[0].Filename)
}

// TestPublisherCreatesAndUpdatesPage is a smoke test for the whole
// orchestration path, which had no coverage at all: metadata, ancestry, page
// creation, body upload and labels in one run.
func TestPublisherCreatesAndUpdatesPage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

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
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: file,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}

	target, err := publishOne(context.Background(), file, api, config.PublishConfig)
	require.NoError(t, err)
	require.NotNil(t, target)

	stored := server.Page(target.ID)
	require.NotNil(t, stored)
	assert.Equal(t, "New Page", stored.Title)
	assert.Contains(t, stored.Body, "<strong>content</strong>")
	assert.Equal(t, []string{"alpha"}, stored.Labels)

	// The page was created under the declared parent.
	parent, err := api.FindPage(context.Background(), "DOCS", "Parent", "page")
	require.NoError(t, err)
	assert.Equal(t, parent.ID, stored.ParentID)
}

func TestPublisherAppliesFrontMatterRestrictions(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
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

	target, err := publishOne(context.Background(), file, api, PublishConfig{
		Rendering: RenderingConfig{
			Features: []string{"frontmatter"},
		},
		Output: io.Discard,
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
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	// Somebody created this at the space root, outside mark.
	orphan := server.AddPage("DOCS", "Standalone", "page", "")

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Standalone -->\n\nAdopted body.\n")
	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: file,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
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
	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: file,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
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
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
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
	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: file,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}))

	page, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
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
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
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
	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: file,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}))

	page, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
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
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})
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
	require.NoError(t, Run(context.Background(), Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: file,
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}))

	page, err := api.FindPage(context.Background(), "DOCS", "Doc", "page")
	require.NoError(t, err)
	require.NotNil(t, page)

	body := server.Page(page.ID).Body
	assert.Contains(t, body, "Included body.")
	assert.NotContains(t, body, "HIDDEN FROM CONFLUENCE")
	assert.NotContains(t, body, "ac:ignore")
}

// publisherFixture is one publishable document and the server it goes to,
// for comparing what a Publisher and Run make of the same settings.
func publisherFixture(t *testing.T, body string) (*confluencetest.Server, *confluence.API, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n"+body)

	config := Config{
		PublishConfig: PublishConfig{
			Rendering: RenderingConfig{
				Features: []string{"mention"},
			},
			Output: io.Discard,
		},
		Files: filepath.Join(dir, "doc.md"),
		Connection: ConnectionConfig{
			BaseURL:  server.URL,
			Username: "user",
			Password: "token",
		},
	}

	return server, confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}), file, config
}

// TestPublisherRefusesWhatRunRefuses: a Publisher is the library's other way
// in, and refuses the publish settings Run refuses, in the same words and
// before Confluence is asked anything.
func TestPublisherRefusesWhatRunRefuses(t *testing.T) {
	cases := map[string]func(*Config){
		"an unknown mermaid engine": func(c *Config) { c.Rendering.MermaidEngine = "graphviz" },
		"an unknown mermaid output": func(c *Config) { c.Rendering.MermaidOutput = "jpeg" },
		"a bundle inside a png":     func(c *Config) { c.Rendering.MermaidBundle = true },
		"an unknown d2 output":      func(c *Config) { c.Rendering.D2Output = "jpeg" },
		"an unknown math output":    func(c *Config) { c.Rendering.MathOutput = "jpeg" },
		"an unknown appearance":     func(c *Config) { c.Page.ContentAppearance = "wide" },
		"a fixed appearance":        func(c *Config) { c.Page.ContentAppearance = "fixed" },
		"a d2 scale of nothing": func(c *Config) {
			c.Rendering.Features = []string{"d2"}
			c.Rendering.D2Scale = math.NaN()
		},
		"warn-only with nothing checked": func(c *Config) { c.Links.CheckLinksWarnOnly = true },
		"an unknown link check":          func(c *Config) { c.Links.CheckLinks = []string{"everything"} },
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			server, api, _, config := publisherFixture(t, "Body.\n")
			change(&config)

			runErr := Run(context.Background(), config)
			require.Error(t, runErr)

			server.ResetRequests()

			publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
			require.Error(t, err)
			assert.Nil(t, publisher)
			assert.Equal(t, runErr.Error(), err.Error(), "the same setting is refused in the same words")
			assert.Empty(t, server.Requests(), "refused before Confluence is asked anything")
		})
	}
}

// TestRunRefusesAnUnknownEngineBeforeTheGlob: a mermaid engine mark does not
// know is a setting like any other, said before a glob that matches nothing.
func TestRunRefusesAnUnknownEngineBeforeTheGlob(t *testing.T) {
	_, _, file, config := publisherFixture(t, "Body.\n")
	config.Files = filepath.Join(filepath.Dir(file), "missing-*.md")
	config.Rendering.MermaidEngine = "graphviz"

	err := Run(context.Background(), config)
	require.ErrorContains(t, err, "unknown mermaid engine")
}

// TestPublisherUsesTheMermaidEngine: a library caller asking a Publisher for
// merman gets merman, not whichever engine some earlier run chose.
func TestPublisherUsesTheMermaidEngine(t *testing.T) {
	// No merman to be found, so the diagram fails in a way only merman can:
	// Chrome would have drawn it.
	t.Setenv("PATH", t.TempDir())

	_, api, file, config := publisherFixture(t, "```mermaid\ngraph TD\n  A --> B\n```\n")
	config.Rendering.Features = []string{"mermaid"}
	config.Rendering.MermaidEngine = mermaid.EngineMerman

	_, err := publishOne(context.Background(), file, api, config.PublishConfig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to start the merman render engine")
}

// TestPublisherReportsMissingPagesAsRunDoes: the ac: links a run leaves
// unresolved are counted and introduced, and so are a Publisher's.
func TestPublisherReportsMissingPagesAsRunDoes(t *testing.T) {
	_, api, file, config := publisherFixture(t, "See [it](ac:Nowhere).\n")
	config.Links.CheckLinks = []string{"confluence"}

	runErr := Run(context.Background(), config)
	require.Error(t, runErr)

	_, err := publishOne(context.Background(), file, api, config.PublishConfig)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "1 link does not resolve:\n  ")
	assert.Contains(t, err.Error(), `"Nowhere"`)
	assert.Equal(t, runErr.Error(), err.Error())
}

// TestRunStopsBetweenFiles: Run is an exported entry point, so a library
// caller can be publishing hundreds of documents with no way to ask it to stop.
// A run cancelled before it starts publishes nothing at all.
func TestRunStopsBetweenFiles(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("First"))
	writeFile(t, dir, "b.md", markdownWithTitle("Second"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Run(ctx, publishConfig(server.URL, filepath.Join(dir, "*.md")))

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, countPagesTitled(t, server, "First"),
		"a run cancelled before it starts publishes nothing")
}

// TestRunPublishesWhenNotCancelled is the control.
func TestRunPublishesWhenNotCancelled(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Published"))

	require.NoError(t, Run(context.Background(), publishConfig(server.URL, file)))
	assert.Equal(t, 1, countPagesTitled(t, server, "Published"))
}

// TestPublisherIsCancellable covers the library's entry point, which a
// caller loops over itself.
func TestPublisherIsCancellable(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md", markdownWithTitle("Not Published"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := publishOne(ctx, file, api, publishConfig(server.URL, file).PublishConfig)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, countPagesTitled(t, server, "Not Published"))
}

// TestRunStopsARunBlockedOnASlowRequest: cancellation reaches the
// request in flight. The fake holds the second file's first lookup until the
// client gives up on it, as a Confluence that has stopped answering would, and
// cancelling the run's context ends the run there rather than after the
// two-minute response-header timeout. The page that lookup was for is never
// created, the one before it stands, and the page manifest is still saved on
// the way out, under a context of its own: the second run proves it by finding
// the first page again after its title changed.
func TestRunStopsARunBlockedOnASlowRequest(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("First"))
	writeFile(t, dir, "b.md", markdownWithTitle("Second"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	blocked := make(chan struct{})
	var once sync.Once
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Query().Get("title") != "Second" {
			return 0, "", false
		}
		once.Do(func() { close(blocked) })
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
		return http.StatusServiceUnavailable, `{"message":"too late"}`, true
	})

	go func() {
		<-blocked
		cancel()
	}()

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.Tracking.TrackPages = true

	started := time.Now()
	err := Run(ctx, config)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(started), 10*time.Second, "the blocked request was not cut short")

	server.SetFail(nil)
	assert.Equal(t, 0, countPagesTitled(t, server, "Second"),
		"the file being published when the run was cancelled did not finish")
	first := findPageTitled(t, server, "First")

	writeFile(t, dir, "a.md", markdownWithTitle("First Renamed"))
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, first.ID, findPageTitled(t, server, "First Renamed").ID,
		"the manifest saved by the cancelled run found the page it made")
}

// TestContinueOnErrorDoesNotSwallowACancellation: with --continue-on-error a
// failed file is logged and the run moves on, but a file that failed because
// the run was cancelled is not a file failure. Cancelled while the last file
// publishes, the run reports that it was stopped, not that some files
// failed.
func TestContinueOnErrorDoesNotSwallowACancellation(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("First"))
	writeFile(t, dir, "b.md", markdownWithTitle("Second"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	blocked := make(chan struct{})
	var once sync.Once
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Query().Get("title") != "Second" {
			return 0, "", false
		}
		once.Do(func() { close(blocked) })
		<-r.Context().Done()
		return http.StatusServiceUnavailable, `{"message":"too late"}`, true
	})

	go func() {
		<-blocked
		cancel()
	}()

	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.ContinueOnError = true

	err := Run(ctx, config)

	require.ErrorIs(t, err, context.Canceled)
}

// findPageTitled looks a page up with a client of its own, for the reason
// countPagesTitled gives.
func findPageTitled(t *testing.T, server *confluencetest.Server, title string) *confluence.PageInfo {
	t.Helper()

	page, err := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"}).FindPage(context.Background(), "DOCS", title, "page")
	require.NoError(t, err)
	require.NotNil(t, page, "no page titled %q", title)

	return page
}

// TestACancelledExplicitManifestSaveIsNotReportedOnceSaved: under
// --continue-on-error a run cancelled while its last file publishes still
// reaches the explicit save at the end. Made under the run's own context, that
// save would be refused before it was sent and go into the report and the
// error, while the save on the way out wrote the manifest under a detached
// context -- so the report would say a manifest that had been saved was not.
func TestACancelledExplicitManifestSaveIsNotReportedOnceSaved(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("First"))
	writeFile(t, dir, "b.md", markdownWithTitle("Second"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The last file's first lookup cancels the run and is held until the
	// client gives up on it, so that file fails on the cancellation and the
	// run goes on to the end, as --continue-on-error has it do.
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Query().Get("title") != "Second" {
			return 0, "", false
		}
		cancel()
		<-r.Context().Done()
		return http.StatusServiceUnavailable, `{"message":"too late"}`, true
	})

	var out bytes.Buffer
	config := publishConfig(server.URL, filepath.Join(dir, "*.md"))
	config.Tracking.TrackPages = true
	config.ContinueOnError = true
	config.Report.OutputFormat = report.FormatJSON
	config.Output = &out

	err := Run(ctx, config)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "unable to save page manifest")

	var parsed report.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	for _, line := range parsed.Errors {
		assert.NotContains(t, line, "unable to save page manifest", out.String())
	}

	// And it was saved: the next run finds the first page after its title
	// changed.
	server.SetFail(nil)
	first := findPageTitled(t, server, "First")

	writeFile(t, dir, "a.md", markdownWithTitle("First Renamed"))
	config.ContinueOnError = false
	config.Report.OutputFormat = ""
	config.Output = io.Discard
	require.NoError(t, Run(context.Background(), config))

	assert.Equal(t, first.ID, findPageTitled(t, server, "First Renamed").ID,
		"the manifest saved by the cancelled run found the page it made")
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
	config.Tracking.TrackPages = false
	config.Page.PageID = target.ID

	err := Run(context.Background(), config)
	require.ErrorContains(t, err, "matched 2 files")
	assert.Equal(t, 0, server.CountRequests(http.MethodPut, "/content/"+target.ID), "nothing is published")

	config.Files = filepath.Join(dir, "a.md")
	require.NoError(t, Run(context.Background(), config))
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
				PublishConfig: PublishConfig{
					Mode: ModeConfig{
						CompileOnly: true,
					},
					Rendering: RenderingConfig{
						Features: []string{"mention"},
					},
					Output: io.Discard,
				},
				Files: filepath.Join(dir, "*.md"),
				Connection: ConnectionConfig{
					BaseURL: "http://127.0.0.1:1",
				},
			}

			b.ReportAllocs()
			for b.Loop() {
				if err := Run(context.Background(), config); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A Publisher keeps a run's state across files, but each file's links are its
// own: a link the first file left unresolved is not the second's failure.
func TestPublisherChecksEachFilesLinks(t *testing.T) {
	server, api, first, config := publisherFixture(t, "[x](ac:Missing)\n")
	config.Links.CheckLinks = []string{"confluence"}
	second := writeFile(t, filepath.Dir(first), "other.md", "<!-- Space: DOCS -->\n<!-- Title: Other -->\n\nBody.\n")

	publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.Publish(context.Background(), first)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing")

	target, err := publisher.Publish(context.Background(), second)
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.NotNil(t, server.Page(target.ID))
}

// A Publisher's templates and page header outlive the context it was made
// under: a user is looked up under the context Publish was given.
func TestPublisherLooksUsersUpUnderPublishContext(t *testing.T) {
	server, api, file, config := publisherFixture(t, "Ask @{Alice Example}.\n")
	server.AddUser(confluencetest.User{AccountID: "acct-alice", FullName: "Alice Example"})
	// Named by the document's title, so that the header's checks at load time
	// do not look it up and leave it cached.
	server.AddUser(confluencetest.User{AccountID: "acct-doc", FullName: "Doc"})
	config.Rendering.PageHeader = writeFile(t, t.TempDir(), "header.html",
		`<p>{{ with user .Title }}{{ .AccountID }}{{ end }}</p>`)

	setup, cancel := context.WithCancel(context.Background())

	publisher, err := NewPublisher(setup, api, config.PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	cancel()

	target, err := publisher.Publish(context.Background(), file)
	require.NoError(t, err)
	require.NotNil(t, target)
	body := server.Page(target.ID).Body
	assert.Contains(t, body, `ri:account-id="acct-alice"`)
	assert.Contains(t, body, `<p>acct-doc</p>`)
}

// The merman engine's "experimental" warning is said once a publisher, not
// once for each time its name is checked.
func TestPublisherWarnsOfMermanOnce(t *testing.T) {
	_, api, _, config := publisherFixture(t, "Body.\n")
	config.Rendering.MermaidEngine = mermaid.EngineMerman

	var err error
	logged := captureLog(t, func() {
		var publisher *Publisher
		publisher, err = NewPublisher(context.Background(), api, config.PublishConfig)
		publisher.Close()
	})

	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(logged, "experimental"))
}

// A Publisher that failed to be made is nil, and closing it is harmless, so
// that a deferred Close cannot hide the error.
func TestNilPublisherCloses(t *testing.T) {
	var publisher *Publisher

	assert.NotPanics(t, publisher.Close)
}

// A Publisher NewPublisher did not make, nil or zero, refuses to publish
// rather than panicking, and closing it is harmless.
func TestUnmadePublisherRefuses(t *testing.T) {
	_, _, file, _ := publisherFixture(t, "Body.\n")

	for name, publisher := range map[string]*Publisher{"nil": nil, "zero": {}} {
		t.Run(name, func(t *testing.T) {
			target, err := publisher.Publish(context.Background(), file)
			require.ErrorIs(t, err, errNoPublisher)
			assert.Nil(t, target)
			assert.NotPanics(t, publisher.Close)
		})
	}
}

// A closed Publisher refuses a file before Confluence is asked anything.
func TestClosedPublisherRefuses(t *testing.T) {
	server, api, file, config := publisherFixture(t, "Body.\n")

	publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
	require.NoError(t, err)
	publisher.Close()
	publisher.Close()

	server.ResetRequests()

	target, err := publisher.Publish(context.Background(), file)
	require.ErrorIs(t, err, errNoPublisher)
	assert.Nil(t, target)
	assert.Empty(t, server.Requests())
}

// TestPublisherKeepsFoldersAcrossFiles: a Publisher remembers the folders it
// resolved, so a second file in the same folder does not look it up again,
// where a Publisher made afresh for each file does.
func TestPublisherKeepsFoldersAcrossFiles(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	first := writeFile(t, dir, "a.md", markdownInFolder("Manuals", "A"))
	second := writeFile(t, dir, "b.md", markdownInFolder("Manuals", "B"))
	third := writeFile(t, dir, "c.md", markdownInFolder("Manuals", "C"))
	config := publishConfig(server.URL, first)

	publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.Publish(context.Background(), first)
	require.NoError(t, err)
	require.Len(t, server.Folders(), 1)

	// A folder is found by listing what is beneath its parent.
	const lookup = "/direct-children"

	server.ResetRequests()
	_, err = publisher.Publish(context.Background(), second)
	require.NoError(t, err)
	assert.Zero(t, server.CountRequests("GET", lookup), "the Publisher does not look the folder up again")

	server.ResetRequests()
	_, err = publishOne(context.Background(), third, api, config.PublishConfig)
	require.NoError(t, err)
	assert.NotZero(t, server.CountRequests("GET", lookup), "a new Publisher starts without the folder")

	assert.Len(t, server.Folders(), 1, "every file lands in the one folder")
}

// TestPublisherPublishesOneFileToAPageID: with Page.PageID every file would
// publish over the one page, so a Publisher refuses a second file, as Run
// does, and still takes the first one again.
func TestPublisherPublishesOneFileToAPageID(t *testing.T) {
	server, api, first, config := publisherFixture(t, "First.\n")
	target := server.AddPage("DOCS", "Target", "page", "")
	config.Page.PageID = target.ID
	second := writeFile(t, filepath.Dir(first), "other.md", "<!-- Space: DOCS -->\n<!-- Title: Other -->\n\nSecond.\n")

	publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.Publish(context.Background(), first)
	require.NoError(t, err)
	published := server.Page(target.ID).Body

	_, err = publisher.Publish(context.Background(), second)
	require.ErrorContains(t, err, "Page.PageID names one page")
	assert.Equal(t, published, server.Page(target.ID).Body, "the page keeps the first file")

	_, err = publisher.Publish(context.Background(), first)
	require.NoError(t, err, "publishing the same file again is not a second file")

	abs, err := filepath.Abs(first)
	require.NoError(t, err)

	_, err = publisher.Publish(context.Background(), abs)
	require.NoError(t, err, "the same file spelled absolutely is not a second file")
}

// TestPublisherLeavesAPageIDToAFileThatReachesIt: a file that fails before
// the page is reached has published nothing there, so the next file may.
func TestPublisherLeavesAPageIDToAFileThatReachesIt(t *testing.T) {
	server, api, first, config := publisherFixture(t, "First.\n")
	target := server.AddPage("DOCS", "Target", "page", "")
	config.Page.PageID = target.ID

	publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
	require.NoError(t, err)
	defer publisher.Close()

	_, err = publisher.Publish(context.Background(), first+".typo")
	require.Error(t, err)

	_, err = publisher.Publish(context.Background(), first)
	require.NoError(t, err)
	assert.Contains(t, server.Page(target.ID).Body, "First.")
}

// TestPublisherKeepsAPageIDForAFileThatReachedIt: a file that fails once the
// page is found may have put something of itself on it, so the page stays
// its own; a dry run refuses the next file just as a real run would.
func TestPublisherKeepsAPageIDForAFileThatReachedIt(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry run %v", dryRun), func(t *testing.T) {
			server, api, first, config := publisherFixture(t, "<!-- Include: missing.md -->\n")
			target := server.AddPage("DOCS", "Target", "page", "")
			config.Page.PageID = target.ID
			config.Mode.DryRun = dryRun
			second := writeFile(t, filepath.Dir(first), "other.md", "<!-- Space: DOCS -->\n<!-- Title: Other -->\n\nSecond.\n")

			publisher, err := NewPublisher(context.Background(), api, config.PublishConfig)
			require.NoError(t, err)
			defer publisher.Close()

			_, err = publisher.Publish(context.Background(), first)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "Page.PageID")

			_, err = publisher.Publish(context.Background(), second)
			require.ErrorContains(t, err, "Page.PageID names one page")
		})
	}
}
