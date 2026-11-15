package page

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A page's space can come from --space, so a Title alone is enough then.
func TestLinkResolverLooksUpNonMarkdownFilesWithATitleAndTheRunsSpace(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "only-title.rst"),
		[]byte("<!-- Title: Page -->\n\nBody.\n"), 0o600))

	api, requests := countingAPI(t)
	resolver := &LinkResolver{API: api, Base: base, Extract: metadata.ExtractOptions{Space: "DOCS"}}

	_, err := resolver.Resolve(t.Context(), "only-title.rst", "")
	require.NoError(t, err)
	assert.NotZero(t, atomic.LoadInt64(requests))
}

// A title derived from the file does not make a script a page, with a space.
func TestDeclaresPageIgnoresDerivedTitles(t *testing.T) {
	name := filepath.Join(t.TempDir(), "run.sh")
	require.NoError(t, os.WriteFile(name, []byte("# Run\necho hi\n"), 0o600))

	declared, err := DeclaresPage(attachment.Root{}, name, metadata.ExtractOptions{
		Space: "DOCS", TitleFromFilename: true, TitleFromH1: true,
	})
	require.NoError(t, err)
	assert.False(t, declared)
}

// readCounter counts the bytes DeclaresPage asks a file for.
type readCounter struct {
	attachment.Root
	read int
}

func (c *readCounter) ReadHead(name string, n int) ([]byte, error) {
	head, err := c.Root.ReadHead(name, n)
	c.read += len(head)

	return head, err
}

// A large file is not read whole, a binary one is no page, and a page that
// declares itself up front is found whatever follows.
func TestDeclaresPageReadsOnlyTheStartOfAFile(t *testing.T) {
	base := t.TempDir()

	big := filepath.Join(base, "big.log")
	require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte("line of log output\n"), 1<<20), 0o600))

	counting := &readCounter{Root: attachment.Root{}}
	declared, err := DeclaresPage(counting, big, metadata.ExtractOptions{Space: "DOCS"})
	require.NoError(t, err)
	assert.False(t, declared)
	assert.LessOrEqual(t, counting.read, headSize)

	pdf := filepath.Join(base, "x.pdf")
	require.NoError(t, os.WriteFile(pdf,
		[]byte("%PDF-1.4\n\x00\x01<!-- Space: A -->\n<!-- Title: B -->\n"), 0o600))
	declared, err = DeclaresPage(attachment.Root{}, pdf, metadata.ExtractOptions{})
	require.NoError(t, err)
	assert.False(t, declared, "a binary file is no page whatever it contains")

	rst := filepath.Join(base, "p.rst")
	require.NoError(t, os.WriteFile(rst,
		[]byte("<!-- Space: A -->\n<!-- Title: B -->\n"+strings.Repeat("x\n", 1<<20)), 0o600))
	declared, err = DeclaresPage(attachment.Root{}, rst, metadata.ExtractOptions{})
	require.NoError(t, err)
	assert.True(t, declared)
}

// --check-links does not report an existing script or binary as a broken
// link: it is left as written and is not a page. A missing file still is.
func TestLinkCheckPassesLinksToNonMarkdownFiles(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "run.sh"), []byte("echo hi\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "x.pdf"), []byte("%PDF-1.4\n\x00\x01"), 0o600))

	api, requests := countingAPI(t)
	resolver := &LinkResolver{
		API: api, Base: base, Checker: NewLinkChecker(LinkChecks{Internal: true}),
	}

	for _, target := range []string{"run.sh", "x.pdf"} {
		_, err := resolver.Resolve(t.Context(), target, "")
		require.NoError(t, err)
	}

	assert.Empty(t, resolver.Broken())
	assert.Zero(t, atomic.LoadInt64(requests))

	_, err := resolver.Resolve(t.Context(), "missing.sh", "")
	require.NoError(t, err)
	assert.Len(t, resolver.Broken(), 1)
}
