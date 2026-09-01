package mark

import (
	"net/http"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failOnceLookingUp makes the fake refuse the first page lookup for a title and
// answer every later one normally, which is what a network blip looks like from
// here.
func failOnceLookingUp(server *confluencetest.Server, title string) {
	failed := false
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if failed || r.Method != http.MethodGet {
			return 0, "", false
		}
		if r.URL.Query().Get("title") != title {
			return 0, "", false
		}

		failed = true

		return http.StatusForbidden, `{"message":"nope"}`, true
	})
}

// TestStaleParentLookupFailureStopsTheRun pins the class of bug where a failed
// read is treated as an answer. refreshStaleParents asks whether a parent title
// still resolves, and a lookup that errored used to be indistinguishable from a
// parent that is exactly where it says it is -- so a blip left the stale title
// in place, which is the one condition that makes ancestry create an empty
// duplicate parent and re-home the real one's children.
func TestStaleParentLookupFailureStopsTheRun(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownWithTitle("Child"))

	failOnceLookingUp(server, "Parent")

	err := Run(trackingConfig(server, file))
	require.Error(t, err, "a parent that could not be read is not a parent that is fine")
	assert.Contains(t, err.Error(), `unable to look up parent "Parent"`)
}

// TestStaleParentLookupSucceedsNormally guards the other side: the failure path
// must not have made the ordinary run any more fragile.
func TestStaleParentLookupSucceedsNormally(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", markdownWithTitle("Child"))

	require.NoError(t, Run(trackingConfig(server, file)))

	published, err := api.FindPage("DOCS", "Child", "page")
	require.NoError(t, err)
	require.NotNil(t, published)
}
