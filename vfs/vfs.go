package vfs

import (
	"io"
	"os"
)

type Opener interface {
	Open(name string) (io.ReadCloser, error)
}

type LocalOSOpener struct {
}

func (o LocalOSOpener) Open(name string) (io.ReadCloser, error) {
	return os.Open(name)
}

var LocalOS = LocalOSOpener{}
