package mark_test

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kovetskiy/mark/v17"
	"github.com/kovetskiy/mark/v17/attachment"
)

// A caller that publishes one document per directory holds each document's
// files to its own directory. A file another document keeps then fails the
// document with attachment.ErrOutsideProject, which Run and Publisher.Publish
// return wrapped. A directory root has to exist, which Validate checks before
// anything is published.
func ExampleRenderingConfig_attachmentRoot() {
	config := mark.PublishConfig{
		Rendering: mark.RenderingConfig{AttachmentRoot: attachment.BoundaryDocument},
	}
	fmt.Println(config.Validate())

	config.Rendering.AttachmentRoot = "no-such-directory"
	err := config.Validate()
	fmt.Println(errors.Is(err, os.ErrNotExist))

	// The message ends in the operating system's own words for the missing
	// directory, so only its start is printed here.
	message, _, _ := strings.Cut(err.Error(), ": stat ")
	fmt.Println(message)
	// Output:
	// <nil>
	// true
	// invalid Rendering.AttachmentRoot (--attachment-root): attachment root "no-such-directory" is not a usable directory
}
