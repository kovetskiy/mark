package vfs

import (
	"io"
	"os"
)

type Opener interface {
	Open(name string) (io.ReadWriteCloser, error)
}

type LocalOSOpener struct {
}

func (o LocalOSOpener) Open(name string) (io.ReadWriteCloser, error) {
	return os.Open(name)
}

var LocalOS = LocalOSOpener{}
