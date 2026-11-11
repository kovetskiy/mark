package attachment

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
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
// The boundary holds through symbolic links too, when the fs.FS can report
// them as an fs.ReadLinkFS, as os.DirFS, the fs.FS of an os.Root and
// fstest.MapFS can: every name is resolved one component at a time before it
// is opened, read, statted or globbed, a link followed only to a relative
// target that stays inside, and a chain of more than 40 links refused as a
// loop. An fs.FS that does not report links is held by name alone: one that
// cannot have links needs nothing more, but a wrapper that opens through
// os.DirFS without passing ReadLinkFS on follows them as os.DirFS does. A
// link swapped in between the check and the read is only kept out by an
// os.Root, which resolves as it opens.
//
// A Root is an Opener, for ResolveLocalAttachment and
// ResolveLocalAttachments, which then also glob and check containment in it.
type Root struct {
	fsys fs.FS
}

// FSRoot is the Root reading from fsys; a nil fsys is the local disk.
func FSRoot(fsys fs.FS) Root {
	if links, ok := fsys.(fs.ReadLinkFS); ok {
		return Root{fsys: linkedFS{links}}
	}

	return Root{fsys: fsys}
}

// FS is what r reads from, or nil for the local disk: for an fs.FS that
// reports symbolic links, one following them only inside it, which reports
// none itself.
func (r Root) FS() fs.FS {
	return r.fsys
}

// maxLinks is how many symbolic links one name may pass through, as Linux
// allows, before it is taken for a loop.
const maxLinks = 40

// linkedFS is an fs.FS holding the names it is asked for to its root through
// the symbolic links in them.
type linkedFS struct {
	fsys fs.ReadLinkFS
}

// resolve is name, a valid path, with every symbolic link in it followed. A
// link to an absolute path or above the root is ErrOutsideProject.
func (l linkedFS) resolve(name string) (string, error) {
	resolved := "."
	rest := strings.Split(name, "/")
	links := 0
	for len(rest) > 0 {
		part := rest[0]
		rest = rest[1:]

		switch part {
		case ".", "":
			continue
		case "..":
			// Only a link's target has these: name is a valid path.
			if resolved == "." {
				return "", errOutsideFS
			}
			resolved = path.Dir(resolved)

			continue
		}

		next := path.Join(resolved, part)
		info, err := l.fsys.Lstat(next)
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			resolved = next

			continue
		}

		links++
		if links > maxLinks {
			return "", fmt.Errorf("%s: too many levels of symbolic links", next)
		}

		target, err := l.fsys.ReadLink(next)
		if err != nil {
			return "", err
		}
		target = filepath.ToSlash(target)
		if target == "" || isAbs(target) || filepath.VolumeName(target) != "" {
			return "", fmt.Errorf("%w: %s links to %s", errOutsideFS, next, target)
		}

		rest = append(strings.Split(target, "/"), rest...)
	}

	return resolved, nil
}

// errOutsideFS is a symbolic link out of an fs.FS.
var errOutsideFS = fmt.Errorf("%w: a symbolic link leads out of the document's file system", ErrOutsideProject)

// at is name resolved, or the error op fails with.
func (l linkedFS) at(op, name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}

	resolved, err := l.resolve(name)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return "", &fs.PathError{Op: op, Path: name, Err: pathErr.Err}
		}

		return "", &fs.PathError{Op: op, Path: name, Err: err}
	}

	return resolved, nil
}

func (l linkedFS) Open(name string) (fs.File, error) {
	resolved, err := l.at("open", name)
	if err != nil {
		return nil, err
	}

	return l.fsys.Open(resolved)
}

func (l linkedFS) Stat(name string) (fs.FileInfo, error) {
	resolved, err := l.at("stat", name)
	if err != nil {
		return nil, err
	}

	return fs.Stat(l.fsys, resolved)
}

func (l linkedFS) ReadFile(name string) ([]byte, error) {
	resolved, err := l.at("read", name)
	if err != nil {
		return nil, err
	}

	return fs.ReadFile(l.fsys, resolved)
}

func (l linkedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	resolved, err := l.at("readdir", name)
	if err != nil {
		return nil, err
	}

	return fs.ReadDir(l.fsys, resolved)
}

// errNotInFS is what a name that does not name a path inside the fs.FS
// opens to: a file that is not there, as far as the FS can tell.
var errNotInFS = errors.New("not a path inside the document's file system")

// fsName is the path inside the fs.FS that name, a path a caller joined with
// filepath or path, stands for.
func (r Root) fsName(name string) (string, error) {
	if isAbs(name) {
		return "", errNotInFS
	}

	cleaned := path.Clean(filepath.ToSlash(name))
	if !fs.ValidPath(cleaned) {
		return "", errNotInFS
	}

	return cleaned, nil
}

// isAbs reports whether name is absolute as an OS path or as a slash path.
func isAbs(name string) bool {
	return filepath.IsAbs(name) || path.IsAbs(filepath.ToSlash(name))
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
	if !isAbs(reference) {
		joined = filepath.Join(base, reference)
	}

	return r.contain(joined, reference)
}

// contain refuses a joined name that is not a path inside the fs.FS. named is
// what the document wrote.
func (r Root) contain(joined, named string) error {
	inside, err := r.fsName(joined)
	if err != nil {
		return fmt.Errorf(
			"%w: %q resolves to %s, which is outside the document's file system",
			ErrOutsideProject, named, filepath.ToSlash(joined),
		)
	}

	// A name not there yet is left to the read to report.
	if linked, ok := r.fsys.(linkedFS); ok {
		if _, err := linked.resolve(inside); errors.Is(err, ErrOutsideProject) {
			return fmt.Errorf("%q: %w", named, err)
		}
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

	prefix, rest := splitPattern(pattern)

	dir, err := r.fsName(path.Join(filepath.ToSlash(base), prefix))
	if err != nil {
		return nil, nil //nolint:nilerr // outside the FS: no matches, and the open refuses it
	}

	sub, err := fs.Sub(r.fsys, dir)
	if err != nil {
		return nil, err
	}

	return globIn(sub, filepath.FromSlash(dir), rest)
}
