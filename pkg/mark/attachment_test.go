package mark

import (
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

func TestPrepareAttachmentsWithWorkDirBase(t *testing.T) {

	attaches, err := prepareAttachments(".", replacements)
	if err != nil {
		println(err.Error())
	}

	assert.Equal(t, "image1.jpg", attaches[0].Name)
	assert.Equal(t, "image1.jpg", attaches[0].Replace)
	assert.Equal(t, "image1.jpg", attaches[0].Path)

	assert.Equal(t, "images/image2.jpg", attaches[1].Name)
	assert.Equal(t, "images/image2.jpg", attaches[1].Replace)
	assert.Equal(t, "images/image2.jpg", attaches[1].Path)

	assert.Equal(t, "../image3.jpg", attaches[2].Name)
	assert.Equal(t, "../image3.jpg", attaches[2].Replace)
	assert.Equal(t, "../image3.jpg", attaches[2].Path)

	assert.Equal(t, len(attaches), 3)
}

func TestPrepareAttachmentsWithSubDirBase(t *testing.T) {

	attaches, _ := prepareAttachments("a/b", replacements)

	assert.Equal(t, "image1.jpg", attaches[0].Name)
	assert.Equal(t, "image1.jpg", attaches[0].Replace)
	assert.Equal(t, "a/b/image1.jpg", attaches[0].Path)

	assert.Equal(t, "images/image2.jpg", attaches[1].Name)
	assert.Equal(t, "images/image2.jpg", attaches[1].Replace)
	assert.Equal(t, "a/b/images/image2.jpg", attaches[1].Path)

	assert.Equal(t, "../image3.jpg", attaches[2].Name)
	assert.Equal(t, "../image3.jpg", attaches[2].Replace)
	assert.Equal(t, "a/image3.jpg", attaches[2].Path)

	assert.Equal(t, len(attaches), 3)
}
