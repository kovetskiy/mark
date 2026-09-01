package page_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFolderCreateConflictDoesNotDependOnWording pins the class of bug where an
// error is identified by the text Confluence happened to put in it. A folder
// another file created moments earlier makes the create fail, and recognising
// that only by the English phrase "folder exists with the same title" turned a
// recoverable collision into a hard failure the moment the wording changed or
// the instance answered in another language.
func TestFolderCreateConflictDoesNotDependOnWording(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")

	// The folder another file in this run already created. It is hidden from
	// the first search so that this run believes it has to create it.
	server.AddFolder("DOCS", "Manuals", root.ID, "page")

	searched := false
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if !searched && r.URL.Path == "/rest/api/search" {
			searched = true

			return http.StatusOK, `{"results":[]}`, true
		}

		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/v2/folders") {
			// Confluence's own refusal, in a wording no caller may rely on.
			return http.StatusBadRequest,
				`{"errors":[{"title":"Es existiert bereits ein Ordner mit diesem Titel"}]}`,
				true
		}

		return 0, "", false
	})

	parent, err := page.EnsureFolderAncestry(false, api, "DOCS", []string{"Manuals"}, &root.ID, nil)
	require.NoError(t, err, "a folder that already exists is not a failure")
	require.NotNil(t, parent)
	assert.Equal(t, "Manuals", parent.Title)

	assert.Len(t, server.Folders(), 1, "no second folder should have been created")
}

// TestFolderCreateFailureIsStillReported guards the other side: falling back to
// a lookup must not turn a genuine refusal into silence. Nothing carries the
// title, so there is nothing to find and the create's own error stands.
func TestFolderCreateFailureIsStillReported(t *testing.T) {
	page.ResetFolderCache()

	api, server := newAPI(t)
	root := server.AddPage("DOCS", "Root", "page", "")

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/v2/folders") {
			return http.StatusForbidden, `{"errors":[{"title":"not permitted"}]}`, true
		}

		return 0, "", false
	})

	_, err := page.EnsureFolderAncestry(false, api, "DOCS", []string{"Manuals"}, &root.ID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `error creating folder with title "Manuals"`)
	assert.Contains(t, err.Error(), "403", "the reason the create was refused has to survive")
}
