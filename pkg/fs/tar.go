package fs

import (
	"archive/tar"
	"io"

	"github.com/reconquest/karma-go"
)

type TarFileSystem struct {
	files map[string][]byte
}

func NewTarFileSystem(input io.Reader) (*TarFileSystem, error) {
	files := map[string][]byte{}

	archive := tar.NewReader(input)
	for {
		header, err := archive.Next()
		if err != nil {
			return nil, karma.Format(
				err,
				"asdasd",
			)
		}
	}
}
