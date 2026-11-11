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
//
// WithIncludePath gives a Root that also reads --include-path, which is on
// disk whatever the document's Root is. WithBoundary gives one holding the
// files a document names to a narrower Boundary.
type Root struct {
	fsys fs.FS

	// boundary narrows what checkAttachment and CheckReadable allow.
	boundary Boundary

	// includePath is --include-path as given; includeDir is it made absolute
	// for a Root reading from an fs.FS, where an absolute name is one under it.
	includePath string
	includeDir  string
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

// WithIncludePath is r reading the files under dir, --include-path, from the
// local disk, and every other name as r does.
//
// On the local disk that is r itself: dir is a directory like any other
// there, under the rules CheckReadable and its callers already give it. In an
// fs.FS no name is absolute, so an absolute name is one under dir, which
// IncludeName gives: Open, Stat, ReadFile and CheckReadable read it from disk
// as long as it stays inside dir through its symbolic links (CheckWithin),
// and refuse it with ErrOutsideProject otherwise; Stat reports one that is
// not there missing before resolving its links. Attachments and globs stay
// inside the fs.FS. An empty dir is r.
func (r Root) WithIncludePath(dir string) Root {
	r.includePath, r.includeDir = dir, ""
	if dir != "" && r.fsys != nil {
		r.includeDir = dir
		if abs, err := filepath.Abs(dir); err == nil {
			r.includeDir = abs
		} else {
			r.includeDir = filepath.Clean(dir)
		}
	}

	return r
}

// IncludeName is the name r reads the file rel names under its include path
// by, and false when r has none: joined to --include-path on the local disk,
// and absolute for an fs.FS, so that it is not taken for a name inside it.
func (r Root) IncludeName(rel string) (string, bool) {
	if r.includePath == "" {
		return "", false
	}

	if r.fsys == nil {
		return filepath.Join(r.includePath, rel), true
	}

	return filepath.Join(r.includeDir, rel), true
}

// OnDisk reports whether r reads name from the local disk: every name for the
// zero Root, and for one reading from an fs.FS an absolute name, under
// WithIncludePath only.
func (r Root) OnDisk(name string) bool {
	return r.fsys == nil || (r.includeDir != "" && filepath.IsAbs(name))
}

// WithBoundary is r with the files a document names held to b as well as to
// r's own limits: every attachment ResolveLocalAttachment and
// ResolveLocalAttachments read through r, and every reference CheckReadable
// passes. Open, Stat and ReadFile are unchanged, so a caller reading a file
// that is not the document's to publish, such as another document a link
// names, reads it as before.
//
// BoundaryDocument holds a file to the document's directory, the base those
// functions are given, in an fs.FS as on disk; a name under --include-path,
// on disk for a Root reading from an fs.FS, is outside it. A directory b is a
// directory on disk, so it holds only a Root on the local disk: one reading
// from an fs.FS refuses every file under it, and is narrowed with fs.Sub
// instead.
func (r Root) WithBoundary(b Boundary) Root {
	r.boundary = b

	return r
}

// Boundary is the Boundary r holds a document's files to.
func (r Root) Boundary() Boundary {
	return r.boundary
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

// Open opens name with its links resolved. fs.ReadFile and fs.ReadDir come
// through it too.
func (l linkedFS) Open(name string) (fs.File, error) {
	resolved, err := l.resolveFor("open", name)
	if err != nil {
		return nil, err
	}

	return l.fsys.Open(resolved)
}

// Stat describes name with its links resolved, without opening it.
func (l linkedFS) Stat(name string) (fs.FileInfo, error) {
	resolved, err := l.resolveFor("stat", name)
	if err != nil {
		return nil, err
	}

	// No link is left in resolved, so Lstat describes what it names.
	return l.fsys.Lstat(resolved)
}

// resolveFor is resolve failing as op does.
func (l linkedFS) resolveFor(op, name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}

	resolved, err := l.resolve(name)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}

		return "", &fs.PathError{Op: op, Path: name, Err: err}
	}

	return resolved, nil
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

// checkDisk refuses a name r reads from disk that is not its to read: for a
// Root reading from an fs.FS, one outside the include path through its
// symbolic links. The local disk's own rules are its callers'.
func (r Root) checkDisk(name string) error {
	if r.fsys == nil {
		return nil
	}

	return CheckWithin(r.includePath, name)
}

// Open implements Opener.
func (r Root) Open(name string) (io.ReadCloser, error) {
	if r.OnDisk(name) {
		if err := r.checkDisk(name); err != nil {
			return nil, err
		}

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
	if r.OnDisk(name) {
		// Statted first: most names a link lookup asks about are not there,
		// and a missing one needs no links resolved to say so.
		info, err := os.Stat(name)
		if err != nil {
			return nil, err
		}

		if err := r.checkDisk(name); err != nil {
			return nil, err
		}

		return info, nil
	}

	inside, err := r.inside("stat", name)
	if err != nil {
		return nil, err
	}

	return fs.Stat(r.fsys, inside)
}

// ReadFile reads the file name names.
func (r Root) ReadFile(name string) ([]byte, error) {
	if r.OnDisk(name) {
		if err := r.checkDisk(name); err != nil {
			return nil, err
		}

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
// absolute, stays inside it. An absolute reference is refused, unless
// WithIncludePath put it on disk, where it has to stay inside the include
// path. Either way, a Boundary from WithBoundary narrows it further.
func (r Root) CheckReadable(base, reference string) error {
	if r.fsys == nil {
		return r.boundary.checkReadable(base, reference)
	}

	if r.OnDisk(reference) {
		if err := CheckWithin(r.includePath, reference); err != nil {
			return err
		}

		return r.narrow(base, reference, reference)
	}

	joined := reference
	if !isAbs(reference) {
		joined = filepath.Join(base, reference)
	}

	if err := r.contain(joined, reference); err != nil {
		return err
	}

	return r.narrow(base, joined, reference)
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

	// Only a link out of the FS is refused here; a name not there yet is
	// left to the read to report.
	if _, err := fs.Stat(r.fsys, inside); errors.Is(err, ErrOutsideProject) {
		return fmt.Errorf("%q: %w", named, err)
	}

	return nil
}

// CheckWithin reports whether path, resolved through its symlinks, is inside
// dir: CheckReadable without the directory mark is running in, for a reader
// that may reach only the one directory, such as a document in an fs.FS
// reaching --include-path on disk.
func CheckWithin(dir, path string) error {
	resolved, ok, err := withinDir(dir, path)
	if err != nil || ok {
		return err
	}

	return fmt.Errorf("%w: %q resolves to %s, which is outside %q", ErrOutsideProject, path, resolved, dir)
}

// checkAttachment holds an attachment to r's boundary.
func (r Root) checkAttachment(base, name string) error {
	if r.fsys == nil {
		return r.boundary.checkAttachmentPath(base, name)
	}

	joined := filepath.Join(base, name)
	if err := r.contain(joined, name); err != nil {
		return err
	}

	return r.narrow(base, joined, name)
}

// narrow holds joined, a name r reading from an fs.FS has already found
// inside it or under its include path, to r's boundary, drawn around a
// document in base. named is what the document wrote.
func (r Root) narrow(base, joined, named string) error {
	switch {
	case r.boundary == "":
		return nil
	case r.boundary.NamesDirectory():
		return fmt.Errorf(
			"%w: %q is read from the document's file system, which the attachment root %q, a directory on disk, does not hold",
			ErrOutsideProject, named, string(r.boundary),
		)
	}

	if !r.OnDisk(joined) {
		dir, dirErr := r.fsName(base)
		file, fileErr := r.fsName(joined)
		if dirErr == nil && fileErr == nil {
			file = r.resolveDeepest(file)
			if dir = r.resolveDeepest(dir); dir == "." || file == dir || strings.HasPrefix(file, dir+"/") {
				return nil
			}

			joined = file
		}
	}

	return r.boundary.outside(base, named, filepath.ToSlash(joined))
}

// resolveDeepest is name, a path inside the fs.FS, with the symbolic links in
// its longest leading part that exists followed, as fspath.ResolveDeepest
// does on disk; name as it is when the fs.FS reports no links.
func (r Root) resolveDeepest(name string) string {
	links, ok := r.fsys.(linkedFS)
	if !ok {
		return name
	}

	rest := ""
	for prefix := name; ; prefix = path.Dir(prefix) {
		if resolved, err := links.resolve(prefix); err == nil {
			return path.Join(resolved, rest)
		}

		if prefix == "." {
			return name
		}

		rest = path.Join(path.Base(prefix), rest)
	}
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
