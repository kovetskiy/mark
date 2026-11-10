package mark_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"testing/fstest"

	mark "github.com/kovetskiy/mark/v17"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/report"
)

// Two documents rendered in memory are published together, linking to each
// other and reading an image from an in-memory file system, with no staging
// directory on disk. Their pages are tracked by keys that stay the same from
// run to run, under a scope of their own, so a later run that no longer
// renders one of them archives its page.
func ExampleRun_documents() {
	assets := fstest.MapFS{
		"reference/diagram.png": {Data: []byte("\x89PNG\r\n\x1a\n")},
	}

	header := "<!-- Space: DOCS -->\n<!-- Parent: Reference -->\n"

	err := mark.Run(context.Background(), mark.Config{
		Connection: mark.ConnectionConfig{
			BaseURL:  "https://example.atlassian.net/wiki",
			Username: "bot@example.com",
			Password: "api-token",
		},
		Tracking: mark.TrackingConfig{
			TrackPages: true,
			OnOrphan:   report.OnOrphanArchive,
		},
		DocumentScope: "api-reference",
		Documents: []mark.Document{
			{
				// Where it sits: ./groups.md and diagram.png resolve from here.
				Path: "reference/users.md",
				// What the manifest remembers it by, the same on every run.
				Key: "reference/users.md",
				// What the report names as the file to edit.
				Source:  "templates/users.md.tmpl",
				Content: []byte(header + "<!-- Title: Users -->\n\nSee [groups](./groups.md).\n\n![](diagram.png)\n"),
				FS:      assets,
			},
			{
				Path:    "reference/groups.md",
				Key:     "reference/groups.md",
				Source:  "templates/groups.md.tmpl",
				Content: []byte(header + "<!-- Title: Groups -->\n\nGroups hold [users](./users.md).\n"),
				FS:      assets,
			},
		},
	})

	// A document that failed names its Path, and its Source.
	var failed *mark.FilesFailedError
	if errors.As(err, &failed) {
		for _, fileErr := range failed.Errors {
			fmt.Printf("%s (from %s): %v\n", fileErr.File, fileErr.Source, fileErr.Err)
		}
	} else if err != nil {
		log.Fatal(err)
	}
}

// A Publisher publishes documents one at a time, keeping its browser and
// caches between them. It keeps no page manifest, so a document's Key is not
// used: its page is found by its title.
func ExamplePublisher_PublishDocument() {
	ctx := context.Background()

	api := confluence.NewAPI("https://example.atlassian.net/wiki", confluence.Options{
		Username: "bot@example.com",
		Password: "api-token",
	})

	publisher, err := mark.NewPublisher(ctx, api, mark.PublishConfig{})
	if err != nil {
		log.Fatal(err)
	}
	defer publisher.Close()

	page, err := publisher.PublishDocument(ctx, mark.Document{
		Path:    "release-notes.md",
		Content: []byte("<!-- Space: DOCS -->\n<!-- Title: Release notes -->\n\nWhat changed.\n"),
	})
	if err != nil {
		log.Fatal(err)
	}

	if page != nil {
		fmt.Println(page.Title)
	}
}
