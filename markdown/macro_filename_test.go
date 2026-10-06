package mark

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMacroFileNameReadsAngleBracketDestination: "<my file.png>" names the
// file the same way an image destination does, brackets and all taken off.
func TestMacroFileNameReadsAngleBracketDestination(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my file.png"), []byte("x"), 0o600))

	assert.Equal(t, "my file.png", macroFileName(dir)("<my file.png>"))
}

// TestMacroFileNameDoesNotProbeOutsideTheProject: a name that reaches outside
// both the document's directory and the working directory comes back as it was
// written, whether or not a file is there, so the answer is the same either way.
func TestMacroFileNameDoesNotProbeOutsideTheProject(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.png"), []byte("x"), 0o600))
	t.Chdir(dir)

	fileName := macroFileName(dir)

	assert.Equal(t, "../secret.png", fileName("../secret.png"))
	assert.Equal(t, "../missing.png", fileName("../missing.png"))
}
