package confluence

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortGrace shortens writeGrace for one test.
func shortGrace(t *testing.T, grace time.Duration) {
	t.Helper()

	saved := writeGrace
	writeGrace = grace
	t.Cleanup(func() { writeGrace = saved })
}

// TestClientCutsAStalledWriteShortAfterItsGrace: a write already sent is
// given writeGrace to answer once the run is stopped, and no more. One that
// has stalled -- an upload the server stopped reading -- would otherwise hold
// a stopped run until TCP gave up.
func TestClientCutsAStalledWriteShortAfterItsGrace(t *testing.T) {
	shortGrace(t, 200*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Stopped once the write is in, which then never answers.
		cancel()
		select {
		case <-r.Context().Done():
		case <-released:
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(released) })

	c := newClient(server.URL+"/rest/api", server.Client(), "user", "token", "")

	started := time.Now()
	_, err := c.do(ctx, nil, http.MethodPut, []string{"content", "42"}, nil, map[string]any{}, &struct{}{})
	elapsed := time.Since(started)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, errGraceExpired)
	assert.True(t, IsCancellation(err))
	assert.Contains(t, err.Error(), "it may have been applied")
	assert.GreaterOrEqual(t, elapsed, 200*time.Millisecond, "the write was given its grace")
	assert.Less(t, elapsed, 5*time.Second, "and no more than that")
}

// TestUpdatePageIsCutShortWhenItStallsAfterTheRunStops: the same through
// UpdatePage, against the fake.
func TestUpdatePageIsCutShortWhenItStallsAfterTheRunStops(t *testing.T) {
	shortGrace(t, 200*time.Millisecond)

	server := confluencetest.New(t)
	server.AddSpace("DOCS")
	stored := server.AddPage("DOCS", "Doc", "page", "")
	api := NewAPI(server.URL, "user", "token", false)

	page, err := api.GetPageByID(context.Background(), stored.ID)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodPut {
			return 0, "", false
		}
		// Read whole, so that the server sees the client give up.
		_, _ = io.Copy(io.Discard, r.Body)
		holdUntilCancelled(r, cancel)
		return http.StatusServiceUnavailable, `{}`, true
	})

	started := time.Now()
	err = api.UpdatePage(ctx, page, "<p>two</p>", false, "", "", "")

	require.ErrorIs(t, err, errGraceExpired)
	assert.Less(t, time.Since(started), 5*time.Second)
	assert.Equal(t, int64(1), page.Version.Number, "no version is recorded for a write that was not answered")
}
