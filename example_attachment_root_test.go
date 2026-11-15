package mark_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing/fstest"

	"github.com/kovetskiy/mark/v17"
	"github.com/kovetskiy/mark/v17/attachment"
)

// A caller that publishes one document per directory holds each document's
// files to its own directory. A file another document keeps then fails the
// document with attachment.ErrOutsideProject, which Run and Publisher.Publish
// return wrapped.
func ExampleRenderingConfig_attachmentRoot() {
	config := mark.Config{
		PublishConfig: mark.PublishConfig{
			Mode:   mark.ModeConfig{CompileOnly: true},
			Output: io.Discard,
		},
		Connection: mark.ConnectionConfig{BaseURL: "https://example.atlassian.net/wiki"},
		Documents: []mark.Document{{
			Path:    "guide/index.md",
			Content: []byte("<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\n![](../other-doc/diagram.png)\n"),
			FS: fstest.MapFS{
				"other-doc/diagram.png": {Data: []byte("\x89PNG\r\n\x1a\n")},
			},
		}},
	}
	fmt.Println("default:", mark.Run(context.Background(), config))

	config.Rendering.AttachmentRoot = attachment.BoundaryDocument
	err := mark.Run(context.Background(), config)
	fmt.Println("document:", errors.Is(err, attachment.ErrOutsideProject))
	fmt.Println(err)
	// Output:
	// default: <nil>
	// document: true
	// unable to compile markdown: line 2, col 1: outside the project: "../other-doc/diagram.png" resolves to other-doc/diagram.png, which is outside the document's directory "guide": Rendering.AttachmentRoot (--attachment-root) is "document"
}

// A directory root has to exist, which Validate checks before anything is
// published.
func ExamplePublishConfig_Validate_attachmentRoot() {
	config := mark.PublishConfig{
		Rendering: mark.RenderingConfig{AttachmentRoot: "no-such-directory"},
	}

	err := config.Validate()
	fmt.Println(errors.Is(err, os.ErrNotExist))

	// The message ends in the operating system's own words for the missing
	// directory, so only its start is printed here.
	message, _, _ := strings.Cut(err.Error(), ": stat ")
	fmt.Println(message)
	// Output:
	// true
	// invalid Rendering.AttachmentRoot (--attachment-root): attachment root "no-such-directory" is not a usable directory
}
