package mark

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkLinksRun publishes a directory containing one document with the given
// body and returns whatever the run made of it.
func checkLinksRun(t *testing.T, modes []string, body string, extra map[string]string) error {
	t.Helper()

	return checkLinksRunWith(t, modes, false, body, extra)
}

func checkLinksRunWith(t *testing.T, modes []string, warnOnly bool, body string, extra map[string]string) error {
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
		Files:              filepath.Join(dir, "doc.md"),
		Features:           []string{"mention"},
		CheckLinks:         modes,
		CheckLinksWarnOnly: warnOnly,
		Output:             io.Discard,
	})
}

func TestCheckLinksInternal(t *testing.T) {
	t.Run("a link to a file that does not exist fails", func(t *testing.T) {
		err := checkLinksRun(t, []string{"internal"}, "See [other](./missing.md).\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "./missing.md")
		assert.Contains(t, err.Error(), "there is no such file")
	})

	t.Run("a link to a file that is never published fails", func(t *testing.T) {
		// A file with no headers still gets metadata when the run knows a
		// space, so this shows up as a missing title rather than missing
		// metadata. Either way the file never becomes a page.
		err := checkLinksRun(t, []string{"internal"}, "See [notes](./notes.md).\n",
			map[string]string{"notes.md": "Just notes, no metadata.\n"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "never published")
	})

	t.Run("a link to a directory fails", func(t *testing.T) {
		err := checkLinksRun(t, []string{"internal"}, "See [it](./sub).\n",
			map[string]string{"sub": ""})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})

	t.Run("a page not published yet is a warning, not a failure", func(t *testing.T) {
		// The ordinary state of a first run over files that link to each other.
		// Failing here would break a build that succeeds on the second attempt.
		err := checkLinksRun(t, []string{"internal"}, "See [other](./other.md).\n",
			map[string]string{
				"other.md": "<!-- Space: DOCS -->\n<!-- Title: Other -->\n\nOther.\n",
			})
		assert.NoError(t, err)
	})

	t.Run("things that are not repository links are left alone", func(t *testing.T) {
		// Including an ac: link and an unreachable URL: neither kind was asked
		// for, so neither is judged.
		err := checkLinksRun(t, []string{"internal"},
			"[a](https://example.invalid/nope) [b](#heading) [c](mailto:x@example.com) "+
				"[d](ac:No Such Page) [e](/site/absolute)\n", nil)
		assert.NoError(t, err)
	})

	t.Run("without the flag a broken link is not an error", func(t *testing.T) {
		err := checkLinksRun(t, nil, "See [other](./missing.md).\n", nil)
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
		err := checkLinksRun(t, []string{"all"}, "See [it]("+reachable+").\n", nil)
		assert.NoError(t, err)
	})

	t.Run("an external URL that does not answer fails", func(t *testing.T) {
		err := checkLinksRun(t, []string{"all"}, "See [it]("+missing+").\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("internal alone does not request external URLs", func(t *testing.T) {
		err := checkLinksRun(t, []string{"internal"}, "See [it]("+missing+").\n", nil)
		assert.NoError(t, err)
	})

	t.Run("internal and confluence together still skip external", func(t *testing.T) {
		// The combination the set exists for: check what is cheap, leave the
		// network alone.
		err := checkLinksRun(t, []string{"internal", "confluence"},
			"See [it]("+missing+").\n", nil)
		assert.NoError(t, err)
	})
}

func TestCheckLinksRejectsAnUnknownMode(t *testing.T) {
	err := checkLinksRun(t, []string{"sometimes"}, "Body.\n", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "internal")
	assert.Contains(t, err.Error(), "confluence")
	assert.Contains(t, err.Error(), "external")
}

func TestCheckLinksConfluence(t *testing.T) {
	t.Run("a link to a page that exists passes", func(t *testing.T) {
		err := checkLinksRun(t, []string{"confluence"}, "See [it](ac:Parent).\n", nil)
		assert.NoError(t, err)
	})

	t.Run("a link to a page that does not exist fails", func(t *testing.T) {
		err := checkLinksRun(t, []string{"confluence"}, "See [it](ac:Nowhere).\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "there is no page \"Nowhere\"")
	})

	t.Run("the title comes from the link text when nothing follows the colon", func(t *testing.T) {
		// [Parent](ac:) is the form where the words between the brackets are
		// the page title, and the renderer reads it that way too.
		assert.NoError(t, checkLinksRun(t, []string{"confluence"}, "See [Parent](ac:).\n", nil))

		err := checkLinksRun(t, []string{"confluence"}, "See [Nowhere](ac:).\n", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Nowhere")
	})

	t.Run("internal alone does not judge an ac: link", func(t *testing.T) {
		err := checkLinksRun(t, []string{"internal"}, "See [it](ac:Nowhere).\n", nil)
		assert.NoError(t, err)
	})

	t.Run("confluence alone does not judge a relative link", func(t *testing.T) {
		err := checkLinksRun(t, []string{"confluence"}, "See [it](./missing.md).\n", nil)
		assert.NoError(t, err)
	})
}

// TestCheckLinksReportsEveryBrokenLink pins that a file is not abandoned at the
// first failure. Reporting one at a time turns a page with several into as many
// runs to find out.
func TestCheckLinksReportsEveryBrokenLink(t *testing.T) {
	err := checkLinksRun(t, []string{"all"},
		"[a](./one.md) [b](./two.md) [c](./three.md) [d](ac:Nowhere)\n", nil)

	require.Error(t, err)
	for _, expected := range []string{"./one.md", "./two.md", "./three.md", "Nowhere"} {
		assert.Contains(t, err.Error(), expected)
	}
	assert.Contains(t, err.Error(), "4 links do not resolve")
}

func TestCheckLinksCountsOneLinkAsSingular(t *testing.T) {
	err := checkLinksRun(t, []string{"internal"}, "[a](./one.md)\n", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 link does not resolve")
}

// TestCheckLinksWarnOnly is the adoption path: see the whole list without the
// build failing over it.
func TestCheckLinksWarnOnly(t *testing.T) {
	t.Run("a broken link does not fail the run", func(t *testing.T) {
		err := checkLinksRunWith(t, []string{"all"}, true,
			"[a](./one.md) [b](ac:Nowhere)\n", nil)
		assert.NoError(t, err)
	})

	t.Run("the page is still published", func(t *testing.T) {
		// A run that reports and refuses to publish would be the worst of both.
		server := confluencetest.New(t)
		home := server.AddPage("DOCS", "Home", "page", "")
		server.SetHomepage("DOCS", home.ID)
		server.AddPage("DOCS", "Parent", "page", home.ID)

		dir := t.TempDir()
		writeFile(t, dir, "doc.md",
			"<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n\n"+
				"Body here. [a](./missing.md)\n")

		require.NoError(t, Run(Config{
			BaseURL: server.URL, Username: "user", Password: "token",
			Files:      filepath.Join(dir, "doc.md"),
			Features:   []string{"mention"},
			CheckLinks: []string{"internal"}, CheckLinksWarnOnly: true,
			Output: io.Discard,
		}))

		api := confluence.NewAPI(server.URL, "user", "token", false)
		doc, err := api.FindPage("DOCS", "Doc", "page")
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Contains(t, server.Page(doc.ID).Body, "Body here")
	})

	t.Run("without it the same document fails", func(t *testing.T) {
		err := checkLinksRunWith(t, []string{"all"}, false,
			"[a](./one.md) [b](ac:Nowhere)\n", nil)
		require.Error(t, err)
	})
}
