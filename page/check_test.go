package page

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLinkCheck(t *testing.T) {
	for value, expected := range map[string]LinkCheck{
		"":              LinkCheckNone,
		"relative-only": LinkCheckRelative,
		"all":           LinkCheckAll,
		"  all  ":       LinkCheckAll,
	} {
		got, err := ParseLinkCheck(value)
		assert.NoError(t, err, "value %q", value)
		assert.Equal(t, expected, got, "value %q", value)
	}

	_, err := ParseLinkCheck("everything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relative-only")
}

// TestLinkCheckerAsksOnce pins the cache. A URL on twenty pages should cost one
// request, not twenty.
func TestLinkCheckerAsksOnce(t *testing.T) {
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewLinkChecker(LinkCheckAll)
	for range 5 {
		assert.NoError(t, checker.CheckExternal(server.URL+"/page"))
	}

	assert.Equal(t, int64(1), atomic.LoadInt64(&requests))
}

// TestLinkCheckerFallsBackToGet covers hosts that refuse HEAD while serving the
// same URL perfectly well, which is common enough to matter.
func TestLinkCheckerFallsBackToGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	assert.NoError(t, NewLinkChecker(LinkCheckAll).CheckExternal(server.URL+"/page"))
}

func TestLinkCheckerReportsFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := NewLinkChecker(LinkCheckAll).CheckExternal(server.URL + "/gone")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")

	// Nothing listening at all.
	err = NewLinkChecker(LinkCheckAll).CheckExternal("http://127.0.0.1:1/nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unreachable")
}

// TestLinkCheckerOffAsksNothing: relative-only must not make network requests.
func TestLinkCheckerOffAsksNothing(t *testing.T) {
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
	}))
	defer server.Close()

	for _, mode := range []LinkCheck{LinkCheckNone, LinkCheckRelative} {
		assert.NoError(t, NewLinkChecker(mode).CheckExternal(server.URL))
	}
	assert.Nil(t, (*LinkChecker)(nil).CheckExternal(server.URL))

	assert.Zero(t, atomic.LoadInt64(&requests))
}
