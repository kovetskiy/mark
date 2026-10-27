package page_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolvePageWithoutAHomePage: the home page is only ever compared against
// -- is the first declared parent the home page, is this parentless page the
// home page -- and never used as a parent. Refusing the document when the
// lookup failed therefore aborted every file in the space, including documents
// whose Parent headers never needed one.
func TestResolvePageWithoutAHomePage(t *testing.T) {
	server := confluencetest.New(t)
	api := confluence.NewAPI(server.URL, "user", "token", false)

	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	server.AddPage("DOCS", "Parent", "page", home.ID)

	// The space will not say what its home page is, however it is asked.
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if strings.Contains(r.URL.Path, "space") {
			return http.StatusNotFound, "no such space", true
		}
		return 0, "", false
	})

	parent, _, err := page.ResolvePage(false, api, &metadata.Meta{
		Space:   "DOCS",
		Type:    "page",
		Title:   "Child",
		Parents: []string{"Parent"},
	}, nil)

	require.NoError(t, err, "a document with an explicit parent does not need the home page")
	require.NotNil(t, parent)
	assert.Equal(t, "Parent", parent.Title)
}

// TestResolvePageLeavesMetadataSlicesAlone: the paths built for ancestry and
// for logging were appended onto meta.Parents and meta.Folders, which writes
// into whatever capacity lies past their ends. That capacity belongs to
// whoever built the slice, so the title of this page ended up in someone
// else's.
func TestResolvePageLeavesMetadataSlicesAlone(t *testing.T) {
	for _, withFolder := range []bool{false, true} {
		name := "pages"
		if withFolder {
			name = "folders"
		}

		t.Run(name, func(t *testing.T) {
			server := confluencetest.New(t)
			api := confluence.NewAPI(server.URL, "user", "token", false)

			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			parent := server.AddPage("DOCS", "Parent", "page", home.ID)
			server.AddPage("DOCS", "Child", "page", parent.ID)

			parents := []string{"Parent", "untouched"}
			folders := []string{"Folder", "untouched"}

			meta := &metadata.Meta{
				Space:   "DOCS",
				Type:    "page",
				Title:   "Child",
				Parents: parents[:1],
			}
			if withFolder {
				meta.Folders = folders[:1]
			}

			_, _, err := page.ResolvePage(false, api, meta, nil)
			require.NoError(t, err)

			assert.Equal(t, []string{"Parent", "untouched"}, parents)
			assert.Equal(t, []string{"Folder", "untouched"}, folders)
		})
	}
}
