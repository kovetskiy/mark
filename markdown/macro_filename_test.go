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

// TestMacroFileNameDropsAnImageTitle: a pattern such as \((.+)\) captures an
// image's title along with its destination, and the title is not part of the
// file's name, in any of the three quotings.
func TestMacroFileNameDropsAnImageTitle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my file.png"), []byte("x"), 0o600))

	fileName := macroFileName(dir)

	assert.Equal(t, "logo.png", fileName(`logo.png "The logo"`))
	assert.Equal(t, "logo.png", fileName(`logo.png 'The logo'`))
	assert.Equal(t, "logo.png", fileName(`logo.png (The logo)`))
	assert.Equal(t, "my file.png", fileName(`<my file.png> "The logo"`))
	assert.Equal(t, "missing.png", fileName(`missing.png "The logo"`),
		"a file that is not there is still named without its title")
}

// TestMacroFileNameDropsAnEscapedImageTitle: a title may hold its own
// delimiter backslash-escaped, and is still a title.
func TestMacroFileNameDropsAnEscapedImageTitle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), []byte("x"), 0o600))

	fileName := macroFileName(dir)

	assert.Equal(t, "logo.png", fileName(`logo.png "a \"b\" c"`))
	assert.Equal(t, "logo.png", fileName(`logo.png 'Bob\'s'`))
	assert.Equal(t, "logo.png", fileName(`logo.png (a \) b)`))
	assert.Equal(t, "logo.png", fileName(`logo.png (a \( b)`))
}

// TestMacroFileNameLeavesUnbalancedParenthesesAlone: CommonMark does not read
// ![x](logo.png (a (b))) as an image at all, so there is no title to drop.
func TestMacroFileNameLeavesUnbalancedParenthesesAlone(t *testing.T) {
	assert.Nil(t, imageTitle.FindStringSubmatch(`logo.png (a (b))`))
}

// TestMacroFileNameTriesTheDestinationBeforeTheTitle: a title that reaches
// outside the project is only a title, and must not fail the run for an image
// that is sitting beside the document -- nor, when that image is missing, turn
// a warning into a failure.
func TestMacroFileNameTriesTheDestinationBeforeTheTitle(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), []byte("x"), 0o600))
	t.Chdir(dir)

	fileName := macroFileName(dir)

	assert.Equal(t, "logo.png", fileName(`logo.png "see v1/../../../old"`))
	assert.Equal(t, "missing.png", fileName(`missing.png "see v1/../../../old"`))
	assert.Equal(t, "../../../secret.png", fileName(`../../../secret.png "t"`),
		"a destination that reaches outside is still handed on to be refused")
}

// TestMacroFileNameKeepsANameThatEndsLikeATitle: a file whose name really ends
// in a parenthesised or quoted part still resolves when there is no file under
// the name without it.
func TestMacroFileNameKeepsANameThatEndsLikeATitle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "draft (v2)"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, `notes "final"`), []byte("x"), 0o600))

	fileName := macroFileName(dir)

	assert.Equal(t, "draft (v2)", fileName("draft (v2)"))
	assert.Equal(t, `notes "final"`, fileName(`notes "final"`))
}

// TestMacroFileNamePrefersTheDestinationWhenBothExist: with both "draft" and
// "draft (v2)" beside the document, the value reads as CommonMark reads
// ![x](draft (v2)) -- the file "draft", titled "v2".
func TestMacroFileNamePrefersTheDestinationWhenBothExist(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "draft"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "draft (v2)"), []byte("x"), 0o600))

	assert.Equal(t, "draft", macroFileName(dir)("draft (v2)"))
}

// TestMacroFileNameNamesAMissingFileAsTheDestinationDoes: when nothing is
// there, the warning and the page name the file, not the Markdown spelling of
// it.
func TestMacroFileNameNamesAMissingFileAsTheDestinationDoes(t *testing.T) {
	fileName := macroFileName(t.TempDir())

	assert.Equal(t, "missing file.png", fileName(`<missing file.png> "t"`))
	assert.Equal(t, "missing file.png", fileName(`<missing file.png>`))
	assert.Equal(t, "missing_file.png", fileName(`missing\_file.png "t"`))
	assert.Equal(t, "a&b.png", fileName(`a&amp;b.png`))
}

// TestMacroFileNameDoesNotDecodeIntoARootedPath: "%2Fetc%2Fpasswd" decodes to
// a path that is not beside the document, so it is no candidate; the value as
// written is, and is what the upload looks for and warns is missing.
func TestMacroFileNameDoesNotDecodeIntoARootedPath(t *testing.T) {
	fileName := macroFileName(t.TempDir())

	assert.Equal(t, "%2Fetc%2Fpasswd", fileName("%2Fetc%2Fpasswd"))
	assert.Equal(t, `\/etc/passwd`, fileName(`\/etc/passwd`))
}
