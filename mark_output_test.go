package mark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func outputServer(t *testing.T) *confluencetest.Server {
	t.Helper()
	s := confluencetest.New(t)
	home := s.AddPage("DOCS", "Home", "page", "")
	s.SetHomepage("DOCS", home.ID)
	s.AddPage("DOCS", "Parent", "page", home.ID)
	return s
}

func runWithFormat(t *testing.T, format string, files map[string]string) (string, error) {
	t.Helper()

	server := outputServer(t)
	dir := t.TempDir()
	for name, content := range files {
		writeFile(t, dir, name, content)
	}

	var out strings.Builder
	err := Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		OutputFormat: format, ContinueOnError: true, Output: &out,
	})

	return out.String(), err
}

const outHeader = "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n"

func TestOutputFormatURLIsUnchanged(t *testing.T) {
	out, err := runWithFormat(t, "", map[string]string{
		"a.md": outHeader + "<!-- Title: A -->\n\nA.\n",
	})
	require.NoError(t, err)

	assert.Contains(t, out, "/display/DOCS/")
	assert.NotContains(t, out, "{", "the default output is not JSON")
}

func TestOutputFormatJSON(t *testing.T) {
	out, err := runWithFormat(t, "json", map[string]string{
		"a.md": outHeader + "<!-- Title: A -->\n\nA.\n",
		"b.md": outHeader + "<!-- Title: B -->\n<!-- Synchronized: false -->\n\nB.\n",
	})
	require.NoError(t, err)

	var got struct {
		Pages []struct {
			File, Status, Space, Title, PageID, URL, Reason string
		} `json:"pages"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got), "the output must parse as JSON")
	require.Len(t, got.Pages, 2)

	byStatus := map[string]string{}
	for _, page := range got.Pages {
		byStatus[page.Status] = page.File
		assert.NotEmpty(t, page.File)
	}

	require.Contains(t, byStatus, "published")
	require.Contains(t, byStatus, "skipped")
	assert.True(t, strings.HasSuffix(byStatus["published"], "a.md"))
	assert.True(t, strings.HasSuffix(byStatus["skipped"], "b.md"))

	// The published page carries what a script would want to act on.
	for _, page := range got.Pages {
		if page.Status == "published" {
			assert.Equal(t, "DOCS", page.Space)
			assert.Equal(t, "A", page.Title)
			assert.NotEmpty(t, page.PageID)
			assert.Contains(t, page.URL, "/display/DOCS/")
		}
	}
}

// TestOutputFormatGitHubAnnotatesAFailingFile is why the format exists: the
// failure has to name the file so it lands on it in a pull request.
func TestOutputFormatGitHubAnnotatesAFailingFile(t *testing.T) {
	out, err := runWithFormat(t, "github", map[string]string{
		"good.md": outHeader + "<!-- Title: Good -->\n\nFine.\n",
		"bad.md":  outHeader + "<!-- Title: Bad -->\n\n<!-- ac:ignore -->\nunclosed\n",
	})
	require.Error(t, err, "the unclosed region should fail its file")

	var errorLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "::error") {
			errorLine = line
		}
	}

	require.NotEmpty(t, errorLine, "a failing file must produce an error annotation")
	assert.Contains(t, errorLine, "file=")
	assert.Contains(t, errorLine, "bad.md")
	assert.Contains(t, out, "::notice", "the page that published should be noticed")
	assert.NotContains(t, out, "\n\n::", "annotations are one per line")
}

func TestOutputFormatRejectsAnUnknownValue(t *testing.T) {
	_, err := runWithFormat(t, "yaml", map[string]string{
		"a.md": outHeader + "<!-- Title: A -->\n\nA.\n",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output-format")
}

// TestOutputFormatGitHubReportsOrphanActions.
func TestOutputFormatGitHubReportsOrphanActions(t *testing.T) {
	server := outputServer(t)
	dir := t.TempDir()
	writeFile(t, dir, "keep.md", outHeader+"<!-- Title: Keep -->\n\nKeep.\n")
	writeFile(t, dir, "gone.md", outHeader+"<!-- Title: Gone -->\n\nGone.\n")

	var first strings.Builder
	config := Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files: filepath.Join(dir, "*.md"), Features: []string{"mention"},
		TrackPages: true, OnOrphan: "delete", OutputFormat: "github", Output: &first,
	}
	require.NoError(t, Run(config))

	require.NoError(t, os.Remove(filepath.Join(dir, "gone.md")))

	var second strings.Builder
	config.Output = &second
	require.NoError(t, Run(config))

	assert.Contains(t, second.String(), "::warning")
	assert.Contains(t, second.String(), "gone.md")
}
