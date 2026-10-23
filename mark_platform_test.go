package mark

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunStopsWhenThePlatformCannotBeIdentified: Cloud and Server take
// different paths for folders, moves and the manifest, so a run that cannot
// tell which one it is talking to stops before it publishes anything.
func TestRunStopsWhenThePlatformCannotBeIdentified(t *testing.T) {
	server, _ := docsSpace(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/rest/api/user/current" {
			return http.StatusForbidden, `{"message":"forbidden"}`, true
		}
		return 0, "", false
	})
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))

	err := Run(trackingConfig(server, filepath.Join(dir, "*.md")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to identify the Confluence platform")
	assert.Equal(t, 0, countPagesTitled(t, server, "Doc"))
}

// TestRunPublishesWhenTheUserNamesNoPlatform: only a failed request stops the
// run; a user with neither an accountId nor a username is taken for Server.
func TestRunPublishesWhenTheUserNamesNoPlatform(t *testing.T) {
	server, _ := docsSpace(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/rest/api/user/current" {
			return http.StatusOK, `{"type":"anonymous"}`, true
		}
		return 0, "", false
	})
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))

	require.NoError(t, Run(trackingConfig(server, filepath.Join(dir, "*.md"))))
	assert.Equal(t, 1, countPagesTitled(t, server, "Doc"))
}

func TestCompileOnlyDoesNotAskForThePlatform(t *testing.T) {
	server, _ := docsSpace(t)
	dir := t.TempDir()
	writeFile(t, dir, "doc.md", markdownWithTitle("Doc"))

	config := trackingConfig(server, filepath.Join(dir, "*.md"))
	config.TrackPages = false
	config.CompileOnly = true
	require.NoError(t, Run(config))
	assert.Zero(t, server.CountRequests("GET", "/rest/api/user/current"))
}
