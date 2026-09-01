package mark

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
