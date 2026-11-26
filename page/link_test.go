package page

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkResolverLeavesNonRepositoryTargetsAlone(t *testing.T) {
	ctx := t.Context()
	// A resolver with an API set but nothing on disk to point at: every target
	// here has to come back empty, meaning "leave the link as written".
	resolver := &LinkResolver{API: &confluence.API{}, Base: t.TempDir()}

	for _, target := range []string{
		"https://example.com/page",
		"http://example.com/page",
		"ftp://example.com/file.txt",
		"mailto:someone@example.com",
		"#heading-in-document",
		"",
		"no-such-file.md",
		"no-such-file.md#hash",
		"../outside/missing.md",
	} {
		resolved, err := resolver.Resolve(ctx, target, "")
		assert.NoError(t, err, "target %q", target)
		assert.Empty(t, resolved, "target %q should have been left alone", target)
	}
}

func TestLinkResolverIgnoresDirectories(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "docs"), 0o755))

	resolver := &LinkResolver{API: &confluence.API{}, Base: base}

	resolved, err := resolver.Resolve(ctx, "docs", "")
	assert.NoError(t, err)
	assert.Empty(t, resolved)
}

// TestLinkResolverLeavesRootedTargetsAlone covers a link written from the root:
// [/example](/example).
//
// A rooted path is not a path in this repository. It is either somewhere on the
// site the pages are published to, or one an attachment has already claimed,
// and neither is mark's to follow -- so the link is left exactly as written
// without anything on disk being consulted at all.
//
// Which is why a directory is no different from a file here, and both are no
// different from nothing: the resolver never looks. Written down because the
// obvious reading of "resolve a link" is that it goes and finds something.
func TestLinkResolverLeavesRootedTargetsAlone(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()

	// Everything a rooted target could name, sitting in the base directory so
	// that a resolver which did look would find it and do something.
	require.NoError(t, os.Mkdir(filepath.Join(base, "example"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "page.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

	resolver := &LinkResolver{API: &confluence.API{}, Base: base}

	for _, target := range []string{
		"/example",
		"/example/",
		"/page.md",
		"/nothing-here",
		"/example#a-heading",
	} {
		resolved, err := resolver.Resolve(ctx, target, "")
		assert.NoError(t, err, "target %q", target)
		assert.Empty(t, resolved, "target %q should have been left as written", target)
	}
}

// TestFindLinkTargetSaysWhichKindOfNothingItFound covers the two ways a
// relative link can fail to name a document, which are worth telling apart:
// --check-links reports the reason, and "there is no such file" would send
// somebody looking for a typo in a path that is perfectly correct.
func TestFindLinkTargetSaysWhichKindOfNothingItFound(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "example"), 0o755))

	_, why := findLinkTarget(context.Background(), attachment.Root{}, []string{base}, "example")
	require.NotNil(t, why)
	assert.Contains(t, why.reason, "directory")

	_, why = findLinkTarget(context.Background(), attachment.Root{}, []string{base}, "no-such-thing")
	require.NotNil(t, why)
	assert.Contains(t, why.reason, "no such file")
}

// TestFindLinkTargetPrefersAFileInALaterSearchDirectory covers a directory that
// shares its name with a document somewhere else on the search path.
//
// A directory is not an answer, so the search goes on rather than stopping at
// it: --include-path exists so that a name can be found somewhere other than
// beside the document, and a directory of that name in the first place looked
// would otherwise hide the file in the second.
func TestFindLinkTargetPrefersAFileInALaterSearchDirectory(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(first, "shared"), 0o755))

	document := filepath.Join(second, "shared")
	require.NoError(t, os.WriteFile(document, []byte("Body.\n"), 0o600))

	found, why := findLinkTarget(context.Background(), attachment.Root{}, []string{first, second}, "shared")
	require.Nil(t, why, "the file in the second directory is the answer")
	assert.Equal(t, document, found)
}

// A file that reads as a document by its name and is not text is reported by
// what it holds.
func TestLinkResolverIgnoresNonTextFiles(t *testing.T) {
	for _, name := range []string{"image.md", "image"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			// A PNG header is enough for http.DetectContentType.
			require.NoError(t, os.WriteFile(
				filepath.Join(base, name),
				[]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"),
				0o644,
			))

			api, requests := countingAPI(t)
			resolver := &LinkResolver{
				API:     api,
				Base:    base,
				Extract: metadata.ExtractOptions{Space: "DOCS", TitleFromFilename: true},
				Checker: NewLinkChecker(LinkChecks{Internal: true}),
			}

			resolved, err := resolver.Resolve(t.Context(), name, "")
			assert.NoError(t, err)
			assert.Empty(t, resolved)
			assert.Zero(t, atomic.LoadInt64(requests), "no page lookup for a file that is not text")
			require.Len(t, resolver.Broken(), 1)
			assert.Contains(t, resolver.Broken()[0], "it is not a text file (image/png)")
		})
	}
}

func TestLinkResolverIgnoresFilesWithoutMetadata(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(base, "plain.md"),
		[]byte("# Just a heading\n\nNo mark metadata here.\n"),
		0o644,
	))

	resolver := &LinkResolver{API: &confluence.API{}, Base: base}

	// Without metadata there is no space or title to look a page up by, so the
	// link stays as it is rather than the run failing.
	resolved, err := resolver.Resolve(ctx, "plain.md", "")
	assert.NoError(t, err)
	assert.Empty(t, resolved)
}

func TestLinkResolverWithoutAPIDoesNothing(t *testing.T) {
	ctx := t.Context()
	var resolver *LinkResolver

	resolved, err := resolver.Resolve(ctx, "anything.md", "")
	assert.NoError(t, err)
	assert.Empty(t, resolved)
}

// TestHeadingAnchorReadsTheAttributeBlock: the ids another document's headings
// are matched against are the ones its page gets, attribute blocks included,
// so a link to api.md#custom finds "## Other {#custom}".
func TestHeadingAnchorReadsTheAttributeBlock(t *testing.T) {
	document := []byte("## Other {#custom}\n\n## GET /users/{id}\n")

	assert.Equal(t, "custom", headingAnchor(document, "custom"))
	assert.Equal(t, "GET-/users/id", headingAnchor(document, "get-users-id"))
}

func countingAPI(t *testing.T) (*confluence.API, *int64) {
	t.Helper()

	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"size":0}`))
	}))
	t.Cleanup(server.Close)

	return confluence.NewAPI(server.URL, confluence.Options{}), &requests
}

// A link to an existing script is valid, so it is left as written and never
// looked up as a page, however the title would be derived from its content.
func TestLinkResolverSkipsNonMarkdownTextFiles(t *testing.T) {
	cases := []struct {
		name    string
		content string
		extract metadata.ExtractOptions
	}{
		{"run.sh", "#!/bin/sh\necho hi\n", metadata.ExtractOptions{TitleFromFilename: true}},
		{"build.sh", "# build the thing\necho hi\n", metadata.ExtractOptions{TitleFromH1: true}},
		{"notes.TXT", "plain words\n", metadata.ExtractOptions{TitleFromFilename: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(base, tc.name), []byte(tc.content), 0o600))

			api, requests := countingAPI(t)
			tc.extract.Space = "DOCS"
			checker := NewLinkChecker(LinkChecks{Internal: true})
			resolver := &LinkResolver{API: api, Base: base, Extract: tc.extract, Checker: checker}

			resolved, err := resolver.Resolve(t.Context(), tc.name, "")
			require.NoError(t, err)
			assert.Empty(t, resolved)
			assert.Empty(t, resolver.Broken(), "an existing file is not a broken link")
			assert.Zero(t, atomic.LoadInt64(requests), "no page lookup for a non-Markdown file")
		})
	}
}

// pageAPI is a Confluence holding the page "Page" in DOCS, and the link a
// document that publishes as it is rewritten to.
func pageAPI(t *testing.T) (*confluence.API, string) {
	t.Helper()

	server := confluencetest.New(t)
	server.AddPage("DOCS", "Page", "page", "")
	api := confluence.NewAPI(server.URL, confluence.Options{Username: "user", Password: "token"})

	link, err := getConfluenceLink(t.Context(), api, "DOCS", "Page")
	require.NoError(t, err)
	require.NotEmpty(t, link)

	return api, link
}

func TestLinkResolverStillResolvesMarkdownAndExtensionlessFiles(t *testing.T) {
	for _, name := range []string{"page.md", "page.MARKDOWN", "page"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(base, name),
				[]byte("<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"), 0o600))

			api, link := pageAPI(t)
			resolver := &LinkResolver{API: api, Base: base}

			resolved, err := resolver.Resolve(t.Context(), name, "")
			require.NoError(t, err)
			assert.Equal(t, link, resolved)
		})
	}
}

// A file published through --files with its own space and title is a page
// whatever its extension, so a link to it is rewritten like any other. A dot
// in a name is not always an extension: v1.2 and README.en are documents too.
func TestLinkResolverLooksUpNonMarkdownFilesThatDeclareAPage(t *testing.T) {
	const headers = "<!-- Space: DOCS -->\n<!-- Title: Page -->\n\nBody.\n"

	cases := []struct{ name, content string }{
		{"page.rst", headers},
		{"notes.TXT", headers},
		{"guide.mdx", "---\nspace: DOCS\ntitle: Page\n---\n\nBody.\n"},
		{"v1.2", headers},
		{"README.en", headers},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(base, tc.name), []byte(tc.content), 0o600))

			api, link := pageAPI(t)
			resolver := &LinkResolver{
				API:     api,
				Base:    base,
				Extract: metadata.ExtractOptions{FrontMatter: true},
			}

			resolved, err := resolver.Resolve(t.Context(), tc.name, "")
			require.NoError(t, err)
			assert.Equal(t, link, resolved)
		})
	}
}

// A linked script the run cannot read is left as written, like one it can:
// it is not a document either way.
func TestLinkResolverLeavesUnreadableNonMarkdownFilesAlone(t *testing.T) {
	base := t.TempDir()
	name := filepath.Join(base, "run.sh")
	require.NoError(t, os.WriteFile(name, []byte("echo hi\n"), 0o000))
	if _, err := os.ReadFile(name); err == nil {
		t.Skip("file modes do not keep this user from reading")
	}

	api, requests := countingAPI(t)
	resolver := &LinkResolver{
		API:     api,
		Base:    base,
		Extract: metadata.ExtractOptions{Space: "DOCS", TitleFromFilename: true},
		Checker: NewLinkChecker(LinkChecks{Internal: true}),
	}

	resolved, err := resolver.Resolve(t.Context(), "run.sh", "")
	require.NoError(t, err)
	assert.Empty(t, resolved)
	assert.Empty(t, resolver.Broken())
	assert.Zero(t, atomic.LoadInt64(requests))
}

// A space or title borrowed from the run does not make a script a page.
func TestLinkResolverSkipsNonMarkdownFilesWithOnlyHalfAPage(t *testing.T) {
	for name, content := range map[string]string{
		"only-space.rst": "<!-- Space: DOCS -->\n\nBody.\n",
		"only-title.rst": "<!-- Title: Page -->\n\nBody.\n",
	} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(base, name), []byte(content), 0o600))

			api, requests := countingAPI(t)
			resolver := &LinkResolver{API: api, Base: base}

			resolved, err := resolver.Resolve(t.Context(), name, "")
			require.NoError(t, err)
			assert.Empty(t, resolved)
			assert.Zero(t, atomic.LoadInt64(requests))
		})
	}
}
