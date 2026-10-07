package mark

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nameOnly is macroFileName for the tests that look only at the name.
func nameOnly(base string) func(string) string {
	resolve := macroFileName(base)

	return func(value string) string {
		name, _ := resolve(value)

		return name
	}
}

// TestMacroFileNameSaysWhetherTheValueIsAFile: the decision is made once, from
// the value as written, and handed on with the name.
func TestMacroFileNameSaysWhetherTheValueIsAFile(t *testing.T) {
	resolve := macroFileName(t.TempDir())

	for value, want := range map[string]struct {
		name   string
		isFile bool
	}{
		"https://example.com/a.png":     {"https://example.com/a.png", false},
		"<https://example.com/a.png>":   {"https://example.com/a.png", false},
		`https://example.com/a.png "t"`: {`https://example.com/a.png "t"`, false},
		"/etc/passwd":                   {"/etc/passwd", false},
		`\\server\share\a.png`:          {`\\server\share\a.png`, false},
		`C:\docs\a.png`:                 {`C:\docs\a.png`, false},
		`\/etc/passwd`:                  {`\/etc/passwd`, true},
		"%2Fetc%2Fpasswd":               {"%2Fetc%2Fpasswd", true},
		"&#47;etc&#47;passwd":           {"&#47;etc&#47;passwd", true},
		"missing.png":                   {"missing.png", true},
		"<missing file.png>":            {"missing file.png", true},
	} {
		name, isFile := resolve(value)

		assert.Equal(t, want.name, name, value)
		assert.Equal(t, want.isFile, isFile, value)
	}
}

// TestMacroFileNameReadsAngleBracketDestination: "<my file.png>" names the
// file the same way an image destination does, brackets and all taken off.
func TestMacroFileNameReadsAngleBracketDestination(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my file.png"), []byte("x"), 0o600))

	assert.Equal(t, "my file.png", nameOnly(dir)("<my file.png>"))
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

	fileName := nameOnly(dir)

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

	fileName := nameOnly(dir)

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

	fileName := nameOnly(dir)

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

	fileName := nameOnly(dir)

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

	fileName := nameOnly(dir)

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

	assert.Equal(t, "draft", nameOnly(dir)("draft (v2)"))
}

// TestMacroFileNameNamesAMissingFileAsTheDestinationDoes: when nothing is
// there, the warning and the page name the file, not the Markdown spelling of
// it.
func TestMacroFileNameNamesAMissingFileAsTheDestinationDoes(t *testing.T) {
	fileName := nameOnly(t.TempDir())

	assert.Equal(t, "missing file.png", fileName(`<missing file.png> "t"`))
	assert.Equal(t, "missing file.png", fileName(`<missing file.png>`))
	assert.Equal(t, "missing_file.png", fileName(`missing\_file.png "t"`))
	assert.Equal(t, "a&b.png", fileName(`a&amp;b.png`))
}

// TestMacroFileNameDoesNotDecodeIntoARootedPath: "%2Fetc%2Fpasswd" decodes to
// a path that is not beside the document, so it is no candidate; the value as
// written is, and is what the upload looks for and warns is missing.
func TestMacroFileNameDoesNotDecodeIntoARootedPath(t *testing.T) {
	fileName := nameOnly(t.TempDir())

	assert.Equal(t, "%2Fetc%2Fpasswd", fileName("%2Fetc%2Fpasswd"))
	assert.Equal(t, `\/etc/passwd`, fileName(`\/etc/passwd`))
}

const widthMacroForTest = `<!-- Macro: \!\[.*\]\((.+)\)\<\!\-\- width=(.*) \-\-\>
     Template: ac:image
     Attachment: ${1}
     Width: ${2} -->
`

// compileWidthMacro compiles one image through the README's width macro and
// returns the page, what was attached, and what was logged.
func compileWidthMacro(t *testing.T, dir, value string) (string, []attachment.Attachment, string) {
	t.Helper()

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	defer func() { log.Logger = restore }()

	source := widthMacroForTest + "\n![A](" + value + ")<!-- width=10 -->\n"

	html, attached, err := CompileMarkdown([]byte(source), std, filepath.Join(dir, "doc.md"), types.MarkConfig{})
	require.NoError(t, err)

	return html, attached, logged.String()
}

// TestMacroFileNameLeavesAnAngleBracketedURLAlone: "<https://...>" is the same
// URL as the bare one, so it is neither looked for nor warned about, and the
// page is the one the bare URL gives.
func TestMacroFileNameLeavesAnAngleBracketedURLAlone(t *testing.T) {
	dir := t.TempDir()

	bare, _, _ := compileWidthMacro(t, dir, "https://example.com/a.png")
	html, attached, logged := compileWidthMacro(t, dir, "<https://example.com/a.png>")

	assert.Empty(t, attached)
	assert.NotContains(t, logged, "is not uploaded")
	assert.Equal(t, bare, html)
}

// TestMacroFileNameWarnsOfAnEscapedRootedPath: "\/etc/passwd" reads as
// "/etc/passwd", just as "%2Fetc%2Fpasswd" and "&#47;etc&#47;passwd" do, and is
// warned about the same way -- without anything being looked up for it.
func TestMacroFileNameWarnsOfAnEscapedRootedPath(t *testing.T) {
	for _, value := range []string{`\/etc/passwd`, "%2Fetc%2Fpasswd", "&#47;etc&#47;passwd"} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			// Where joining the value onto the directory would land on Linux.
			require.NoError(t, os.MkdirAll(filepath.Join(dir, `\`, "etc"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, `\`, "etc", "passwd"), []byte("x"), 0o600))

			_, attached, logged := compileWidthMacro(t, dir, value)

			assert.Empty(t, attached)
			assert.Contains(t, logged, "is not uploaded")
		})
	}
}
