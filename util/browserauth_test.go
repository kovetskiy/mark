package util

import (
	"context"
	"net/http"
	"runtime"
	"testing"

	"github.com/kovetskiy/mark/v16/browser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withCacheDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)

	// canOpenBrowser is reached whenever a login is actually attempted. On
	// ubuntu-latest (upstream CI) GOOS is linux and neither DISPLAY nor
	// WAYLAND_DISPLAY is set, so without this every test below that expects a
	// login to proceed would instead fail canOpenBrowser -- something this
	// suite must not depend on the host's windowing system to avoid.
	t.Setenv("DISPLAY", ":0")
}

// stubLogin records whether the browser would have been opened.
func stubLogin(called *bool, cookies []*http.Cookie, err error) loginFunc {
	return func(context.Context, string, browser.Options) ([]*http.Cookie, error) {
		*called = true
		return cookies, err
	}
}

// TestResolveLogsInWhenNoCacheExists covers the first run.
func TestResolveLogsInWhenNoCacheExists(t *testing.T) {
	withCacheDir(t)

	var called bool
	got, err := resolveBrowserAuth(context.Background(), "https://conf.example.com",
		BrowserAuth{}, stubLogin(&called, []*http.Cookie{{Name: "a", Value: "b"}}, nil))
	require.NoError(t, err)

	assert.True(t, called)
	require.Len(t, got, 1)

	// And it was cached for next time.
	assert.Len(t, browser.LoadCookies("https://conf.example.com"), 1)
}

// TestResolveSkipsBrowserForCompileOnly covers --compile-only, which never
// talks to Confluence: opening an SSO window to render markdown to stdout
// would be absurd.
func TestResolveSkipsBrowserForCompileOnly(t *testing.T) {
	withCacheDir(t)

	var called bool
	got, err := resolveBrowserAuth(context.Background(), "https://conf.example.com",
		BrowserAuth{CompileOnly: true}, stubLogin(&called, nil, nil))
	require.NoError(t, err)

	assert.False(t, called)
	assert.Nil(t, got)
}

// TestResolveDryRunStillResolvesASession covers --login --dry-run: a dry run
// still calls page.ResolvePage / api.GetPageByID, so it needs a session like
// any other run. Skipping the browser here used to mean it published
// unauthenticated, which reports every page on an SSO instance as new.
func TestResolveDryRunStillResolvesASession(t *testing.T) {
	withCacheDir(t)

	var called bool
	got, err := resolveBrowserAuth(context.Background(), "https://conf.example.com",
		BrowserAuth{DryRun: true}, stubLogin(&called, []*http.Cookie{{Name: "a", Value: "b"}}, nil))
	require.NoError(t, err)

	assert.True(t, called)
	require.Len(t, got, 1)
}

// TestResolveRefusesInCI covers the failure this feature most needs to get
// right: a CI job must fail with an explanation, not hang on a login window.
func TestResolveRefusesInCI(t *testing.T) {
	withCacheDir(t)

	var called bool
	_, err := resolveBrowserAuth(context.Background(), "https://conf.example.com",
		BrowserAuth{CI: true}, stubLogin(&called, nil, nil))

	require.Error(t, err)
	assert.False(t, called)
	assert.Contains(t, err.Error(), "--password")
}

// TestResolveReusesAValidCachedCookie is the reason the cache exists: the
// browser must not open on every run.
func TestResolveReusesAValidCachedCookie(t *testing.T) {
	withCacheDir(t)
	require.NoError(t, browser.SaveCookies("https://conf.example.com",
		[]*http.Cookie{{Name: "cached", Value: "v"}}))

	var called bool
	got, err := resolveBrowserAuth(context.Background(), "https://conf.example.com",
		BrowserAuth{authenticates: func([]*http.Cookie) bool { return true }},
		stubLogin(&called, nil, nil))
	require.NoError(t, err)

	assert.False(t, called)
	require.Len(t, got, 1)
	assert.Equal(t, "cached", got[0].Name)
}

// TestResolveDiscardsAStaleCachedCookie covers an expired session: log in
// again, and overwrite what was cached.
func TestResolveDiscardsAStaleCachedCookie(t *testing.T) {
	withCacheDir(t)
	require.NoError(t, browser.SaveCookies("https://conf.example.com",
		[]*http.Cookie{{Name: "stale", Value: "v"}}))

	var called bool
	got, err := resolveBrowserAuth(context.Background(), "https://conf.example.com",
		BrowserAuth{authenticates: func([]*http.Cookie) bool { return false }},
		stubLogin(&called, []*http.Cookie{{Name: "fresh", Value: "v"}}, nil))
	require.NoError(t, err)

	assert.True(t, called)
	require.Len(t, got, 1)
	assert.Equal(t, "fresh", got[0].Name)

	cached := browser.LoadCookies("https://conf.example.com")
	require.Len(t, cached, 1)
	assert.Equal(t, "fresh", cached[0].Name)
}

// TestResolveRefusesOnLinuxWithNoDisplay covers the other half of
// canOpenBrowser: a Linux box with no X11 or Wayland session must fail with
// the same advice a CI run gets, and must not open a browser. The check
// itself is Linux-only by design (macOS and Windows always have a display),
// so this only means anything on GOOS=linux.
func TestResolveRefusesOnLinuxWithNoDisplay(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("canOpenBrowser's no-display check is Linux-only")
	}

	withCacheDir(t)
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	var called bool
	_, err := resolveBrowserAuth(context.Background(), "https://conf.example.com",
		BrowserAuth{}, stubLogin(&called, nil, nil))

	require.Error(t, err)
	assert.False(t, called)
	assert.Contains(t, err.Error(), "--password")
}
