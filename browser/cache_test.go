package browser

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withCacheDir points os.UserCacheDir at a temporary directory for the test.
// On Linux os.UserCacheDir reads XDG_CACHE_HOME; on macOS it reads HOME and
// appends Library/Caches. Setting both covers the platforms CI runs on.
func withCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
	return dir
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withCacheDir(t)

	in := []*http.Cookie{{Name: "JSESSIONID", Value: "abc123", Path: "/"}}
	require.NoError(t, SaveCookies("https://conf.example.com", in))

	out := LoadCookies("https://conf.example.com")
	require.Len(t, out, 1)
	assert.Equal(t, "JSESSIONID", out[0].Name)
	assert.Equal(t, "abc123", out[0].Value)
}

// TestLoadAbsentIsNil covers the first ever run: no cache file is not a
// problem to report, it is just a login that has not happened yet.
func TestLoadAbsentIsNil(t *testing.T) {
	withCacheDir(t)

	assert.Nil(t, LoadCookies("https://conf.example.com"))
}

// TestLoadCorruptIsNil covers a truncated or hand-edited file: treat it as
// absent and log in again rather than failing the run.
func TestLoadCorruptIsNil(t *testing.T) {
	withCacheDir(t)

	require.NoError(t, SaveCookies("https://conf.example.com",
		[]*http.Cookie{{Name: "a", Value: "b"}}))

	path, err := CachePath()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	assert.Nil(t, LoadCookies("https://conf.example.com"))
}

func TestLoadDropsExpiredCookies(t *testing.T) {
	withCacheDir(t)

	require.NoError(t, SaveCookies("https://conf.example.com", []*http.Cookie{
		{Name: "stale", Value: "x", Expires: time.Now().Add(-time.Hour)},
		{Name: "fresh", Value: "y", Expires: time.Now().Add(time.Hour)},
		{Name: "session", Value: "z"}, // no expiry: a session cookie, keep it
	}))

	out := LoadCookies("https://conf.example.com")
	require.Len(t, out, 2)
	names := []string{out[0].Name, out[1].Name}
	assert.ElementsMatch(t, []string{"fresh", "session"}, names)
}

// TestSaveIsPrivate holds the file mode: the cache is a bearer credential.
func TestSaveIsPrivate(t *testing.T) {
	withCacheDir(t)

	require.NoError(t, SaveCookies("https://conf.example.com",
		[]*http.Cookie{{Name: "a", Value: "b"}}))

	path, err := CachePath()
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	dir, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())
}

// TestTwoHostsCoexist covers a user who publishes to two instances: saving
// one must not evict the other.
func TestTwoHostsCoexist(t *testing.T) {
	withCacheDir(t)

	require.NoError(t, SaveCookies("https://a.example.com",
		[]*http.Cookie{{Name: "one", Value: "1"}}))
	require.NoError(t, SaveCookies("https://b.example.com",
		[]*http.Cookie{{Name: "two", Value: "2"}}))

	a := LoadCookies("https://a.example.com")
	b := LoadCookies("https://b.example.com")
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	assert.Equal(t, "one", a[0].Name)
	assert.Equal(t, "two", b[0].Name)
}
