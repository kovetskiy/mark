package mark

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkLinksRun publishes a directory containing one document with the given
// body and returns whatever the run made of it.
func checkLinksRun(t *testing.T, mode, body string, extra map[string]string) error {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	dir := t.TempDir()
	for name, content := range extra {
		if content == "" {
			require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o755))
			continue
		}
		writeFile(t, dir, name, content)
	}
	writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+body)

	return Run(Config{
		BaseURL: server.URL, Username: "user", Password: "token",
		Files:      filepath.Join(dir, "doc.md"),
		Features:   []string{"mention"},
		CheckLinks: mode,
		Output:     io.Discard,
	})
}

func TestCheckLinksRelativeOnly(t *testing.T) {
	t.Run("a link to a file that does not exist fails", func(t *testing.T) {
		err := checkLinksRun(t, "relative-only", "See [other](./missing.md).\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "./missing.md")
		assert.Contains(t, err.Error(), "there is no such file")
	})

	t.Run("a link to a file that is never published fails", func(t *testing.T) {
		// A file with no headers still gets metadata when the run knows a
		// space, so this shows up as a missing title rather than missing
		// metadata. Either way the file never becomes a page.
		err := checkLinksRun(t, "relative-only", "See [notes](./notes.md).\n",
			map[string]string{"notes.md": "Just notes, no metadata.\n"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "never published")
	})

	t.Run("a link to a directory fails", func(t *testing.T) {
		err := checkLinksRun(t, "relative-only", "See [it](./sub).\n",
			map[string]string{"sub": ""})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})

	t.Run("a page not published yet is a warning, not a failure", func(t *testing.T) {
		// The ordinary state of a first run over files that link to each other.
		// Failing here would break a build that succeeds on the second attempt.
		err := checkLinksRun(t, "relative-only", "See [other](./other.md).\n",
			map[string]string{
				"other.md": "<!-- Space: DOCS -->\n<!-- Title: Other -->\n\nOther.\n",
			})
		assert.NoError(t, err)
	})

	t.Run("things that are not repository links are left alone", func(t *testing.T) {
		err := checkLinksRun(t, "relative-only",
			"[a](https://example.com/nope) [b](#heading) [c](mailto:x@example.com) "+
				"[d](ac:Some Page) [e](/site/absolute)\n", nil)
		assert.NoError(t, err)
	})

	t.Run("without the flag a broken link is not an error", func(t *testing.T) {
		err := checkLinksRun(t, "", "See [other](./missing.md).\n", nil)
		assert.NoError(t, err)
	})
}

func TestCheckLinksAll(t *testing.T) {
	var reachable, missing string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer remote.Close()
	reachable = remote.URL + "/ok"
	missing = remote.URL + "/gone"

	t.Run("an external URL that answers passes", func(t *testing.T) {
		err := checkLinksRun(t, "all", "See [it]("+reachable+").\n", nil)
		assert.NoError(t, err)
	})

	t.Run("an external URL that does not answer fails", func(t *testing.T) {
		err := checkLinksRun(t, "all", "See [it]("+missing+").\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("relative-only does not request external URLs", func(t *testing.T) {
		err := checkLinksRun(t, "relative-only", "See [it]("+missing+").\n", nil)
		assert.NoError(t, err)
	})
}

func TestCheckLinksRejectsAnUnknownMode(t *testing.T) {
	err := checkLinksRun(t, "sometimes", "Body.\n", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relative-only")
}
