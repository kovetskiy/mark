package includes

import (
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncludeOutsideTheProjectIsRefused covers a directive naming a file the
// document has no business reading.
//
// An include becomes page content, so a path that climbs out of the repository
// publishes whatever it lands on -- and the same path written as an image has
// always been refused, which makes this the asymmetry rather than the decision.
func TestIncludeOutsideTheProjectIsRefused(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("a private thing\n"), 0o600))

	// Its own directory, so that neither it nor the working directory contains
	// the file being reached for.
	project := t.TempDir()
	document := filepath.Join(project, "docs")
	require.NoError(t, os.Mkdir(document, 0o755))

	t.Chdir(project)

	// Written as a climb rather than as an absolute path, which is the form
	// that reaches anything: a directive naming "/etc/hostname" is joined to
	// the document's directory and lands nowhere, while "../../.." is cleaned
	// on the way and lands exactly where it says.
	reference := filepath.Join("..", "..", filepath.Base(outside), "secret.txt")

	_, _, _, err := ProcessIncludes(
		document, "", []byte("<!-- Include: "+reference+" -->"), template.New("test"))
	require.Error(t, err)
	assert.ErrorIs(t, err, attachment.ErrOutsideProject)
}

// TestIncludeOfAnAbsolutePathFindsNothing pins the other form, which was never
// a way out: it is joined to the document's directory like any other name, so
// it names a file below the document that does not exist.
func TestIncludeOfAnAbsolutePathFindsNothing(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("a private thing\n"), 0o600))

	project := t.TempDir()
	t.Chdir(project)

	_, _, _, err := ProcessIncludes(
		project, "", []byte("<!-- Include: "+secret+" -->"), template.New("test"))
	require.Error(t, err)

	assert.NotContains(t, err.Error(), "a private thing")
}

// TestIncludeBesideTheDocumentStillWorks is the ordinary case, and the boundary
// the refusal above rests on.
func TestIncludeBesideTheDocumentStillWorks(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(project, "fragment.md"), []byte("included\n"), 0o600))

	_, output, _, err := ProcessIncludes(
		project, "", []byte("<!-- Include: fragment.md -->"), template.New("test"))
	require.NoError(t, err)

	assert.Contains(t, string(output), "included")
}

// TestIncludeUnderTheIncludePathStillWorks covers the directory the operator
// chose rather than the document: --include-path is a permitted root because
// somebody publishing the run named it, which is the whole of what it is for.
func TestIncludeUnderTheIncludePathStillWorks(t *testing.T) {
	shared := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(shared, "shared.md"), []byte("from the shared directory\n"), 0o600))

	project := t.TempDir()
	t.Chdir(project)

	_, output, _, err := ProcessIncludes(
		project, shared, []byte("<!-- Include: shared.md -->"), template.New("test"))
	require.NoError(t, err)

	assert.Contains(t, string(output), "from the shared directory")
}

// TestIncludePathDoesNotOpenTheWholeDisk is its boundary: naming a directory to
// look in does not make everything above it readable too.
func TestIncludePathDoesNotOpenTheWholeDisk(t *testing.T) {
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(outside, "secret.txt"), []byte("a private thing\n"), 0o600))

	shared := t.TempDir()

	project := t.TempDir()
	document := filepath.Join(project, "docs")
	require.NoError(t, os.Mkdir(document, 0o755))

	t.Chdir(project)

	reference := filepath.Join("..", "..", filepath.Base(outside), "secret.txt")

	_, _, _, err := ProcessIncludes(
		document, shared, []byte("<!-- Include: "+reference+" -->"), template.New("test"))
	require.Error(t, err)
	assert.ErrorIs(t, err, attachment.ErrOutsideProject)
}
