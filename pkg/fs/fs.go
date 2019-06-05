package fs

import (
	"io"
)

type FileSystem interface {
	Open(path string) (io.ReadCloser, error)
}
