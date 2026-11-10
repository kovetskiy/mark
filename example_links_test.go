package mark_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/kovetskiy/mark/v17"
)

// A caller that renders its documents into a staging directory and publishes
// them from there sets the two roots, so that a relative link is answered by
// the tree it was written in rather than by the staged copy.
func ExampleLinkConfig() {
	source, _ := os.MkdirTemp("", "source")
	staged, _ := os.MkdirTemp("", "staged")
	defer os.RemoveAll(source)
	defer os.RemoveAll(staged)

	// The guide is in the source tree only: the caller staged the document
	// it is publishing and nothing else.
	doc := "<!-- Space: DOCS -->\n<!-- Title: Intro -->\n\nSee [the guide](./guide.md).\n"
	_ = os.WriteFile(filepath.Join(source, "intro.md"), []byte(doc), 0o600)
	_ = os.WriteFile(filepath.Join(source, "guide.md"), []byte("<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nGuide.\n"), 0o600)
	_ = os.WriteFile(filepath.Join(staged, "intro.md"), []byte(doc), 0o600)

	// A Confluence holding no pages yet: the guide's page is one a later
	// publish creates.
	confluence := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [], "size": 0}`))
	}))
	defer confluence.Close()

	config := mark.Config{
		PublishConfig: mark.PublishConfig{
			Mode:  mark.ModeConfig{CompileOnly: true},
			Links: mark.LinkConfig{CheckLinks: []string{"internal"}},
		},
		Files:      filepath.Join(staged, "*.md"),
		Connection: mark.ConnectionConfig{BaseURL: confluence.URL},
	}

	err := mark.Run(context.Background(), config)
	fmt.Println("staged copy alone:", strings.Contains(err.Error(), "there is no such file"))

	config.Links.StagedRoot = staged
	config.Links.SourceRoot = source

	fmt.Println("with the source tree:", mark.Run(context.Background(), config))
	// Output:
	// staged copy alone: true
	// with the source tree: <nil>
}

// Settings Run and NewPublisher would refuse can be checked on their own,
// before there is a connection or a file to publish.
func ExamplePublishConfig_Validate() {
	config := mark.PublishConfig{
		Links: mark.LinkConfig{CheckLinks: []string{"internal"}, StagedRoot: "build/docs"},
	}

	fmt.Println(config.Validate())
	// Output:
	// Links.StagedRoot (--links-staged-root) requires Links.SourceRoot (--links-source-root): one says where documents are read, the other where their links were written, and neither means anything alone
}

// A refused link setting names the field and its flag, so a caller can say
// which of its own settings to change without parsing the message.
func ExampleLinkConfigError() {
	config := mark.PublishConfig{
		Links: mark.LinkConfig{CheckLinksWarnOnly: true},
	}

	var refused *mark.LinkConfigError
	if errors.As(config.Validate(), &refused) {
		fmt.Println(refused.Field)
		fmt.Println(refused.Flag)
	}
	// Output:
	// Links.CheckLinksWarnOnly
	// --check-links-warn-only
}
