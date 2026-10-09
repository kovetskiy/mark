package mark

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCancellingTheCloudProbeIsNotAFolderRefusal: on a host that does not name
// itself Cloud, a file with folders asks the server which platform it is. A
// cancellation that cut that question short used to be read as "not Cloud",
// and the file failed saying folders need Cloud -- wrong, and not an error
// errors.Is recognises as the cancellation ProcessFileContext promises.
func TestCancellingTheCloudProbeIsNotAFolderRefusal(t *testing.T) {
	server, api := docsSpace(t)
	dir := t.TempDir()

	file := writeFile(t, dir, "doc.md",
		"<!-- Space: DOCS -->\n<!-- Folder: Manuals -->\n<!-- Title: Doc -->\n\nBody.\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The probe is held until the client gives up on it, and the run is
	// cancelled once it has arrived.
	blocked := make(chan struct{})
	var once sync.Once
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if !strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/rest/api/user/current") {
			return 0, "", false
		}
		once.Do(func() { close(blocked) })
		<-r.Context().Done()
		return http.StatusServiceUnavailable, `{"message":"too late"}`, true
	})

	go func() {
		<-blocked
		cancel()
	}()

	_, err := ProcessFileContext(ctx, file, api, publishConfig(server.URL, file))

	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "only available on Confluence Cloud")
}
