package mark

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stagedTree is a source tree with a document and the guide it links to, and a
// staging directory holding a copy of the document alone, as a caller that
// renders its documents before publishing them leaves things.
func stagedTree(t *testing.T) (staged, source, document string) {
	t.Helper()

	source = t.TempDir()
	staged = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "team"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(staged, "team"), 0o755))

	body := "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\nSee [the guide](./guide.md).\n"
	writeFile(t, filepath.Join(source, "team"), "doc.md", body)
	writeFile(t, filepath.Join(source, "team"), "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nGuide.\n")
	document = writeFile(t, filepath.Join(staged, "team"), "doc.md", body)

	return staged, source, document
}

// TestLinksResolveAgainstTheSourceTree: a staged document's relative link names
// a file beside its source, not beside the copy, and is checked and rewritten
// as the source tree answers it.
func TestLinksResolveAgainstTheSourceTree(t *testing.T) {
	staged, source, document := stagedTree(t)

	t.Run("without the roots the staged copy answers", func(t *testing.T) {
		_, api, _, config := publisherFixture(t, "")
		config.Links.CheckLinks = []string{"internal"}

		_, err := publishOne(context.Background(), document, api, config.PublishConfig)
		require.ErrorContains(t, err, "there is no such file")
	})

	t.Run("with them the source tree does", func(t *testing.T) {
		server, api, _, config := publisherFixture(t, "")
		server.AddPage("DOCS", "Guide", "page", "")
		config.Links = LinkConfig{CheckLinks: []string{"internal"}, StagedRoot: staged, SourceRoot: source}

		target, err := publishOne(context.Background(), document, api, config.PublishConfig)
		require.NoError(t, err)
		require.NotNil(t, target)

		body := server.Page(target.ID).Body
		assert.Contains(t, body, "/x/", "the link is rewritten to the guide's page")
		assert.NotContains(t, body, "./guide.md")
	})

	t.Run("and Run does the same", func(t *testing.T) {
		server, _, _, config := publisherFixture(t, "")
		server.AddPage("DOCS", "Guide", "page", "")
		config.Files = document
		config.Links = LinkConfig{CheckLinks: []string{"all"}, StagedRoot: staged, SourceRoot: source}

		require.NoError(t, Run(context.Background(), config))
	})
}

// TestTheStagedCopyComesFirst: where both trees hold the file a link names, the
// staged one is what publishes, so it is the one the link is rewritten from.
func TestTheStagedCopyComesFirst(t *testing.T) {
	staged, source, document := stagedTree(t)
	writeFile(t, filepath.Join(staged, "team"), "guide.md", "<!-- Space: DOCS -->\n<!-- Title: Staged Guide -->\n\nGuide.\n")

	server, api, _, config := publisherFixture(t, "")
	// Only the staged copy's page exists: were the source's read, the link
	// would wait for "Guide" and be left as written.
	server.AddPage("DOCS", "Staged Guide", "page", "")
	config.Links = LinkConfig{CheckLinks: []string{"internal"}, StagedRoot: staged, SourceRoot: source}

	target, err := publishOne(context.Background(), document, api, config.PublishConfig)
	require.NoError(t, err)
	assert.Contains(t, server.Page(target.ID).Body, "/x/")
}

// TestADocumentOutsideTheStagedRootFails: with the roots set, a document they
// do not map would have its links answered by wherever it sits, which is what
// they are set to stop.
func TestADocumentOutsideTheStagedRootFails(t *testing.T) {
	staged, source, _ := stagedTree(t)

	server, api, file, config := publisherFixture(t, "Body.\n")
	config.Links = LinkConfig{StagedRoot: staged, SourceRoot: source}

	_, err := publishOne(context.Background(), file, api, config.PublishConfig)

	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
	var linkErr *LinkConfigError
	require.ErrorAs(t, err, &linkErr)
	assert.Equal(t, "Links.StagedRoot", linkErr.Field)
	assert.Equal(t, "--links-staged-root", linkErr.Flag)
	assert.Contains(t, err.Error(), file)

	assert.Empty(t, pageIDTitled(t, server, "Doc"), "nothing is published")
}

// TestLinkConfigIsRefusedUpFront covers the link settings Run, NewPublisher
// and PublishConfig.Validate refuse before anything is published, each as a
// *LinkConfigError naming the field and its flag.
func TestLinkConfigIsRefusedUpFront(t *testing.T) {
	dir := t.TempDir()
	notADir := writeFile(t, dir, "file.md", "x")

	cases := map[string]struct {
		links LinkConfig
		field string
		flag  string
	}{
		"warn-only with nothing checked": {
			LinkConfig{CheckLinksWarnOnly: true}, "Links.CheckLinksWarnOnly", "--check-links-warn-only",
		},
		"warn-only with only empty kinds": {
			LinkConfig{CheckLinks: []string{"", " , "}, CheckLinksWarnOnly: true},
			"Links.CheckLinksWarnOnly", "--check-links-warn-only",
		},
		"an unknown kind": {
			LinkConfig{CheckLinks: []string{"none"}}, "Links.CheckLinks", "--check-links",
		},
		"a staged root alone": {
			LinkConfig{StagedRoot: dir}, "Links.StagedRoot", "--links-staged-root",
		},
		"a source root alone": {
			LinkConfig{SourceRoot: dir}, "Links.SourceRoot", "--links-source-root",
		},
		"a source root that is missing": {
			LinkConfig{StagedRoot: dir, SourceRoot: filepath.Join(dir, "missing")}, "Links.SourceRoot", "--links-source-root",
		},
		"a staged root that is a file": {
			LinkConfig{StagedRoot: notADir, SourceRoot: dir}, "Links.StagedRoot", "--links-staged-root",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			config := PublishConfig{Links: tc.links}

			err := config.Validate()
			var linkErr *LinkConfigError
			require.ErrorAs(t, err, &linkErr)
			assert.Equal(t, tc.field, linkErr.Field)
			assert.Equal(t, tc.flag, linkErr.Flag)
			assert.Contains(t, err.Error(), tc.field+" ("+tc.flag+")")

			server, api, _, full := publisherFixture(t, "Body.\n")
			full.Links = tc.links

			runErr := Run(context.Background(), full)
			assert.Equal(t, err.Error(), runErr.Error())

			publisher, pubErr := NewPublisher(context.Background(), api, full.PublishConfig)
			assert.Nil(t, publisher)
			assert.Equal(t, err.Error(), pubErr.Error())
			assert.Empty(t, server.Requests(), "refused before Confluence is asked anything")
		})
	}

	t.Run("a missing root keeps its cause", func(t *testing.T) {
		err := PublishConfig{Links: LinkConfig{StagedRoot: dir, SourceRoot: filepath.Join(dir, "missing")}}.Validate()
		assert.True(t, errors.Is(err, os.ErrNotExist))
	})
}

// TestReadOnlyRunsCountTheirOwnPages: an ac: link to a page the run publishes
// is looked up once the run is over. A real run has created it by then; a dry
// run or a compile never does, and must not report the link as missing for it.
func TestReadOnlyRunsCountTheirOwnPages(t *testing.T) {
	modes := map[string]ModeConfig{
		"dry run":      {DryRun: true},
		"compile only": {CompileOnly: true},
	}

	for name, mode := range modes {
		t.Run(name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			writeFile(t, dir, "a.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: A -->\n\nSee [b](ac:B) and [c](ac:Nowhere).\n")
			writeFile(t, dir, "b.md", "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: B -->\n\nB.\n")

			err := Run(context.Background(), Config{
				PublishConfig: PublishConfig{
					Mode:      mode,
					Rendering: RenderingConfig{Features: []string{"mention"}},
					Links:     LinkConfig{CheckLinks: []string{"confluence"}},
					Output:    io.Discard,
				},
				Files:      filepath.Join(dir, "*.md"),
				Connection: ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
			})
			require.Error(t, err, "a page nobody publishes is still missing")
			assert.Contains(t, err.Error(), `"Nowhere"`)
			assert.NotContains(t, err.Error(), `page "B"`)
			assert.Contains(t, err.Error(), "1 link does not resolve")
		})
	}
}
