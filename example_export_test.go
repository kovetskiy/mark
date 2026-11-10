package mark_test

import (
	"context"
	"fmt"

	"github.com/kovetskiy/mark/v17"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/export"
)

// ExampleExport_textOnly reads a page's title, labels and body as values: no
// metadata in the body, no attachments downloaded, nothing written anywhere.
func ExampleExport_textOnly() {
	// A fake Confluence stands in for a real one here.
	server := confluencetest.Start()
	defer server.Close()
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	page := server.AddPage("DOCS", "Release process", "page", home.ID)
	server.EditPage(page.ID, `<p>Tag the release, then <strong>publish</strong> it.</p>`)
	server.AddLabel(page.ID, "how-to")
	server.AddLabel(page.ID, "team=docs")

	result, err := mark.Export(context.Background(), mark.ExportConfig{
		Connection: mark.ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		PageID:     page.ID,
		Metadata:   export.MetadataNone,
		// The body still refers to its attachments by name; none is fetched.
		// Without NoAttachments, and with no Output or AttachmentsDir, they
		// would be written to the working directory.
		NoAttachments: true,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("title:", result.Title)
	fmt.Println("labels:", result.Labels)
	fmt.Print(result.Markdown)
	// Output:
	// title: Release process
	// labels: [how-to team=docs]
	// Tag the release, then **publish** it.
}
