package page

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
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

	_, why := findLinkTarget([]string{base}, "example")
	require.NotNil(t, why)
	assert.Contains(t, why.reason, "directory")

	_, why = findLinkTarget([]string{base}, "no-such-thing")
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

	found, why := findLinkTarget([]string{first, second}, "shared")
	require.Nil(t, why, "the file in the second directory is the answer")
	assert.Equal(t, document, found)
}

func TestLinkResolverIgnoresNonTextFiles(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	// A PNG header is enough for http.DetectContentType.
	require.NoError(t, os.WriteFile(
		filepath.Join(base, "image.png"),
		[]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"),
		0o644,
	))

	resolver := &LinkResolver{API: &confluence.API{}, Base: base}

	resolved, err := resolver.Resolve(ctx, "image.png", "")
	assert.NoError(t, err)
	assert.Empty(t, resolved)
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
