package mark

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	replacements = []string{
		"image1.jpg",
		"images/image2.jpg",
		"../image3.jpg",
	}
)

type bufferCloser struct {
	*bytes.Buffer
}

func (bufferCloser) Close() error { return nil }

type virtualOpenner struct {
	PathToBuf map[string]*bufferCloser
}

func (o *virtualOpenner) Open(name string) (io.ReadWriteCloser, error) {
	if buf, ok := o.PathToBuf[name]; ok {
		return buf, nil
	}
	return nil, os.ErrNotExist
}

func TestPrepareAttachmentsWithWorkDirBase(t *testing.T) {

	testingOpener := &virtualOpenner{
		PathToBuf: map[string]*bufferCloser{
			"image1.jpg":        &bufferCloser{bytes.NewBuffer(nil)},
			"images/image2.jpg": &bufferCloser{bytes.NewBuffer(nil)},
			"../image3.jpg":     &bufferCloser{bytes.NewBuffer(nil)},
		},
	}

	attaches, err := prepareAttachments(testingOpener, ".", replacements)
	t.Logf("attatches: %s", err)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "image1.jpg", attaches[0].Name)
	assert.Equal(t, "image1.jpg", attaches[0].Replace)

	assert.Equal(t, "images/image2.jpg", attaches[1].Name)
	assert.Equal(t, "images/image2.jpg", attaches[1].Replace)

	assert.Equal(t, "../image3.jpg", attaches[2].Name)
	assert.Equal(t, "../image3.jpg", attaches[2].Replace)

	assert.Equal(t, len(attaches), 3)
}

func TestPrepareAttachmentsWithSubDirBase(t *testing.T) {
	testingOpener := &virtualOpenner{
		PathToBuf: map[string]*bufferCloser{
			"a/b/image1.jpg":        {bytes.NewBuffer(nil)},
			"a/b/images/image2.jpg": {bytes.NewBuffer(nil)},
			"a/image3.jpg":          {bytes.NewBuffer(nil)},
		},
	}

	attaches, err := prepareAttachments(testingOpener, "a/b", replacements)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "image1.jpg", attaches[0].Name)
	assert.Equal(t, "image1.jpg", attaches[0].Replace)

	assert.Equal(t, "images/image2.jpg", attaches[1].Name)
	assert.Equal(t, "images/image2.jpg", attaches[1].Replace)

	assert.Equal(t, "../image3.jpg", attaches[2].Name)
	assert.Equal(t, "../image3.jpg", attaches[2].Replace)

	assert.Equal(t, len(attaches), 3)
}
