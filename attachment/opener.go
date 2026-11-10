package attachment

import (
	"io"
	"os"
)

// Opener opens the files a document refers to, so that tests can hand the
// resolver files that are not on disk.
type Opener interface {
	Open(name string) (io.ReadCloser, error)
}

// LocalOSOpener opens files from the local file system. The zero Root does
// too, and also globs and checks containment as the disk does.
type LocalOSOpener struct {
}

func (o LocalOSOpener) Open(name string) (io.ReadCloser, error) {
	return os.Open(name)
}

var LocalOS = LocalOSOpener{}
