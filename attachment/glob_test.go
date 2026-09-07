package attachment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/vfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// write puts a file where a document can name it, and returns nothing: what
// these tests care about is which files a pattern found, not what is in them.
func write(t *testing.T, dir, name string) {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("content of "+name), 0o600))
}

// names returns what each attachment will be referred to by, which is the path
// the document would write.
func names(attachments []Attachment) []string {
	got := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		got = append(got, attachment.Replace)
	}

	return got
}

// TestAttachmentPatternUploadsEveryMatch covers the point of the feature: a
// directory of images is one intention, and writing it out file by file is
// bookkeeping the document should not have to carry.
func TestAttachmentPatternUploadsEveryMatch(t *testing.T) {
	_, docs := project(t)

	write(t, docs, "images/b.png")
	write(t, docs, "images/a.png")
	write(t, docs, "images/c.png")
	write(t, docs, "images/notes.txt")

	got, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"images/*.png"})
	require.NoError(t, err)

	// Sorted, because the order files are uploaded in should not depend on how
	// the directory happens to be laid out.
	assert.Equal(t, []string{"images/a.png", "images/b.png", "images/c.png"}, names(got))
}

// TestAttachmentPatternMatchesRecursively covers ** , which is the syntax
// --files already uses and so the one people will reach for.
func TestAttachmentPatternMatchesRecursively(t *testing.T) {
	_, docs := project(t)

	write(t, docs, "media/a.png")
	write(t, docs, "media/nested/b.png")

	got, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"media/**/*.png"})
	require.NoError(t, err)

	assert.Equal(t, []string{"media/a.png", "media/nested/b.png"}, names(got))
}

// TestAttachmentWithoutAPatternIsUnchanged is the compatibility boundary: an
// ordinary name is still an ordinary name, and still fails in the same words.
func TestAttachmentWithoutAPatternIsUnchanged(t *testing.T) {
	_, docs := project(t)
	write(t, docs, "logo.png")

	got, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"logo.png"})
	require.NoError(t, err)
	assert.Equal(t, []string{"logo.png"}, names(got))

	_, err = ResolveLocalAttachments(vfs.LocalOS, docs, []string{"missing.png"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing.png")
}

// TestAttachmentNamedLikeAPatternStillWorks is the reason a pattern that finds
// nothing falls back to the name as written.
//
// A bracket is a character and a syntax at once, and the two cannot be told
// apart from the outside: a file really called "report[2024].pdf" is an
// ordinary name that somebody has already attached, and reading it as a pattern
// would turn a document that publishes into one that does not.
func TestAttachmentNamedLikeAPatternStillWorks(t *testing.T) {
	_, docs := project(t)
	write(t, docs, "report[2024].pdf")

	got, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"report[2024].pdf"})
	require.NoError(t, err)

	assert.Equal(t, []string{"report[2024].pdf"}, names(got))
}

// TestAttachmentPatternMatchingNothingIsReportedByName covers the typo, which
// is what a pattern finding nothing usually is. It is reported by the open that
// follows, naming the path the author actually wrote.
func TestAttachmentPatternMatchingNothingIsReportedByName(t *testing.T) {
	_, docs := project(t)
	write(t, docs, "images/a.png")

	_, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"pictures/*.png"})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "pictures/*.png")
}

// TestAttachmentPatternCannotReachOutsideTheProject is the one that matters: a
// pattern is written by a document, and a document is content.
func TestAttachmentPatternCannotReachOutsideTheProject(t *testing.T) {
	root, docs := project(t)

	outside := filepath.Dir(root)
	secret := filepath.Join(outside, "secret-"+filepath.Base(root)+".pem")
	require.NoError(t, os.WriteFile(secret, []byte("a private key"), 0o600))

	t.Cleanup(func() { _ = os.Remove(secret) })

	_, err := ResolveLocalAttachments(vfs.LocalOS, docs,
		[]string{filepath.Join("..", "..", "secret-"+filepath.Base(root)+".pem")})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOutsideProject)

	// And the same reach written as a pattern, which is the form that would
	// otherwise sweep up whatever it found.
	_, err = ResolveLocalAttachments(vfs.LocalOS, docs, []string{filepath.Join("..", "..", "*.pem")})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOutsideProject)
}

// TestMatchedAttachmentsAreNotReportedUnused covers the noise a pattern would
// otherwise make. Somebody who wrote "images/*.png" asked for the set: warning
// about each file the page did not link to would be a line per file, every run,
// for doing what was asked.
func TestMatchedAttachmentsAreNotReportedUnused(t *testing.T) {
	_, docs := project(t)

	write(t, docs, "images/a.png")
	write(t, docs, "images/b.png")
	write(t, docs, "named.png")

	got, err := ResolveLocalAttachments(vfs.LocalOS, docs, []string{"images/*.png", "named.png"})
	require.NoError(t, err)

	resolver := NewResolver(got)

	assert.Equal(t, []string{"named.png"}, resolver.Unused(got),
		"only what the document named outright is worth reporting")
}
