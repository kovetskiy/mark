package fs

import (
	"io"
	"os"
	"path/filepath"
)

type DiskFileSystem struct {
	baseDir string
}

func NewDiskFileSystem(baseDir string) *DiskFileSystem {
	return &DiskFileSystem{
		baseDir: baseDir,
	}
}

func (system *DiskFileSystem) Open(path string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(system.baseDir, path))
}
