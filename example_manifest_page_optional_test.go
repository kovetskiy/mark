package mark_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kovetskiy/mark/v17"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/kovetskiy/mark/v17/report"
)

// The first run into a space whose manifest page does not exist yet publishes
// untracked, and the JSON report names the space.
func ExampleTrackingConfig_manifestPageOptional() {
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

	var out bytes.Buffer
	err = mark.Run(context.Background(), mark.Config{
		PublishConfig: mark.PublishConfig{
			Rendering: mark.RenderingConfig{Features: []string{"none"}},
			Output:    &out,
		},
		Files:      filepath.Join(dir, "*.md"),
		Connection: mark.ConnectionConfig{BaseURL: server.URL, Username: "user", Password: "token"},
		Tracking: mark.TrackingConfig{
			TrackPages:           true,
			ManifestPage:         "Handbook",
			ManifestPageOptional: true,
		},
		Report: mark.ReportConfig{OutputFormat: report.FormatJSON},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	var result report.Report
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		fmt.Println(err)
		return
	}
	for _, space := range result.Untracked {
		fmt.Printf("space %s ran untracked: no page %q\n", space.Space, space.ManifestPage)
	}

	// Output:
	// space DOCS ran untracked: no page "Handbook"
}
