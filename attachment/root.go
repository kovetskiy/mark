package attachment

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
)

// Root is where the files a document names are read from, and the boundary
// they are held to.
//
// The zero Root is the local disk: names are OS paths, and a document may
// read what CheckReadable allows, its own directory or the one mark is
// running in. A Root from FSRoot reads from an fs.FS instead, and nothing
// outside it: a name is the slash-separated path inside it, and one that
// climbs above its root with ".." is refused with ErrOutsideProject. The
// fs.FS's root is the boundary, so a document there may reach
// "../images/logo.png" as long as that stays inside it. As on disk, an
// attachment's absolute name is joined to the document's directory, and
// CheckReadable refuses an absolute reference.
//
// The boundary is the names asked for: what the fs.FS does with them is its
// own. os.DirFS follows a symbolic link out of its directory; the fs.FS of
// an os.Root does not.
//
// A Root is an Opener, for ResolveLocalAttachment and
// ResolveLocalAttachments, which then also glob and check containment in it.
type Root struct {
	fsys fs.FS
}

// FSRoot is the Root reading from fsys; a nil fsys is the local disk.
func FSRoot(fsys fs.FS) Root {
	return Root{fsys: fsys}
}

// FS is what r reads from, or nil for the local disk.
func (r Root) FS() fs.FS {
	return r.fsys
}

// errNotInFS is what a name that does not name a path inside the fs.FS
// opens to: a file that is not there, as far as the FS can tell.
var errNotInFS = errors.New("not a path inside the document's file system")

// fsName is the path inside the fs.FS that name, a path a caller joined with
// filepath or path, stands for.
func (r Root) fsName(name string) (string, error) {
	if filepath.IsAbs(name) || path.IsAbs(filepath.ToSlash(name)) {
		return "", errNotInFS
	}

	cleaned := path.Clean(filepath.ToSlash(name))
	if !fs.ValidPath(cleaned) {
		return "", errNotInFS
	}

	return cleaned, nil
}

// inside is the path inside the fs.FS that name stands for, or the error an
// op on a name outside it fails with.
func (r Root) inside(op, name string) (string, error) {
	inside, err := r.fsName(name)
	if err != nil {
		return "", &fs.PathError{Op: op, Path: name, Err: err}
	}

	return inside, nil
}

// Open implements Opener.
func (r Root) Open(name string) (io.ReadCloser, error) {
	if r.fsys == nil {
		return os.Open(name)
	}

	inside, err := r.inside("open", name)
	if err != nil {
		return nil, err
	}

	return r.fsys.Open(inside)
}

// Stat describes the file name names.
func (r Root) Stat(name string) (fs.FileInfo, error) {
	if r.fsys == nil {
		return os.Stat(name)
	}

	inside, err := r.inside("stat", name)
	if err != nil {
		return nil, err
	}

	return fs.Stat(r.fsys, inside)
}

// ReadFile reads the file name names.
func (r Root) ReadFile(name string) ([]byte, error) {
	if r.fsys == nil {
		return os.ReadFile(name)
	}

	inside, err := r.inside("read", name)
	if err != nil {
		return nil, err
	}

	return fs.ReadFile(r.fsys, inside)
}

// CheckReadable is CheckReadable held to r's boundary: on the local disk the
// same check, and in an fs.FS that reference, joined to base unless it is
// absolute, stays inside it.
func (r Root) CheckReadable(base, reference string) error {
	if r.fsys == nil {
		return CheckReadable(base, reference)
	}

	joined := reference
	if !filepath.IsAbs(reference) && !path.IsAbs(filepath.ToSlash(reference)) {
		joined = filepath.Join(base, reference)
	}

	return r.contain(joined, reference)
}

// contain refuses a joined name that is not a path inside the fs.FS. named is
// what the document wrote.
func (r Root) contain(joined, named string) error {
	if _, err := r.fsName(joined); err != nil {
		return fmt.Errorf(
			"%w: %q resolves to %s, which is outside the document's file system",
			ErrOutsideProject, named, filepath.ToSlash(joined),
		)
	}

	return nil
}

// CheckWithin reports whether path, resolved through its symlinks, is inside
// dir: CheckReadable without the directory mark is running in, for a reader
// that may reach only the one directory, such as a document in an fs.FS
// reaching --include-path on disk.
func CheckWithin(dir, path string) error {
	resolved, ok := within([]string{dir}, path)
	if ok {
		return nil
	}

	return fmt.Errorf("%w: %q resolves to %s, which is outside %q", ErrOutsideProject, path, resolved, dir)
}

// rooted is an Opener that says itself how a name it opens is held to its
// boundary and how a pattern is matched in it; any other Opener is the disk.
type rooted interface {
	checkAttachment(base, name string) error
	glob(base, pattern string) ([]string, error)
}

// checkAttachment holds an attachment to r's boundary.
func (r Root) checkAttachment(base, name string) error {
	if r.fsys == nil {
		return checkAttachmentPath(base, name)
	}

	return r.contain(filepath.Join(base, name), name)
}

// glob matches pattern under base, as glob does on disk: in an fs.FS too its
// literal leading directories move the root, and only the rest is a pattern.
// A root outside the FS matches nothing, so the literal name is tried and
// refused by name.
func (r Root) glob(base, pattern string) ([]string, error) {
	if r.fsys == nil {
		return glob(base, pattern)
	}

	prefix, rest := doublestar.SplitPattern(path.Clean(filepath.ToSlash(pattern)))

	dir, err := r.fsName(path.Join(filepath.ToSlash(base), prefix))
	if err != nil {
		return nil, nil //nolint:nilerr // outside the FS: no matches, and the open refuses it
	}

	sub, err := fs.Sub(r.fsys, dir)
	if err != nil {
		return nil, err
	}

	found, err := doublestar.Glob(sub, rest)
	if err != nil {
		return nil, err
	}

	for i, match := range found {
		found[i] = filepath.FromSlash(path.Join(dir, match))
	}

	return found, nil
}
