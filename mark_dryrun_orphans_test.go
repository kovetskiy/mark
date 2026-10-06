package mark

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDryRunDoesNotReportPublishedPagesAsOrphans: a dry run over documents that
// are all still present, after a tracked publish, must not read them as gone.
// The dry run found each page by title and returned before recording it, so
// every tracked path looked unseen - and with --on-orphan delete it then set
// about removing the whole set.
func TestDryRunDoesNotReportPublishedPagesAsOrphans(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()

	writeFile(t, dir, "a.md", markdownWithTitle("A"))
	writeFile(t, dir, "b.md", markdownWithTitle("B"))
	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.OnOrphan = "delete"
	require.NoError(t, Run(config))

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	dry := config
	dry.DryRun = true
	require.NoError(t, Run(dry))

	assert.NotContains(t, logged.String(), "had no matching source file",
		"every document is still there")
	assert.NotContains(t, logged.String(), "would be deleted")
}
