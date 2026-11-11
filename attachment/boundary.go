package attachment

import (
	"fmt"
	"os"
)

// Boundary is how far the files a document names may reach, with symlinks
// followed, to be read for it: Attachment: headers, images, files links point
// at, files macros name, and a d2 diagram's imports and images. A Root carries
// one (Root.WithBoundary), and a file outside it is refused with
// ErrOutsideProject. On the command line it is --attachment-root.
//
// The zero value is the default boundary: on disk the document's own
// directory or the working directory, whichever holds the file, so that
// "../images/logo.png" from a docs directory reaches the repository's shared
// images; in an fs.FS, the fs.FS. BoundaryDocument narrows either to the
// document's directory and what is below it. Any other value is a directory
// on disk, relative to the working directory or absolute, that every
// document's files have to be inside, whichever document names them.
//
// A directory really called "document" is written "./document".
type Boundary string

// BoundaryDocument holds each document to its own directory, for a caller that
// publishes one document per directory and wants none reaching into another's.
const BoundaryDocument Boundary = "document"

// NamesDirectory reports whether b names a directory on disk rather than
// being the default or BoundaryDocument. It does not look at the disk:
// Validate checks that the directory is there.
func (b Boundary) NamesDirectory() bool {
	return b != "" && b != BoundaryDocument
}

// Validate refuses a Boundary that names a directory which is not there, or is
// not a directory. The zero value and BoundaryDocument are always valid.
func (b Boundary) Validate() error {
	if !b.NamesDirectory() {
		return nil
	}

	info, err := os.Stat(string(b))
	if err != nil {
		return fmt.Errorf("attachment root %q is not a usable directory: %w", string(b), err)
	}

	if !info.IsDir() {
		return fmt.Errorf("attachment root %q is not a directory", string(b))
	}

	return nil
}
