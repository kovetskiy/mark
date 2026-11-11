package mark_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kovetskiy/mark/v17"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/report"
)

// A json report written to a writer of its own leaves the page URLs where they
// always are, so neither has to be rebuilt from the other.
func ExampleReportConfig_output() {
	server := confluencetest.Start()
	defer server.Close()
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir, err := os.MkdirTemp("", "mark-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)
	doc := "<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\nA guide.\n"
	if err := os.WriteFile(filepath.Join(dir, "guide.md"), []byte(doc), 0o600); err != nil {
		fmt.Println(err)
		return
	}

	var urls, reportJSON bytes.Buffer
	err = mark.Run(context.Background(), mark.Config{
		PublishConfig: mark.PublishConfig{
			Rendering: mark.RenderingConfig{Features: []string{"none"}},
			Output:    &urls,
		},
		Files:      filepath.Join(dir, "*.md"),
		Connection: mark.ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		Report: mark.ReportConfig{
			OutputFormat: report.FormatJSON,
			Output:       &reportJSON,
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Print(strings.ReplaceAll(urls.String(), server.URL, "https://confluence.example"))

	var result report.Report
	if err := json.Unmarshal(reportJSON.Bytes(), &result); err != nil {
		fmt.Println(err)
		return
	}
	for _, page := range result.Pages {
		fmt.Printf("%s: %s\n", page.Title, page.Status)
	}

	// Output:
	// https://confluence.example/display/DOCS/1003
	// Guide: published
}
