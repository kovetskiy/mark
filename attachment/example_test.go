package attachment_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kovetskiy/mark/v17/attachment"
)

// A caller publishing one document per directory holds each to its own, so
// that one document cannot upload another's files.
func ExampleRoot_WithBoundary() {
	repo, err := os.MkdirTemp("", "mark-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(repo) }()

	for _, file := range []string{"guide/diagram.png", "other-doc/file.pdf"} {
		path := filepath.Join(repo, filepath.FromSlash(file))
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(file), 0o600)
	}

	guide := filepath.Join(repo, "guide")
	root := attachment.Root{}.WithBoundary(attachment.BoundaryDocument)

	found, err := attachment.ResolveLocalAttachments(root, guide, []string{"diagram.png"})
	fmt.Println(len(found), err)

	_, err = attachment.ResolveLocalAttachments(root, guide, []string{"../other-doc/file.pdf"})
	fmt.Println(errors.Is(err, attachment.ErrOutsideProject))

	// Output:
	// 1 <nil>
	// true
}
